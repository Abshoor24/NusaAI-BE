package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
	"github.com/Abshoor24/backend-go/internal/modules/project/repository"
)

// Service is the project domain contract used by HTTP handlers.
type Service interface {
	Create(ctx context.Context, userID string, req model.CreateProjectRequest) (*model.Project, error)
	List(ctx context.Context, userID string, limit, offset int) ([]*model.Project, error)
	Get(ctx context.Context, userID, id string) (*model.Project, *model.Version, error)
	Delete(ctx context.Context, userID, id string) error
	// UpdateTitle renames an owned project unless a generation is running.
	UpdateTitle(ctx context.Context, userID, id, title string) (*model.Project, error)
	ListVersions(ctx context.Context, userID, projectID string) ([]*model.Version, error)
	GetVersion(ctx context.Context, userID, projectID string, number int) (*model.Version, error)
	// ExportTarget resolves the project + HTML version for download.
	ExportTarget(ctx context.Context, userID, projectID string, version *int) (*model.Project, *model.Version, error)
	ListAttachments(ctx context.Context, userID, projectID string) ([]*model.ProjectAttachment, error)
	// GetAttachment returns the attachment row plus an open file reader.
	GetAttachment(ctx context.Context, userID, projectID, attachmentID string) (*model.ProjectAttachment, io.ReadCloser, error)

	// PrepareGenerate validates input, atomically claims the project
	// (draft/failed -> generating), stores attachments, and builds the
	// prompt without touching the provider. It is safe to open SSE headers
	// after this returns; only OpenStream talks to the provider.
	PrepareGenerate(ctx context.Context, userID, projectID string, imgs []model.ImagePayload) (*PendingStream, error)
	// PrepareRevise is the ready -> generating counterpart.
	PrepareRevise(ctx context.Context, userID, projectID, instruction string, baseVersion *int, imgs []model.ImagePayload) (*PendingStream, error)
	// OpenStream runs the provider open phase (retries + fallback models)
	// and attaches the winning stream to pending. On failure it rolls the
	// project status back and returns AI_BUSY when the open budget was
	// exhausted or every model stayed overloaded/unresponsive.
	OpenStream(ctx context.Context, pending *PendingStream, observe OpenObserver) error
	// BeginGenerate is the combined prepare+open helper kept for callers
	// that do not stream SSE (and for tests).
	BeginGenerate(ctx context.Context, userID, projectID string, imgs []model.ImagePayload) (*PendingStream, error)
	// BeginRevise is the ready -> generating counterpart.
	BeginRevise(ctx context.Context, userID, projectID, instruction string, baseVersion *int, imgs []model.ImagePayload) (*PendingStream, error)
	// FinishStream validates the full buffer and stores the new version in
	// one transaction (status -> ready). No version is stored on failure.
	FinishStream(ctx context.Context, pending *PendingStream, rawBuffer string) (*model.Version, error)
	// FailStream rolls status back (failed for generate, ready for revise).
	// Callers must pass a detached context (not the cancelled request ctx).
	FailStream(ctx context.Context, pending *PendingStream)
}

// PendingStream carries everything the handler needs between opening the
// provider stream and finalizing the version.
type PendingStream struct {
	Project   *model.Project
	Kind      string
	Prompt    string // stored in project_versions.prompt
	Stream    <-chan aiprovider.StreamChunk
	StartedAt time.Time
	MaxBytes  int64
	// Model is the provider model that actually served the request
	// (primary or fallback). Populated by OpenStream.
	Model string
	// FallbackUsed is true when a non-primary model served the request.
	FallbackUsed bool
	// AttemptNumber is the provider open attempt within the selected model.
	AttemptNumber int
	// TotalAttempts counts every provider open call made (including retries
	// and failed fallback attempts).
	TotalAttempts int
	// OpenDuration is the time spent in the provider open phase.
	OpenDuration time.Duration
	// ThinkingLevel is the level actually sent, or "default" when omitted.
	ThinkingLevel string
	// HedgeStarted is the delay before the hedge fallback was started; zero
	// when hedging never fired.
	HedgeStarted time.Duration
	// HedgeWinner is "primary" or "fallback" when hedging fired.
	HedgeWinner string
	// LoserCancelled is true when the losing hedge candidate was still
	// running and had to be cancelled.
	LoserCancelled bool
	// systemPrompt, userPrompt and providerImages are captured by
	// PrepareGenerate/PrepareRevise and sent by OpenStream.
	systemPrompt   string
	userPrompt     string
	providerImages []providerImage
	// FinishReason is the provider's terminal reason observed on the stream
	// (e.g. "STOP", "MAX_TOKENS"); empty when unknown. The handler records
	// it while relaying chunks.
	FinishReason string
	// BlockReason is the provider's safety block reason, if any.
	BlockReason string
	// Usage carries token accounting reported with the terminal chunk.
	Usage *aiprovider.UsageMetadata
	// Chunks and TextBytes count relayed provider chunks and text bytes for
	// per-stream diagnostics (populated by the handler loop).
	Chunks    int
	TextBytes int
}

// Open phase labels surfaced to the frontend as SSE status events.
const (
	OpenPhaseConnecting = "connecting"
	OpenPhaseRetrying   = "retrying"
	OpenPhaseFallback   = "fallback"
)

