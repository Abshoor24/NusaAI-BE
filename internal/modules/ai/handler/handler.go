package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler exposes the AI assistant and conversation endpoints.
type Handler struct {
	ai     service.Service
	logger *slog.Logger
	cfg    config.AIConfig
}

// New creates a new AI Handler.
func New(svc service.Service, logger *slog.Logger, cfg config.AIConfig) *Handler {
	return &Handler{ai: svc, logger: logger, cfg: cfg}
}

// Health handles GET /api/v1/ai/health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	providerConfigured := h.cfg.Provider != ""
	apiKeyConfigured := h.cfg.APIKey != ""
	modelConfigured := h.cfg.Model != ""

	response.RespondSuccess(w, http.StatusOK, "Request successful", map[string]interface{}{
		"provider":   h.cfg.Provider,
		"configured": providerConfigured && apiKeyConfigured && modelConfigured,
		"checks": map[string]bool{
			"provider": providerConfigured,
			"api_key":  apiKeyConfigured,
			"model":    modelConfigured,
		},
	})
}

// Chat handles POST /api/v1/ai/chat. It receives a prompt message,
// creates or continues a conversation, and returns the assistant's reply.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	var req model.ChatRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("ai chat request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.Chat(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("ai chat service execution failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
}

// ListConversations handles GET /api/v1/ai/conversations.
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	convs, err := h.ai.ListConversations(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list conversations", "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", convs)
}

// GetConversation handles GET /api/v1/ai/conversations/{id}.
func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		response.RespondError(w, h.logger, apperror.Validation("Conversation ID is required"))
		return
	}

	detail, err := h.ai.GetConversation(r.Context(), id, userID)
	if err != nil {
		h.logger.Error("failed to get conversation", "conversation_id", id, "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", detail)
}

// DeleteConversation handles DELETE /api/v1/ai/conversations/{id}.
func (h *Handler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		response.RespondError(w, h.logger, apperror.Validation("Conversation ID is required"))
		return
	}

	if err := h.ai.DeleteConversation(r.Context(), id, userID); err != nil {
		h.logger.Error("failed to delete conversation", "conversation_id", id, "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Conversation deleted successfully", nil)
}
