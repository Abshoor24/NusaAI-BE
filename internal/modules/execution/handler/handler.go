package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/modules/execution/model"
	"github.com/Abshoor24/backend-go/internal/modules/execution/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler handles HTTP requests for code execution and history.
type Handler struct {
	svc    service.Service
	logger *slog.Logger
	cfg    config.ExecutionConfig
}

// New creates a new execution Handler.
func New(svc service.Service, logger *slog.Logger, cfg config.ExecutionConfig) *Handler {
	if cfg.MaxCodeBytes <= 0 {
		cfg.MaxCodeBytes = 65536
	}
	return &Handler{
		svc:    svc,
		logger: logger,
		cfg:    cfg,
	}
}

// Execute handles POST /api/v1/execute.
func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok || strings.TrimSpace(userID) == "" {
		response.RespondError(w, h.logger, apperror.Unauthorized("User authentication is required", nil))
		return
	}

	// Limit request body size to prevent memory exhaustion attacks
	maxBodyBytes := h.cfg.MaxCodeBytes + 4096
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req model.ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondError(w, h.logger, apperror.BadRequest("Invalid JSON request body", err))
		return
	}

	// Early payload size guard at handler layer
	if int64(len([]byte(req.Code))) > h.cfg.MaxCodeBytes {
		response.RespondError(w, h.logger, apperror.Validation(fmt.Sprintf("Code size exceeds maximum limit of %d KB", h.cfg.MaxCodeBytes/1024)))
		return
	}

	res, err := h.svc.Execute(r.Context(), userID, req)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Code executed successfully", res)
}

// ListHistory handles GET /api/v1/execute/history.
func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok || strings.TrimSpace(userID) == "" {
		response.RespondError(w, h.logger, apperror.Unauthorized("User authentication is required", nil))
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	list, err := h.svc.ListExecutions(r.Context(), userID, limit, offset)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", list)
}

// GetDetail handles GET /api/v1/execute/{id}.
func (h *Handler) GetDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok || strings.TrimSpace(userID) == "" {
		response.RespondError(w, h.logger, apperror.Unauthorized("User authentication is required", nil))
		return
	}

	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		response.RespondError(w, h.logger, apperror.Validation("Execution ID is required"))
		return
	}

	detail, err := h.svc.GetExecution(r.Context(), id, userID)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", detail)
}