// OpenBusyMessage is the client-safe Indonesian message used when the open
// budget is exhausted or the provider remains overloaded.
const OpenBusyMessage = "Server AI sedang ramai. Silakan coba lagi sebentar lagi."

// OpenPhaseEvent is one progress notification from the provider open phase.
// Model names are intentionally excluded from the client contract.
type OpenPhaseEvent struct {
	Phase   string
	Attempt int
	Elapsed time.Duration
}

// OpenObserver receives open-phase progress notifications. It is called from
// OpenStream's goroutine; implementations must not block.
type OpenObserver func(OpenPhaseEvent)

type service struct {
	repo       repository.Repository
	provider   aiprovider.Provider
	aiCfg      config.AIConfig
	uploadCfg  config.UploadConfig
	projectCfg config.ProjectConfig
	logger     *slog.Logger
	quota      *quotaState
}

// New wires the project service. provider may be nil in tests that never stream.
func New(repo repository.Repository, provider aiprovider.Provider, aiCfg config.AIConfig, uploadCfg config.UploadConfig, projectCfg config.ProjectConfig, logger *slog.Logger) Service {
	if uploadCfg.Dir == "" {
		uploadCfg.Dir = "uploads"
	}
	if uploadCfg.MaxSizeBytes <= 0 {
		uploadCfg.MaxSizeBytes = 10 << 20
	}
	if projectCfg.MaxImages <= 0 {
		projectCfg.MaxImages = 4
	}
	if projectCfg.MaxHTMLBytes <= 0 {
		projectCfg.MaxHTMLBytes = 524288
	}
	return &service{
		repo: repo, provider: provider, aiCfg: aiCfg, uploadCfg: uploadCfg,
		projectCfg: projectCfg, logger: logger, quota: newQuotaState(time.Now),
	}
}

func (s *service) Create(ctx context.Context, userID string, req model.CreateProjectRequest) (*model.Project, error) {
	req.Brief.ApplyDefaults()
	if fields := model.Validate(req); fields != nil {
		return nil, apperror.ValidationFields(fields)
	}
	title := model.DeriveTitle(req.Title, req.Brief)
	return s.repo.Create(ctx, userID, title, req.Brief)
}

func (s *service) List(ctx context.Context, userID string, limit, offset int) ([]*model.Project, error) {
	return s.repo.List(ctx, userID, limit, offset)
}

func (s *service) Get(ctx context.Context, userID, id string) (*model.Project, *model.Version, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil, apperror.Validation("Invalid project ID")
	}
	return s.repo.FindWithLatest(ctx, userID, id)
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return apperror.Validation("Invalid project ID")
	}
	paths, _, err := s.repo.Delete(ctx, userID, id)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			s.logger.Warn("failed to remove attachment file", "path", p, "error", err)
		}
	}
	return nil
}

func (s *service) UpdateTitle(ctx context.Context, userID, id, title string) (*model.Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, apperror.ValidationFields(map[string]string{"title": "is required"})
	}
	if fields := model.Validate(model.UpdateTitleRequest{Title: title}); fields != nil {
		return nil, apperror.ValidationFields(fields)
	}
	return s.repo.UpdateTitle(ctx, userID, id, title)
}

func (s *service) ListVersions(ctx context.Context, userID, projectID string) ([]*model.Version, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	return s.repo.ListVersions(ctx, userID, projectID)
}

func (s *service) GetVersion(ctx context.Context, userID, projectID string, number int) (*model.Version, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	if number < 1 {
		return nil, apperror.Validation("Invalid version number")
	}
	return s.repo.FindVersion(ctx, userID, projectID, number)
}

func (s *service) ExportTarget(ctx context.Context, userID, projectID string, version *int) (*model.Project, *model.Version, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, nil, apperror.Validation("Invalid project ID")
	}
	p, err := s.repo.FindByID(ctx, userID, projectID)
	if err != nil {
		return nil, nil, err
	}
	var v *model.Version
	if version != nil {
		if *version < 1 {
			return nil, nil, apperror.Validation("Invalid version number")
		}
		v, err = s.repo.FindVersion(ctx, userID, projectID, *version)
	} else {
		v, err = s.repo.LatestVersion(ctx, p.ID)
	}
	if err != nil {
		return nil, nil, err
	}
	return p, v, nil
}

func (s *service) ListAttachments(ctx context.Context, userID, projectID string) ([]*model.ProjectAttachment, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	return s.repo.ListAttachments(ctx, userID, projectID)
}

func (s *service) GetAttachment(ctx context.Context, userID, projectID, attachmentID string) (*model.ProjectAttachment, io.ReadCloser, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, nil, apperror.Validation("Invalid project ID")
	}
	if _, err := uuid.Parse(attachmentID); err != nil {
		return nil, nil, apperror.Validation("Invalid attachment ID")
	}
	att, storagePath, err := s.repo.FindAttachment(ctx, userID, projectID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(storagePath)
	if err != nil {
		s.logger.Error("attachment file missing on disk", "attachment_id", attachmentID, "error", err)
		return nil, nil, apperror.NotFound("Attachment not found", nil)
	}
	return att, f, nil
}

