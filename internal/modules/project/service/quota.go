package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Abshoor24/backend-go/internal/aistate"
	"github.com/Abshoor24/backend-go/internal/apperror"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

// QuotaExhaustedMessage is the client-safe message used when every configured
// model is blocked by an exhausted daily provider quota.
const QuotaExhaustedMessage = "Kuota AI harian telah habis. Silakan coba lagi nanti atau hubungi penyelenggara."

const (
	// maxTemporaryQuotaRetry is the largest RetryInfo delay still treated as a
	// one-shot retry rather than the existing 503 backoff policy.
	maxTemporaryQuotaRetry = 10 * time.Second
	// defaultDailyQuotaCooldown is used when a daily quota error has no
	// RetryInfo delay; it is still clamped to the next 15:00 WIB reset.
	defaultDailyQuotaCooldown = 24 * time.Hour
	// quotaLogInterval limits the clear ERROR log to once per model per hour.
	quotaLogInterval = time.Hour
	// defaultDailyQuotaLimit matches the Gemini free tier: 20 generate
	// requests per model per day.
	defaultDailyQuotaLimit = 20
)

// wib is UTC+7 without DST. The Gemini free-tier quota resets at midnight
// Pacific, which is 15:00 WIB.
var wib = time.FixedZone("WIB", 7*60*60)

// quotaWindowStart returns the start of the quota day containing now.
// The boundary is 15:00 WIB, so the "day" runs from 15:00 to 15:00.
func quotaWindowStart(now time.Time) time.Time {
	local := now.In(wib)
	resetToday := time.Date(local.Year(), local.Month(), local.Day(), 15, 0, 0, 0, wib)
	if local.Before(resetToday) {
		return resetToday.AddDate(0, 0, -1)
	}
	return resetToday
}

func quotaResetAt(windowStart time.Time) time.Time {
	return windowStart.AddDate(0, 0, 1)
}

// QuotaStore persists daily quota state so it survives a backend restart.
type QuotaStore interface {
	LoadQuota(ctx context.Context, windowStart time.Time) ([]aistate.QuotaRecord, error)
	IncrementGenerate(ctx context.Context, model string, windowStart time.Time) error
	MarkExhausted(ctx context.Context, model string, windowStart time.Time, until time.Time) error
}

// quotaState is the in-memory cache in front of QuotaStore. It is safe for
// concurrent requests; now is injectable in tests.
type quotaState struct {
	mu        sync.Mutex
	now       func() time.Time
	exhausted map[string]time.Time
	logged    map[string]time.Time
	counts    map[string]int
	window    time.Time
	loaded    bool
}

func newQuotaState(now func() time.Time) *quotaState {
	if now == nil {
		now = time.Now
	}
	q := &quotaState{
		now:       now,
		exhausted: make(map[string]time.Time),
		logged:    make(map[string]time.Time),
		counts:    make(map[string]int),
	}
	q.window = quotaWindowStart(now())
	return q
}

func quotaKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// rollLocked resets the per-window state when the 15:00 WIB boundary passes.
func (q *quotaState) rollLocked(now time.Time) time.Time {
	window := quotaWindowStart(now)
	if q.window.IsZero() || !q.window.Equal(window) {
		q.window = window
		q.exhausted = make(map[string]time.Time)
		q.counts = make(map[string]int)
		q.loaded = false
	}
	return window
}

func (q *quotaState) currentWindow() time.Time {
	return quotaWindowStart(q.now())
}

func (q *quotaState) needsLoad(window time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return !q.loaded || !q.window.Equal(window)
}

// hydrate replaces the current-window state with persisted rows, keeping the
// larger of persisted and in-process counts so a concurrent increment that
// landed before the load finished is not lost.
func (q *quotaState) hydrate(window time.Time, records []aistate.QuotaRecord) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(q.now())
	if !q.window.Equal(window) {
		return
	}
	persisted := make(map[string]int, len(records))
	exhausted := make(map[string]time.Time)
	for _, rec := range records {
		key := quotaKey(rec.Model)
		persisted[key] = rec.GenerateCount
		if !rec.ExhaustedUntil.IsZero() {
			exhausted[key] = rec.ExhaustedUntil
		}
	}
	for key, count := range persisted {
		if count > q.counts[key] {
			q.counts[key] = count
		}
	}
	q.exhausted = exhausted
	q.loaded = true
}

