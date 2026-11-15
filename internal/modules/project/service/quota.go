package service

import (
	"strings"
	"sync"
	"time"

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
	// RetryInfo delay, so the model is not hammered until the next request.
	defaultDailyQuotaCooldown = 24 * time.Hour
	// quotaLogInterval limits the clear ERROR log to once per model per hour.
	quotaLogInterval = time.Hour
)

// quotaState remembers models whose daily quota is exhausted. It is safe for
// concurrent requests; now is injectable in tests.
type quotaState struct {
	mu        sync.Mutex
	now       func() time.Time
	exhausted map[string]time.Time
	logged    map[string]time.Time
}

func newQuotaState(now func() time.Time) *quotaState {
	if now == nil {
		now = time.Now
	}
	return &quotaState{
		now:       now,
		exhausted: make(map[string]time.Time),
		logged:    make(map[string]time.Time),
	}
}

func quotaKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// markExhausted records model as unusable until retryDelay elapses and returns
// that deadline. A missing delay falls back to 24h to avoid repeated calls.
func (q *quotaState) markExhausted(model string, retryDelay time.Duration) time.Time {
	if retryDelay <= 0 {
		retryDelay = defaultDailyQuotaCooldown
	}
	until := q.now().Add(retryDelay)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.exhausted[quotaKey(model)] = until
	return until
}

// isExhausted reports whether model is still inside its daily-quota cooldown.
// Expired entries are removed lazily.
func (q *quotaState) isExhausted(model string) bool {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
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

// recordDailyQuota marks the requested model and emits at most one clear ERROR
// per model per hour. It returns true when err is a daily-quota failure.
func (s *service) recordDailyQuota(err error, model string) bool {
	info, ok := aiprovider.QuotaErrorInfo(err)
	if !ok || !info.Daily {
		return false
	}
	until := s.quota.markExhausted(model, info.RetryDelay)
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