// decodeImages validates count, base64-decodes, and MIME-normalizes payloads.
func (s *service) decodeImages(imgs []model.ImagePayload) ([]decodedImage, error) {
	if len(imgs) > s.projectCfg.MaxImages {
		return nil, apperror.Validation(fmt.Sprintf("Too many images: maximum %d per request", s.projectCfg.MaxImages))
	}
	out := make([]decodedImage, 0, len(imgs))
	for i, im := range imgs {
		raw, err := base64.StdEncoding.DecodeString(im.Data)
		if err != nil {
			// Tolerate unpadded encoding before giving up.
			raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(im.Data))
			if err != nil {
				return nil, apperror.Validation(fmt.Sprintf("Image %d: invalid base64 data", i+1))
			}
		}
		mime, name, ftype, err := normalizeImage(im.FileName, im.MimeType, raw, s.uploadCfg.MaxSizeBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, decodedImage{fileName: name, mimeType: mime, fileType: ftype, data: raw})
	}
	return out, nil
}

// candidateModels builds the deduplicated list of AI models to attempt in order
// (primary model followed by configured fallback models).
func (s *service) candidateModels() []string {
	models := make([]string, 0, 1+len(s.aiCfg.FallbackModels))
	if m := strings.TrimSpace(s.aiCfg.Model); m != "" {
		models = append(models, m)
	}
	for _, fb := range s.aiCfg.FallbackModels {
		fb = strings.TrimSpace(fb)
		if fb == "" {
			continue
		}
		duplicate := false
		for _, existing := range models {
			if strings.EqualFold(existing, fb) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			models = append(models, fb)
		}
	}
	if len(models) == 0 {
		return []string{""}
	}
	return models
}

// thinkingLevelFor resolves the per-model override, then the global level.
// Levels that the model is documented not to support are omitted instead of
// being sent (Gemini 3.8/3.7 Flash reject MINIMAL with HTTP 400).
func (s *service) thinkingLevelFor(modelName string) *string {
	level, configured, supported := s.aiCfg.ThinkingLevelFor(modelName)
	if !configured {
		return nil
	}
	if !supported {
		s.logger.Warn("ai thinking level not supported by model, omitting",
			"model", modelName, "thinking_level", level)
		return nil
	}
	return &level
}

// smallJitter returns 0..20% of d (capped at 500ms) so retries do not
// synchronize across concurrent requests.
func smallJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	window := d / 5
	if window > 500*time.Millisecond {
		window = 500 * time.Millisecond
	}
	if window <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(window) + 1))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// OpenStream opens the provider stream for a prepared pending request. It
// retries overloaded responses on the same model (up to AI_RETRY_ON_UNAVAILABLE
// with jittered backoff), moves to the next fallback model for other failures,
// and never retries a response-header timeout on the same model. The caller's
// context is expected to carry the AI_OPEN_BUDGET deadline.
//
// When AI_HEDGE_AFTER > 0 and a fallback exists, the first fallback model is
// started in parallel if the primary has not produced its first chunk in time.
// The first candidate to produce a chunk wins; the other is cancelled.
func (s *service) OpenStream(ctx context.Context, pending *PendingStream, observe OpenObserver) error {
	if pending == nil || pending.Project == nil {
		return apperror.AIProviderReason(apperror.ReasonProviderError, "AI request is not prepared", nil)
	}
	if s.provider == nil {
		s.failDetached(ctx, pending)
		return apperror.AIProviderReason(apperror.ReasonProviderError, "AI provider is not configured", nil)
	}

	converted := make([]aiprovider.ImageAttachment, 0, len(pending.providerImages))
	for _, im := range pending.providerImages {
		converted = append(converted, aiprovider.ImageAttachment{MimeType: im.MimeType, Data: im.Data})
	}

	models := s.candidateModels()
	maxRetries := s.aiCfg.RetryOnUnavailable
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries > 2 {
		maxRetries = 2
	}
	openStart := time.Now()

	// Skip the initial hedge when either candidate is already known to be out
	// of daily quota; openSequential below will bypass exhausted models.
	if s.aiCfg.HedgeAfter > 0 && len(models) > 1 &&
		!s.quota.isExhausted(models[0]) && !s.quota.isExhausted(models[1]) {
		lastErr, attempts := s.openHedged(ctx, pending, observe, models[0], models[1], converted, maxRetries, openStart)
		if lastErr == nil {
			pending.TotalAttempts = attempts
			return nil
		}
		if ctx.Err() != nil {
			pending.TotalAttempts = attempts
			s.failDetached(ctx, pending)
			return apperror.AIBusy(OpenBusyMessage, ctx.Err())
		}
		// Both hedge candidates failed. The remaining fallbacks (if any)
		// still run sequentially, never re-running the hedged fallback.
		if len(models) > 2 {
			seqErr, seqAttempts := s.openSequential(ctx, pending, observe, models[2:], 2, models[0], converted, maxRetries, openStart)
			pending.TotalAttempts = attempts + seqAttempts
			if seqErr == nil {
				return nil
			}
			lastErr = seqErr
		} else {
			pending.TotalAttempts = attempts
		}
		s.failDetached(ctx, pending)
		return s.openFailure(ctx, lastErr, models)
	}

	lastErr, attempts := s.openSequential(ctx, pending, observe, models, 0, models[0], converted, maxRetries, openStart)
	pending.TotalAttempts = attempts
	if lastErr == nil && ctx.Err() == nil {
		return nil
	}
	s.failDetached(ctx, pending)
	return s.openFailure(ctx, lastErr, models)
}