func (q *quotaState) markLoaded(window time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.window.Equal(window) {
		q.loaded = true
	}
}

// markExhausted records model as unusable until retryDelay elapses (capped at
// the next 15:00 WIB reset) and returns that deadline.
func (q *quotaState) markExhausted(model string, retryDelay time.Duration) time.Time {
	now := q.now()
	if retryDelay <= 0 {
		retryDelay = defaultDailyQuotaCooldown
	}
	until := now.Add(retryDelay)
	q.mu.Lock()
	defer q.mu.Unlock()
	window := q.rollLocked(now)
	if reset := quotaResetAt(window); until.After(reset) {
		until = reset
	}
	q.exhausted[quotaKey(model)] = until
	return until
}

// isExhausted reports whether model is still inside its daily-quota cooldown.
// Expired entries are removed lazily.
func (q *quotaState) isExhausted(model string) bool {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(now)
	until, ok := q.exhausted[quotaKey(model)]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(q.exhausted, quotaKey(model))
		return false
	}
	return true
}

// allExhausted reports whether every model is inside a daily-quota cooldown.
func (q *quotaState) allExhausted(models []string) bool {
	if len(models) == 0 {
		return false
	}
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(now)
	for _, model := range models {
		key := quotaKey(model)
		until, ok := q.exhausted[key]
		if !ok || !now.Before(until) {
			if ok {
				delete(q.exhausted, key)
			}
			return false
		}
	}
	return true
}

func (q *quotaState) increment(model string) {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(now)
	q.counts[quotaKey(model)]++
}

func (q *quotaState) used(model string) int {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(now)
	return q.counts[quotaKey(model)]
}

func (q *quotaState) exhaustedUntil(model string) (time.Time, bool) {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rollLocked(now)
	until, ok := q.exhausted[quotaKey(model)]
	if !ok || !now.Before(until) {
		if ok {
			delete(q.exhausted, quotaKey(model))
		}
		return time.Time{}, false
	}
	return until, true
}

// shouldLog is true at most once per model per hour.
func (q *quotaState) shouldLog(model string) bool {
	now := q.now()
	key := quotaKey(model)
	q.mu.Lock()
	defer q.mu.Unlock()
	if last, ok := q.logged[key]; ok && now.Sub(last) < quotaLogInterval {
		return false
	}
	q.logged[key] = now
	return true
}

// ensureQuotaLoaded hydrates the in-memory quota state from the persistent
// store once per 15:00 WIB window. A store failure degrades to memory-only for
// that window instead of failing the request.
func (s *service) ensureQuotaLoaded(ctx context.Context) time.Time {
	window := s.quota.currentWindow()
	if s.quotaStore == nil || !s.quota.needsLoad(window) {
		return window
	}
	records, err := s.quotaStore.LoadQuota(ctx, window)
	if err != nil {
		s.logger.Warn("failed to load persisted AI quota state", "error", err)
		s.quota.markLoaded(window)
		return window
	}
	s.quota.hydrate(window, records)
	return window
}

// noteGenerateAttempt persists and counts one provider generate call. This is
// the estimate used by /ai/quota-status; cached results never call it.
func (s *service) noteGenerateAttempt(ctx context.Context, model string) {
	window := s.quota.currentWindow()
	s.quota.increment(model)
	if s.quotaStore == nil {
		return
	}
	if err := s.quotaStore.IncrementGenerate(ctx, model, window); err != nil {
		s.logger.Warn("failed to persist AI generate count", "model", model, "error", err)
	}
}

