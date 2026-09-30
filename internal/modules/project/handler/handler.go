package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
	"github.com/Abshoor24/backend-go/internal/modules/project/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler exposes the AI Website Generator endpoints.
type Handler struct {
	svc        service.Service
	logger     *slog.Logger
	aiCfg      config.AIConfig
	uploadCfg  config.UploadConfig
	projectCfg config.ProjectConfig
}

// New creates a project Handler.
func New(svc service.Service, logger *slog.Logger, aiCfg config.AIConfig, uploadCfg config.UploadConfig, projectCfg config.ProjectConfig) *Handler {
	if uploadCfg.MaxSizeBytes <= 0 {
		uploadCfg.MaxSizeBytes = 10 << 20
	}
	if aiCfg.MaxGenerateBodyBytes <= 0 {
		aiCfg.MaxGenerateBodyBytes = 32 << 20
	}
	return &Handler{svc: svc, logger: logger, aiCfg: aiCfg, uploadCfg: uploadCfg, projectCfg: projectCfg}
}

func userID(w http.ResponseWriter, r *http.Request, h *Handler) (string, bool) {
	uid, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return "", false
	}
	return uid, true
}

// decodeStrict parses a JSON body with unknown-field rejection. Decode-level
// failures (syntax, unknown fields, oversize) map to BAD_REQUEST per api.md;
// struct validation maps to VALIDATION_ERROR with fields.
func decodeStrict(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			// Empty body: validate the zero value (generate allows {}, revise
			// will fail on missing instruction).
		} else {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return apperror.BadRequest("Request body is too large", err)
			}
			return apperror.BadRequest("Request body is not valid JSON", err)
		}
	} else if dec.More() {
		return apperror.BadRequest("Request body must contain a single JSON object", nil)
	}
	if fields := model.Validate(dst); fields != nil {
		return apperror.ValidationFields(fields)
	}
	return nil
}

// Create handles POST /api/v1/projects.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	var req model.CreateProjectRequest
	if err := decodeStrict(w, r, &req, h.aiCfg.MaxBytes); err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	p, err := h.svc.Create(r.Context(), uid, req)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	response.RespondSuccess(w, http.StatusCreated, "Project created", model.ToProjectSummaryResponse(p))
}

// List handles GET /api/v1/projects. Invalid limit/offset fall back to defaults.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	limit, offset := 20, 0
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			limit = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	projects, err := h.svc.List(r.Context(), uid, limit, offset)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	out := make([]model.ProjectSummaryResponse, 0, len(projects))
	for _, p := range projects {
		out = append(out, model.ToProjectSummaryResponse(p))
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", out)
}

// Get handles GET /api/v1/projects/{id} with the latest version + HTML.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	p, latest, err := h.svc.Get(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", model.ToProjectResponse(p, latest))
}

// Delete handles DELETE /api/v1/projects/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id"))); err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	response.RespondSuccess(w, http.StatusOK, "Project deleted", nil)
}

// UpdateTitle handles PATCH /api/v1/projects/{id}.
func (h *Handler) UpdateTitle(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	var req model.UpdateTitleRequest
	if err := decodeStrict(w, r, &req, h.aiCfg.MaxBytes); err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	p, err := h.svc.UpdateTitle(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")), req.Title)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	response.RespondSuccess(w, http.StatusOK, "Project renamed", model.ToProjectSummaryResponse(p))
}

// ListVersions handles GET /api/v1/projects/{id}/versions (no HTML).
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	versions, err := h.svc.ListVersions(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	out := make([]model.VersionResponse, 0, len(versions))
	for _, v := range versions {
		out = append(out, model.ToVersionResponse(v, false))
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", out)
}

// GetVersion handles GET /api/v1/projects/{id}/versions/{number} (with HTML).
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	num, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "number")))
	if err != nil || num < 1 {
		response.RespondError(w, h.logger, apperror.Validation("Invalid version number"))
		return
	}
	v, err := h.svc.GetVersion(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")), num)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", model.ToVersionResponse(v, true))
}