func (s *service) openFailure(ctx context.Context, lastErr error, models []string) error {
	if ctx.Err() != nil {
		return apperror.AIBusy(OpenBusyMessage, ctx.Err())
	}
	if s.quota.allExhausted(models) {
		return quotaBusyError(lastErr)
	}
	if lastErr == nil {
		lastErr = apperror.AIProviderReason(apperror.ReasonProviderError, "The AI provider returned an error", nil)
	}
	if aiprovider.IsOverloadedError(lastErr) || aiprovider.IsOpenTimeoutError(lastErr) {
		return apperror.AIBusy(OpenBusyMessage, lastErr)
	}
	return lastErr
}

// openSequential tries models in order, retrying only 429/503 on each model.
// It returns the last error (nil on success) and the number of provider calls.
func (s *service) openSequential(
	ctx context.Context,
	pending *PendingStream,
	observe OpenObserver,
	models []string,
	modelStart int,
	primaryModel string,
	converted []aiprovider.ImageAttachment,
	maxRetries int,
	openStart time.Time,
) (error, int) {
	var lastErr error
	totalAttempts := 0

	for localIdx, modelName := range models {
		if ctx.Err() != nil {
			break
		}
		if s.quota.isExhausted(modelName) {
			s.logger.Info("ai stream skipping model with exhausted daily quota", "model", modelName)
			continue
		}
		modelIdx := modelStart + localIdx
		fallback := modelIdx > 0
		level := s.thinkingLevelFor(modelName)
		req := s.providerRequest(pending, modelName, level, converted)

		attempts := maxRetries + 1
		thinkingRetried := false
		quotaRetried := false
		attempt := 0
		for attempt < attempts {
			if ctx.Err() != nil {
				break
			}
			phase := OpenPhaseConnecting
			if fallback {
				phase = OpenPhaseFallback
			} else if attempt > 0 {
				phase = OpenPhaseRetrying
			}
			if observe != nil {
				observe(OpenPhaseEvent{Phase: phase, Attempt: attempt + 1, Elapsed: time.Since(openStart)})
			}
			totalAttempts++
			stream, err := s.provider.CompleteStream(ctx, req)
			if err == nil {
				s.setOpenWinner(pending, modelName, level, fallback, attempt+1, time.Since(openStart))
				pending.Stream = stream
				attrs := s.openLogAttrs(pending, totalAttempts)
				if fallback {
					s.logger.Info("ai stream opened using fallback model", append(attrs, "primary_model", primaryModel, "fallback_index", modelIdx)...)
				} else if attempt > 0 {
					s.logger.Info("ai stream open retry succeeded", attrs...)
				} else {
					s.logger.Info("ai stream opened", attrs...)
				}
				return nil, totalAttempts
			}
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			if s.handleThinkingRejection(err, &thinkingRetried, modelName, level, &req) {
				continue
			}
			if s.recordDailyQuota(err, modelName) {
				// Daily quota does not recover within a normal retry: move on
				// to the next model immediately.
				break
			}
			if delay, ok := temporaryQuotaRetry(err); ok && !quotaRetried {
				quotaRetried = true
				s.logger.Warn("ai stream open temporary quota, retrying after retry delay",
					"model", modelName, "retry_delay", delay.String(), "error", err)
				if !sleepCtx(ctx, delay) {
					break
				}
				continue
			}
			if aiprovider.IsOpenTimeoutError(err) || !aiprovider.IsOverloadedError(err) {
				break
			}
			attempt++
			if attempt < attempts {
				delay := s.retryDelay()
				s.logger.Warn("ai stream open overloaded, retrying",
					"model", modelName, "attempt", attempt, "max_attempts", attempts,
					"backoff_ms", delay.Milliseconds(), "error", err)
				if !sleepCtx(ctx, delay) {
					break
				}
			} else {
				s.logger.Warn("ai stream open retries exhausted",
					"model", modelName, "attempts", attempts, "error", err)
			}
		}
		if ctx.Err() != nil {
			break
		}
		if localIdx+1 < len(models) {
			s.logger.Warn("ai stream failed on model, attempting fallback",
				"failed_model", modelName, "next_model", models[localIdx+1], "error", lastErr)
		}
	}
	return lastErr, totalAttempts
}

type hedgeResult struct {
	fallback bool
	chunk    aiprovider.StreamChunk
	stream   <-chan aiprovider.StreamChunk
	model    string
	attempt  int
	level    string
	err      error
}

func hedgeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