// recordDailyQuota marks the requested model and emits at most one clear ERROR
// per model per hour. It returns true when err is a daily-quota failure.
func (s *service) recordDailyQuota(ctx context.Context, err error, model string) bool {
	info, ok := aiprovider.QuotaErrorInfo(err)
	if !ok || !info.Daily {
		return false
	}
	window := s.quota.currentWindow()
	until := s.quota.markExhausted(model, info.RetryDelay)
	if s.quotaStore != nil {
		if perr := s.quotaStore.MarkExhausted(ctx, model, window, until); perr != nil {
			s.logger.Warn("failed to persist AI quota exhaustion", "model", model, "error", perr)
		}
	}
	if s.quota.shouldLog(model) {
		attrs := []any{
			"model", model,
			"quota_id", info.QuotaID,
			"quota_value", info.QuotaValue,
			"retry_delay", info.RetryDelay.String(),
			"exhausted_until", until.UTC().Format(time.RFC3339),
		}
		if info.QuotaModel != "" && !strings.EqualFold(info.QuotaModel, model) {
			attrs = append(attrs, "quota_model", info.QuotaModel)
		}
		s.logger.Error("ai provider daily quota exhausted", attrs...)
	}
	return true
}

// temporaryQuotaRetry returns a short RetryInfo delay that should be honored
// once on the same model. Daily quotas and delays above 10s fall through to the
// existing 503 retry/fallback policy.
func temporaryQuotaRetry(err error) (time.Duration, bool) {
	info, ok := aiprovider.QuotaErrorInfo(err)
	if !ok || info.Daily || info.RetryDelay <= 0 || info.RetryDelay > maxTemporaryQuotaRetry {
		return 0, false
	}
	return info.RetryDelay, true
}

func quotaBusyError(lastErr error) error {
	return apperror.AIBusyReason(apperror.ReasonQuota, QuotaExhaustedMessage, lastErr)
}

// QuotaModelStatus is the per-model section of GET /ai/quota-status.
type QuotaModelStatus struct {
	Model          string     `json:"model"`
	Used           int        `json:"used"`
	Remaining      int        `json:"remaining"`
	Exhausted      bool       `json:"exhausted"`
	ExhaustedUntil *time.Time `json:"exhausted_until,omitempty"`
	Status         string     `json:"status"`
}

// QuotaStatus is the response body of GET /ai/quota-status.
type QuotaStatus struct {
	WindowStart time.Time          `json:"window_start"`
	ResetAt     time.Time          `json:"reset_at"`
	DailyLimit  int                `json:"daily_limit"`
	Models      []QuotaModelStatus `json:"models"`
}

// QuotaStatus reports the estimated remaining free-tier generates for every
// configured model in the current 15:00 WIB quota window.
func (s *service) QuotaStatus(ctx context.Context) QuotaStatus {
	window := s.ensureQuotaLoaded(ctx)
	limit := s.aiCfg.DailyQuotaLimit
	if limit <= 0 {
		limit = defaultDailyQuotaLimit
	}
	models := s.candidateModels()
	out := QuotaStatus{
		WindowStart: window.In(wib),
		ResetAt:     quotaResetAt(window).In(wib),
		DailyLimit:  limit,
		Models:      make([]QuotaModelStatus, 0, len(models)),
	}
	for _, modelName := range models {
		if strings.TrimSpace(modelName) == "" {
			continue
		}
		used := s.quota.used(modelName)
		remaining := limit - used
		if remaining < 0 {
			remaining = 0
		}
		until, exhausted := s.quota.exhaustedUntil(modelName)
		status := "available"
		if exhausted || used >= limit {
			status = "exhausted"
			remaining = 0
		}
		item := QuotaModelStatus{
			Model:     modelName,
			Used:      used,
			Remaining: remaining,
			Exhausted: status == "exhausted",
			Status:    status,
		}
		if exhausted {
			t := until.In(wib)
			item.ExhaustedUntil = &t
		}
		out.Models = append(out.Models, item)
	}
	return out
}
