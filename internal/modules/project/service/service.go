package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
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

	// BeginGenerate validates input, atomically claims the project
	// (draft/failed -> generating), stores attachments, and opens the
	// provider stream. A non-nil stream with nil error means the provider
	// accepted; the caller may now commit to SSE headers.
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

type service struct {
	repo       repository.Repository
	provider   aiprovider.Provider
	aiCfg      config.AIConfig
	uploadCfg  config.UploadConfig
	projectCfg config.ProjectConfig
	logger     *slog.Logger
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
	return &service{repo: repo, provider: provider, aiCfg: aiCfg, uploadCfg: uploadCfg, projectCfg: projectCfg, logger: logger}
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

func (s *service) openStream(ctx context.Context, systemPrompt, userPrompt string, imgs []providerImage) (<-chan aiprovider.StreamChunk, error) {
	if s.provider == nil {
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI provider is not configured", nil)
	}
	converted := make([]aiprovider.ImageAttachment, 0, len(imgs))
	for _, im := range imgs {
		converted = append(converted, aiprovider.ImageAttachment{MimeType: im.MimeType, Data: im.Data})
	}
	req := aiprovider.Request{
		Model: s.aiCfg.Model,
		Messages: []aiprovider.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt, Images: converted},
		},
	}
	// Limited pre-stream retry on provider overload (HTTP 429/503 before
	// any chunk exists). Default 0 keeps this a single call, byte-identical
	// in behavior to before. Never retries mid-stream errors (those arrive
	// as StreamChunk.Err after open succeeds), truncation, blocks, or
	// validation failures. The caller's context carries the overall
	// AI_STREAM_MAX_DURATION deadline and client cancellation, so retries
	// plus backoff never outlive either.
	maxRetries := s.aiCfg.RetryOnUnavailable
	if maxRetries <= 0 {
		return s.provider.CompleteStream(ctx, req)
	}
	if maxRetries > 2 {
		maxRetries = 2
	}
	var err error
	var stream <-chan aiprovider.StreamChunk
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			// Client gone or total deadline hit: stop retrying, surface
			// the last provider error so rollback still runs.
			if err != nil {
				return nil, err
			}
			return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI stream request was cancelled", ctx.Err())
		}
		stream, err = s.provider.CompleteStream(ctx, req)
		if err == nil {
			if attempt > 0 {
				s.logger.Info("ai stream open retry succeeded",
					"attempt", attempt+1, "max_attempts", maxRetries+1)
			}
			return stream, nil
		}
		if !aiprovider.IsOverloadedError(err) {
			return nil, err
		}
		if attempt == maxRetries {
			s.logger.Warn("ai stream open retries exhausted",
				"attempts", attempt+1, "max_attempts", maxRetries+1, "error", err)
			return nil, err
		}
		s.logger.Warn("ai stream open overloaded, retrying",
			"attempt", attempt+1, "max_attempts", maxRetries+1, "error", err)
		if backoff := s.aiCfg.RetryBackoff; backoff > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, err
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, err
}

func (s *service) BeginGenerate(ctx context.Context, userID, projectID string, imgs []model.ImagePayload) (*PendingStream, error) {
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
	stream, err := s.openStream(ctx, SystemPrompt, BuildGeneratePrompt(briefText), provImgs)
	if err != nil {
		s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindGenerate})
		return nil, err
	}
	return &PendingStream{Project: p, Kind: model.KindGenerate, Prompt: p.Brief.Description, Stream: stream, StartedAt: time.Now(), MaxBytes: s.projectCfg.MaxHTMLBytes}, nil
}

func (s *service) BeginRevise(ctx context.Context, userID, projectID, instruction string, baseVersion *int, imgs []model.ImagePayload) (*PendingStream, error) {
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
	stream, err := s.openStream(ctx, SystemPrompt, BuildRevisePrompt(briefText, base.HTML, instruction), provImgs)
	if err != nil {
		s.failDetached(ctx, &PendingStream{Project: p, Kind: model.KindRevise})
		return nil, err
	}
	return &PendingStream{Project: p, Kind: model.KindRevise, Prompt: instruction, Stream: stream, StartedAt: time.Now(), MaxBytes: s.projectCfg.MaxHTMLBytes}, nil
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