// openHedged races the primary model against the first fallback. The fallback
// starts when the primary has not produced a first chunk within AI_HEDGE_AFTER,
// or immediately after the primary exhausts its retries. Exactly one fallback
// worker is ever started.
func (s *service) openHedged(
	ctx context.Context,
	pending *PendingStream,
	observe OpenObserver,
	primaryModel string,
	fallbackModel string,
	converted []aiprovider.ImageAttachment,
	maxRetries int,
	openStart time.Time,
) (error, int) {
	primaryCtx, cancelPrimary := hedgeContext(ctx)
	var fallbackCtx context.Context
	var cancelFallback context.CancelFunc

	resCh := make(chan hedgeResult, 4)
	var attempts atomic.Int32
	go s.openHedgeCandidate(primaryCtx, pending, observe, primaryModel, false, converted, maxRetries, openStart, &attempts, resCh)

	startedFallback := false
	hedgeTriggered := false
	var hedgeStarted time.Duration
	startFallback := func(byTimer bool) {
		if startedFallback {
			return
		}
		startedFallback = true
		if byTimer {
			hedgeTriggered = true
			hedgeStarted = time.Since(openStart)
			s.logger.Warn("ai stream hedge started",
				"primary_model", primaryModel,
				"fallback_model", fallbackModel,
				"hedge_after_ms", s.aiCfg.HedgeAfter.Milliseconds(),
				"elapsed_ms", hedgeStarted.Milliseconds())
		}
		fallbackCtx, cancelFallback = hedgeContext(ctx)
		go s.openHedgeCandidate(fallbackCtx, pending, observe, fallbackModel, true, converted, maxRetries, openStart, &attempts, resCh)
	}

	timer := time.NewTimer(s.aiCfg.HedgeAfter)
	defer timer.Stop()

	failures := 0
	primaryDone := false
	fallbackDone := false
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			cancelPrimary()
			if cancelFallback != nil {
				cancelFallback()
			}
			return ctx.Err(), int(attempts.Load())
		case <-timer.C:
			startFallback(true)
		case res := <-resCh:
			if res.fallback {
				fallbackDone = true
			} else {
				primaryDone = true
			}
			if res.err == nil {
				loserCancelled := false
				winnerCtx := primaryCtx
				if res.fallback {
					winnerCtx = fallbackCtx
					if !primaryDone {
						cancelPrimary()
						loserCancelled = true
					}
				} else if startedFallback && !fallbackDone {
					cancelFallback()
					loserCancelled = true
				}

				pending.Stream = prependStream(winnerCtx, res.chunk, res.stream)
				s.setOpenWinner(pending, res.model, levelPtr(res.level), res.fallback, res.attempt, time.Since(openStart))
				pending.LoserCancelled = loserCancelled
				if hedgeTriggered {
					pending.HedgeStarted = hedgeStarted
					if res.fallback {
						pending.HedgeWinner = "fallback"
					} else {
						pending.HedgeWinner = "primary"
					}
				}
				attrs := s.openLogAttrs(pending, int(attempts.Load()))
				if hedgeTriggered {
					attrs = append(attrs,
						"hedge_started_ms", hedgeStarted.Milliseconds(),
						"hedge_winner", pending.HedgeWinner,
						"loser_cancelled", loserCancelled)
					s.logger.Info("ai stream opened with hedging", attrs...)
				} else if res.fallback {
					s.logger.Info("ai stream opened using fallback model", append(attrs, "primary_model", primaryModel)...)
				} else {
					s.logger.Info("ai stream opened", attrs...)
				}
				return nil, int(attempts.Load())
			}

			failures++
			lastErr = res.err
			if !res.fallback && !startedFallback {
				// The primary exhausted its retries before the hedge timer:
				// run the normal fallback immediately.
				startFallback(false)
			}
			if failures >= 2 {
				cancelPrimary()
				if cancelFallback != nil {
					cancelFallback()
				}
				return lastErr, int(attempts.Load())
			}
		}
	}
}

// openHedgeCandidate opens one model (with the same retry policy as the
// sequential path) and reports either its first stream chunk or a terminal
// open failure. It sends exactly one result.
func (s *service) openHedgeCandidate(
	ctx context.Context,
	pending *PendingStream,
	observe OpenObserver,
	modelName string,
	fallback bool,
	converted []aiprovider.ImageAttachment,
	maxRetries int,
	openStart time.Time,
	attempts *atomic.Int32,
	out chan<- hedgeResult,
) {
	level := s.thinkingLevelFor(modelName)
	req := s.providerRequest(pending, modelName, level, converted)
	allowed := maxRetries + 1
	thinkingRetried := false
	quotaRetried := false
	attempt := 0
	var lastErr error

	fail := func(err error) {
		out <- hedgeResult{fallback: fallback, model: modelName, err: err}
	}

	for attempt < allowed {
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		phase := OpenPhaseConnecting
		if fallback {
			phase = OpenPhaseFallback
		} else if attempt > 0 {
			phase = OpenPhaseRetrying
		}
		if observe != nil {
			observe(OpenPhaseEvent{Phase: phase, Attempt: attempt + 1, Elapsed: time.Since(openStart)})
		}
		attempts.Add(1)
		stream, err := s.provider.CompleteStream(ctx, req)
		if err == nil {
			select {
			case chunk, ok := <-stream:
				if !ok {
					fail(apperror.AIProviderReason(apperror.ReasonStreamInterrupted, "AI stream ended before the first chunk", nil))
					return
				}
				if chunk.Err != nil {
					fail(chunk.Err)
					return
				}
				levelName := "default"
				if level != nil {
					levelName = *level
				}
				out <- hedgeResult{
					fallback: fallback,
					chunk:    chunk,
					stream:   stream,
					model:    modelName,
					attempt:  attempt + 1,
					level:    levelName,
				}
				return
			case <-ctx.Done():
				fail(ctx.Err())
				return
			}
		}
		lastErr = err
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		if s.handleThinkingRejection(err, &thinkingRetried, modelName, level, &req) {
			continue
		}
		if s.recordDailyQuota(err, modelName) {
			fail(err)
			return
		}
		if delay, ok := temporaryQuotaRetry(err); ok && !quotaRetried {
			quotaRetried = true
			s.logger.Warn("ai stream open temporary quota, retrying after retry delay",
				"model", modelName, "retry_delay", delay.String(), "error", err)
			if !sleepCtx(ctx, delay) {
				fail(ctx.Err())
				return
			}
			continue
		}
		if aiprovider.IsOpenTimeoutError(err) || !aiprovider.IsOverloadedError(err) {
			fail(err)
			return
		}
		attempt++
		if attempt < allowed {
			delay := s.retryDelay()
			s.logger.Warn("ai stream open overloaded, retrying",
				"model", modelName, "attempt", attempt, "max_attempts", allowed,
				"backoff_ms", delay.Milliseconds(), "error", err)
			if !sleepCtx(ctx, delay) {
				fail(ctx.Err())
				return
			}
		} else {
			s.logger.Warn("ai stream open retries exhausted",
				"model", modelName, "attempts", allowed, "error", err)
		}
	}
	fail(lastErr)
}