// Export handles GET /api/v1/projects/{id}/export — raw HTML download.
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	var version *int
	if v := strings.TrimSpace(r.URL.Query().Get("version")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			response.RespondError(w, h.logger, apperror.Validation("Invalid version number"))
			return
		}
		version = &n
	}
	p, ver, err := h.svc.ExportTarget(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")), version)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	filename := service.ExportFilename(p.Title, ver.VersionNumber)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(ver.HTML)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ver.HTML)
}

// ListAttachments handles GET /api/v1/projects/{id}/attachments.
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	atts, err := h.svc.ListAttachments(r.Context(), uid, strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	out := make([]model.ProjectAttachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, *a)
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", out)
}

// GetAttachment handles GET /api/v1/projects/{id}/attachments/{attachmentId}.
func (h *Handler) GetAttachment(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	att, file, err := h.svc.GetAttachment(r.Context(), uid,
		strings.TrimSpace(chi.URLParam(r, "id")),
		strings.TrimSpace(chi.URLParam(r, "attachmentId")))
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", att.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", att.FileName))
	w.Header().Set("Content-Length", strconv.FormatInt(att.FileSize, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, file)
}

// --- Generate & Revise (SSE) ---

// multipartImages extracts reference images from a parsed multipart form.
func (h *Handler) multipartImages(r *http.Request) ([]model.ImagePayload, error) {
	var out []model.ImagePayload
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	var headers []*multipart.FileHeader
	for _, field := range []string{"image", "images", "file", "files"} {
		headers = append(headers, r.MultipartForm.File[field]...)
	}
	maxSize := h.uploadCfg.MaxSizeBytes
	for _, fh := range headers {
		if fh.Size > maxSize {
			return nil, apperror.Validation(fmt.Sprintf("Image size exceeds maximum allowed limit of %d MB", maxSize/(1<<20)))
		}
		f, err := fh.Open()
		if err != nil {
			return nil, apperror.Validation("Failed to read attached file: " + err.Error())
		}
		data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
		f.Close()
		if err != nil {
			return nil, apperror.Internal("Failed to read uploaded file", err)
		}
		if int64(len(data)) > maxSize {
			return nil, apperror.Validation(fmt.Sprintf("Image size exceeds maximum allowed limit of %d MB", maxSize/(1<<20)))
		}
		out = append(out, model.ImagePayload{
			FileName: fh.Filename,
			MimeType: fh.Header.Get("Content-Type"),
			Data:     b64(data),
		})
	}
	return out, nil
}

// Generate handles POST /api/v1/projects/{id}/generate (SSE).
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "id"))
	var images []model.ImagePayload
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			response.RespondError(w, h.logger, apperror.Validation("Failed to parse multipart form: "+err.Error()))
			return
		}
		var err error
		images, err = h.multipartImages(r)
		if err != nil {
			response.RespondError(w, h.logger, err)
			return
		}
	} else {
		var req model.GenerateRequest
		if err := decodeStrict(w, r, &req, h.aiCfg.MaxGenerateBodyBytes); err != nil {
			response.RespondError(w, h.logger, err)
			return
		}
		images = req.Images
	}

	// Open the provider stream BEFORE committing to SSE headers so an early
	// provider failure is still a JSON 502, not a broken stream.
	pending, err := h.svc.BeginGenerate(r.Context(), uid, projectID, images)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	h.streamPending(w, r, pending)
}