func (s *service) providerRequest(pending *PendingStream, modelName string, level *string, converted []aiprovider.ImageAttachment) aiprovider.Request {
	return aiprovider.Request{
		Model: modelName,
		Messages: []aiprovider.ChatMessage{
			{Role: "system", Content: pending.systemPrompt},
			{Role: "user", Content: pending.userPrompt, Images: converted},
		},
		ThinkingLevel: level,
	}
}

func (s *service) handleThinkingRejection(err error, done *bool, modelName string, level *string, req *aiprovider.Request) bool {
	if *done || !aiprovider.IsThinkingLevelError(err) {
		return false
	}
	thinking := "default"
	if level != nil {
		thinking = *level
	}
	s.logger.Warn("ai provider rejected thinking level, retrying without it",
		"model", modelName, "thinking_level", thinking, "error", err)
	*done = true
	req.DisableThinking = true
	req.ThinkingLevel = nil
	return true
}

func (s *service) retryDelay() time.Duration {
	delay := s.aiCfg.RetryBackoff
	if delay > 0 {
		delay += smallJitter(delay)
	}
	return delay
}

func (s *service) setOpenWinner(pending *PendingStream, modelName string, level *string, fallback bool, attempt int, duration time.Duration) {
	pending.Model = modelName
	pending.FallbackUsed = fallback
	pending.AttemptNumber = attempt
	pending.OpenDuration = duration
	if level != nil {
		pending.ThinkingLevel = *level
	} else {
		pending.ThinkingLevel = "default"
	}
}

func (s *service) openLogAttrs(pending *PendingStream, attempts int) []any {
	return []any{
		"model", pending.Model,
		"attempt", pending.AttemptNumber,
		"attempts", attempts,
		"fallback_used", pending.FallbackUsed,
		"thinking_level", pending.ThinkingLevel,
		"open_ms", pending.OpenDuration.Milliseconds(),
	}
}

func levelPtr(level string) *string {
	if level == "" || level == "default" {
		return nil
	}
	return &level
}

// prependStream re-emits the already-consumed first chunk, then relays the
// rest of the winner stream. It stops when ctx is cancelled so a disconnected
// client cannot leak the relay goroutine.
func prependStream(ctx context.Context, first aiprovider.StreamChunk, rest <-chan aiprovider.StreamChunk) <-chan aiprovider.StreamChunk {
	out := make(chan aiprovider.StreamChunk, 1)
	go func() {
		defer close(out)
		select {
		case out <- first:
		case <-ctx.Done():
			return
		}
		for {
			select {
			case chunk, ok := <-rest:
				if !ok {
					return
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (s *service) PrepareGenerate(ctx context.Context, userID, projectID string, imgs []model.ImagePayload) (*PendingStream, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	decoded, err := s.decodeImages(imgs)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.TryStartGenerate(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	atts, provImgs, err := storeImages(s.uploadCfg.Dir, userID, p.ID, decoded)
	if err != nil {
		s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindGenerate})
		return nil, err
	}
	if len(atts) > 0 {
		if err := s.repo.SaveAttachments(ctx, p.ID, atts); err != nil {
			s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindGenerate})
			return nil, err
		}
	}
	briefText := FormatBrief(p.Brief.Description, p.Brief.WebsiteType, p.Brief.BusinessName, p.Brief.Style, p.Brief.ColorPalette, p.Brief.Language, p.Brief.Sections)
	return &PendingStream{
		Project: p, Kind: model.KindGenerate, Prompt: p.Brief.Description,
		StartedAt: time.Now(), MaxBytes: s.projectCfg.MaxHTMLBytes,
		systemPrompt: SystemPrompt, userPrompt: BuildGeneratePrompt(briefText), providerImages: provImgs,
	}, nil
}

func (s *service) PrepareRevise(ctx context.Context, userID, projectID, instruction string, baseVersion *int, imgs []model.ImagePayload) (*PendingStream, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, apperror.Validation("Invalid project ID")
	}
	if strings.TrimSpace(instruction) == "" {
		return nil, apperror.ValidationFields(map[string]string{"instruction": "is required"})
	}
	if len(instruction) > 4000 {
		return nil, apperror.ValidationFields(map[string]string{"instruction": "must be at most 4000 characters"})
	}
	decoded, err := s.decodeImages(imgs)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.TryStartRevise(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	// Resolve the base HTML before claiming images.
	var base *model.Version
	if baseVersion != nil {
		if *baseVersion < 1 {
			s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindRevise})
			return nil, apperror.Validation("Invalid base version")
		}
		base, err = s.repo.FindVersion(ctx, userID, p.ID, *baseVersion)
	} else {
		base, err = s.repo.LatestVersion(ctx, p.ID)
	}
	if err != nil {
		s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindRevise})
		return nil, err
	}
	atts, provImgs, err := storeImages(s.uploadCfg.Dir, userID, p.ID, decoded)
	if err != nil {
		s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindRevise})
		return nil, err
	}
	if len(atts) > 0 {
		if err := s.repo.SaveAttachments(ctx, p.ID, atts); err != nil {
			s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindRevise})
			return nil, err
		}
	}
	briefText := FormatBrief(p.Brief.Description, p.Brief.WebsiteType, p.Brief.BusinessName, p.Brief.Style, p.Brief.ColorPalette, p.Brief.Language, p.Brief.Sections)
	return &PendingStream{
		Project: p, Kind: model.KindRevise, Prompt: instruction,
		StartedAt: time.Now(), MaxBytes: s.projectCfg.MaxHTMLBytes,
		systemPrompt: SystemPrompt, userPrompt: BuildRevisePrompt(briefText, base.HTML, instruction), providerImages: provImgs,
	}, nil
}