// Revise handles POST /api/v1/projects/{id}/revise (SSE).
func (h *Handler) Revise(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r, h)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "id"))
	var instruction string
	var baseVersion *int
	var images []model.ImagePayload
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			response.RespondError(w, h.logger, apperror.Validation("Failed to parse multipart form: "+err.Error()))
			return
		}
		instruction = strings.TrimSpace(r.FormValue("instruction"))
		if v := strings.TrimSpace(r.FormValue("base_version")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				response.RespondError(w, h.logger, apperror.Validation("Invalid base version"))
				return
			}
			baseVersion = &n
		}
		var err error
		images, err = h.multipartImages(r)
		if err != nil {
			response.RespondError(w, h.logger, err)
			return
		}
		if instruction == "" {
			response.RespondError(w, h.logger, apperror.ValidationFields(map[string]string{"instruction": "is required"}))
			return
		}
		if len(instruction) > 4000 {
			response.RespondError(w, h.logger, apperror.ValidationFields(map[string]string{"instruction": "must be at most 4000 characters"}))
			return
		}
	} else {
		var req model.ReviseRequest
		if err := decodeStrict(w, r, &req, h.aiCfg.MaxGenerateBodyBytes); err != nil {
			response.RespondError(w, h.logger, err)
			return
		}
		instruction = req.Instruction
		baseVersion = req.BaseVersion
		images = req.Images
	}

	pending, err := h.svc.BeginRevise(r.Context(), uid, projectID, instruction, baseVersion, images)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}
	h.streamPending(w, r, pending)
}

// streamPending relays the provider stream as SSE token events, then stores
// the version and emits done — or emits error with no version stored.
// A heartbeat comment is emitted while waiting so idle streams survive
// proxies; every write refreshes the write deadline.
func (h *Handler) streamPending(w http.ResponseWriter, r *http.Request, pending *service.PendingStream) {
	sse := newSSEWriter(w)
	var buf strings.Builder
	start := time.Now()

	var tickCh <-chan time.Time
	if sse.heartbeat > 0 {
		ticker := time.NewTicker(sse.heartbeat)
		defer ticker.Stop()
		tickCh = ticker.C
	}

loop:
	for {
		select {
		case <-r.Context().Done():
			// Client gone: roll back on a detached context and stop silently.
			h.svc.FailStream(r.Context(), pending)
			return
		case <-tickCh:
			sse.writeHeartbeat()
			continue
		case chunk, ok := <-pending.Stream:
			if !ok {
				break loop
			}
			pending.Chunks++
			// Record terminal metadata for FinishStream diagnostics and
			// end-of-stream classification; harmless when empty or repeated.
			if chunk.FinishReason != "" {
				pending.FinishReason = chunk.FinishReason
			}
			if chunk.BlockReason != "" {
				pending.BlockReason = chunk.BlockReason
			}
			if chunk.Usage != nil {
				pending.Usage = chunk.Usage
			}
			if chunk.Err != nil {
				h.logger.Error("project stream chunk error",
					"project_id", pending.Project.ID, "kind", pending.Kind, "error", chunk.Err,
					"chunks", pending.Chunks, "bytes", buf.Len())
				h.svc.FailStream(r.Context(), pending)
				sse.writeEvent("error", sseErrorPayload("The AI provider returned an error"))
				return
			}
			if chunk.Done {
				break loop
			}
			if chunk.Token == "" {
				continue
			}
			if int64(buf.Len()+len(chunk.Token)) > pending.MaxBytes+64*1024 {
				// Bound streaming memory: the final validator enforces the
				// exact limit; this guard only stops unbounded growth.
				h.logger.Error("project stream exceeded buffer cap", "project_id", pending.Project.ID)
				h.svc.FailStream(r.Context(), pending)
				sse.writeEvent("error", sseErrorPayload("The AI provider returned an error"))
				return
			}
			buf.WriteString(chunk.Token)
			pending.TextBytes += len(chunk.Token)
			sse.writeEvent("token", map[string]string{"token": chunk.Token})
		}
	}

	ver, err := h.svc.FinishStream(r.Context(), pending, buf.String())
	if err != nil {
		h.logger.Error("project stream finalize failed",
			"project_id", pending.Project.ID, "kind", pending.Kind,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		sse.writeEvent("error", sseErrorPayload("The AI provider returned an error"))
		return
	}
	h.logger.Info("project stream done",
		"project_id", pending.Project.ID, "version_number", ver.VersionNumber,
		"duration_ms", time.Since(start).Milliseconds(), "bytes", len(ver.HTML))
	sse.writeEvent("done", map[string]interface{}{
		"project_id":     pending.Project.ID,
		"version_id":     ver.ID,
		"version_number": ver.VersionNumber,
	})
}