func (s *service) BeginGenerate(ctx context.Context, userID, projectID string, imgs []model.ImagePayload) (*PendingStream, error) {
	pending, err := s.PrepareGenerate(ctx, userID, projectID, imgs)
	if err != nil {
		return nil, err
	}
	if err := s.OpenStream(ctx, pending, nil); err != nil {
		return nil, err
	}
	return pending, nil
}

func (s *service) BeginRevise(ctx context.Context, userID, projectID, instruction string, baseVersion *int, imgs []model.ImagePayload) (*PendingStream, error) {
	pending, err := s.PrepareRevise(ctx, userID, projectID, instruction, baseVersion, imgs)
	if err != nil {
		return nil, err
	}
	if err := s.OpenStream(ctx, pending, nil); err != nil {
		return nil, err
	}
	return pending, nil
}

func (s *service) FinishStream(ctx context.Context, pending *PendingStream, rawBuffer string) (*model.Version, error) {
	// Per-stream diagnostics (Task 2, project half): every terminal path
	// logs chunks, text bytes, raw finish_reason, stream end type, block
	// reason, usageMetadata, duration and project_id. Never the HTML body.
	durationMs := time.Since(pending.StartedAt).Milliseconds()
	if isTruncated(pending.FinishReason) {
		attrs := []any{
			"project_id", pending.Project.ID,
			"kind", pending.Kind,
			"stream_end", "eof",
			"duration_ms", durationMs,
			"chunks", pending.Chunks,
			"bytes", len(rawBuffer),
			"finish_reason", pending.FinishReason,
			"block_reason", pending.BlockReason,
		}
		attrs = appendUsage(attrs, pending.Usage)
		s.logger.Error("project generation truncated by provider", attrs...)
		s.failDetached(ctx, pending)
		return nil, apperror.AIProviderReason(apperror.ReasonOutputTruncated, "AI output was truncated before completing the HTML document", nil)
	}
	// A safety refusal can arrive as candidates[].finishReason (SAFETY, ...)
	// or as promptFeedback.blockReason with no candidates at all. Either
	// one means "blocked", distinct from truncation or a clean STOP.
	if isBlocked(pending.FinishReason) || isBlocked(pending.BlockReason) {
		attrs := []any{
			"project_id", pending.Project.ID,
			"kind", pending.Kind,
			"stream_end", "eof",
			"duration_ms", durationMs,
			"chunks", pending.Chunks,
			"bytes", len(rawBuffer),
			"finish_reason", pending.FinishReason,
			"block_reason", pending.BlockReason,
		}
		attrs = appendUsage(attrs, pending.Usage)
		s.logger.Error("project generation blocked by provider", attrs...)
		s.failDetached(ctx, pending)
		return nil, apperror.AIProviderReason(apperror.ReasonOutputBlocked, "AI output was blocked by provider safety filters", nil)
	}
	if !isCleanStop(pending.FinishReason) {
		attrs := []any{
			"project_id", pending.Project.ID,
			"kind", pending.Kind,
			"stream_end", "eof",
			"duration_ms", durationMs,
			"chunks", pending.Chunks,
			"bytes", len(rawBuffer),
			"finish_reason", pending.FinishReason,
			"block_reason", pending.BlockReason,
		}
		attrs = appendUsage(attrs, pending.Usage)
		s.logger.Error("project stream ended unexpectedly", attrs...)
		s.failDetached(ctx, pending)
		return nil, apperror.AIProviderReason(apperror.ReasonStreamInterrupted, "AI stream ended unexpectedly without a finish reason", nil)
	}
	doc, err := CleanAndValidate(rawBuffer, pending.MaxBytes)
	if err != nil {
		lower := strings.ToLower(strings.TrimSpace(rawBuffer))
		attrs := []any{
			"project_id", pending.Project.ID,
			"kind", pending.Kind,
			"stream_end", "eof",
			"duration_ms", durationMs,
			"finish_reason", pending.FinishReason,
			"block_reason", pending.BlockReason,
			"chunks", pending.Chunks,
			"bytes", len(rawBuffer),
			"has_fence_prefix", strings.HasPrefix(lower, "```"),
			"has_closing_html", strings.Contains(lower, "</html>"),
			"error", err,
		}
		attrs = appendUsage(attrs, pending.Usage)
		if s.aiCfg.DebugLogOutputEdges {
			attrs = append(attrs, "output_head", headEdge(rawBuffer), "output_tail", tailEdge(rawBuffer))
		}
		s.logger.Error("project generation failed validation", attrs...)
		s.failDetached(ctx, pending)
		return nil, err
	}
	v, err := s.repo.FinishSuccess(ctx, pending.Project.ID, pending.Kind, pending.Prompt, doc)
	if err != nil {
		s.failDetached(ctx, pending)
		return nil, err
	}
	attrs := []any{
		"project_id", pending.Project.ID,
		"version_number", v.VersionNumber,
		"kind", pending.Kind,
		"stream_end", "eof",
		"duration_ms", time.Since(pending.StartedAt).Milliseconds(),
		"chunks", pending.Chunks,
		"bytes", len(doc),
		"finish_reason", pending.FinishReason,
		"block_reason", pending.BlockReason,
	}
	attrs = appendUsage(attrs, pending.Usage)
	s.logger.Info("project generation finished", attrs...)
	return v, nil
}

// isTruncated reports whether the provider stopped for running out of
// output budget (Gemini MAX_TOKENS) rather than finishing cleanly.
func isTruncated(finishReason string) bool {
	return strings.EqualFold(strings.TrimSpace(finishReason), "MAX_TOKENS")
}

// blockedFinishReasons are terminal states where the provider refused
// content (safety filters, recitation, blocklists). They are distinct from
// truncation: retrying the same prompt will not help.
var blockedFinishReasons = map[string]bool{
	"SAFETY": true, "RECITATION": true, "BLOCKLIST": true,
	"PROHIBITED_CONTENT": true, "SPII": true,
}

// isBlocked reports whether a finish or block reason is a provider content
// refusal. Both candidates[].finishReason and promptFeedback.blockReason
// share the same vocabulary (SAFETY, BLOCKLIST, ...); FinishStream checks
// both so a block with no candidates is still classified as blocked.
func isBlocked(reason string) bool {
	return blockedFinishReasons[strings.ToUpper(strings.TrimSpace(reason))]
}

// appendUsage appends token accounting to a log attribute slice when the
// provider reported usageMetadata. Field names match the provider's
// usageMetadata keys.
func appendUsage(attrs []any, usage *aiprovider.UsageMetadata) []any {
	if usage == nil {
		return attrs
	}
	return append(attrs,
		"prompt_tokens", usage.PromptTokenCount,
		"candidates_tokens", usage.CandidatesTokenCount,
		"thoughts_tokens", usage.ThoughtsTokenCount,
		"total_tokens", usage.TotalTokenCount,
	)
}

// isCleanStop reports whether the stream finished normally. Anything else
// (empty, OTHER, FINISH_REASON_UNSPECIFIED, unknown future values) is
// treated as an unexpected end: no version is stored.
func isCleanStop(finishReason string) bool {
	return strings.EqualFold(strings.TrimSpace(finishReason), "STOP")
}

// edgeChars bounds debug output excerpts. Only used when
// AI_DEBUG_LOG_OUTPUT_EDGES is enabled in development.
const edgeChars = 200

func headEdge(s string) string {
	if len(s) <= edgeChars {
		return s
	}
	return s[:edgeChars]
}

func tailEdge(s string) string {
	if len(s) <= edgeChars {
		return s
	}
	return s[len(s)-edgeChars:]
}

func (s *service) FailStream(ctx context.Context, pending *PendingStream) {
	if pending == nil || pending.Project == nil {
		return
	}
	cleanup, cancel := detached(ctx)
	defer cancel()
	var err error
	if pending.Kind == model.KindGenerate {
		err = s.repo.FinishFailedGenerate(cleanup, pending.Project.ID)
	} else {
		err = s.repo.FinishFailedRevise(cleanup, pending.Project.ID)
	}
	if err != nil {
		s.logger.Error("failed to roll back project status",
			"project_id", pending.Project.ID, "kind", pending.Kind, "error", err)
	} else {
		s.logger.Info("project generation rolled back",
			"project_id", pending.Project.ID, "kind", pending.Kind)
	}
}

// detached returns a context with a short timeout that survives request
// cancellation, so status cleanup always runs even on client disconnect.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

// failDetached rolls back via FailStream on a detached context and releases it.
func (s *service) failDetached(ctx context.Context, pending *PendingStream) {
	cleanup, cancel := detached(ctx)
	defer cancel()
	s.FailStream(cleanup, pending)
}

// ExportFilename builds the api.md download name: website-<slug-judul>-v<n>.html.
// Titles that already slugify with a leading "website-" (e.g. the api.md
// example "Website Kopi Senja" -> "website-kopi-senja-v2.html") are not
// double-prefixed.
func ExportFilename(title string, versionNumber int) string {
	slug := Slugify(title)
	if !strings.HasPrefix(slug, "website-") {
		slug = "website-" + slug
	}
	return fmt.Sprintf("%s-v%d.html", slug, versionNumber)
}

// Slugify builds a safe filename slug: lowercase alphanumerics + hyphens.
func Slugify(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevHyphen := true // trim leading hyphens
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "website"
	}
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	return s
}
