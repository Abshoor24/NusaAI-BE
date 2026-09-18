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

// Handler exposes the AI assistant, conversation, and document endpoints.
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

// ChatStream handles POST /api/v1/ai/chat/stream.
// It streams the AI response token-by-token using Server-Sent Events (SSE).
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	var req model.ChatRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("ai chat-stream request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	ch, convID, msgID, err := h.ai.ChatStream(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("ai chat-stream service start failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	// SSE is now committed — headers have been sent, we can only write events.
	sse := newSSEWriter(w)

	for chunk := range ch {
		if chunk.Err != nil {
			h.logger.Error("ai chat-stream chunk error", "error", chunk.Err)
			sse.writeEvent("error", map[string]string{"error": "The AI provider returned an error"})
			return
		}
		if chunk.Done {
			sse.writeEvent("done", map[string]string{
				"conversation_id": convID,
				"message_id":      msgID,
			})
			return
		}
		if chunk.Token != "" {
			sse.writeEvent("token", map[string]string{"token": chunk.Token})
		}
	}
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

// IngestDocument handles POST /api/v1/ai/documents.
func (h *Handler) IngestDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	var req model.IngestDocumentRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("ingest document request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.IngestDocument(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("ingest document service failed", "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusCreated, "Document ingested successfully", resp)
}

// ListDocuments handles GET /api/v1/ai/documents.
func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	docs, err := h.ai.ListDocuments(r.Context(), userID)
	if err != nil {
		h.logger.Error("list documents failed", "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", docs)
}

// GetDocument handles GET /api/v1/ai/documents/{id}.
func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		response.RespondError(w, h.logger, apperror.Validation("Document ID is required"))
		return
	}

	doc, err := h.ai.GetDocument(r.Context(), id, userID)
	if err != nil {
		h.logger.Error("get document failed", "document_id", id, "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", doc)
}

// DeleteDocument handles DELETE /api/v1/ai/documents/{id}.
func (h *Handler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		response.RespondError(w, h.logger, apperror.Validation("Document ID is required"))
		return
	}

	if err := h.ai.DeleteDocument(r.Context(), id, userID); err != nil {
		h.logger.Error("delete document failed", "document_id", id, "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Document deleted successfully", nil)
}

// UploadDocument handles POST /api/v1/ai/documents/upload (and /api/v1/documents/upload).
// It accepts a multipart/form-data upload containing a file (PDF, DOCX, TXT) and optional title.
func (h *Handler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	// Limit multipart body to max upload size + 1MB overhead (default 32MB buffer limit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.logger.Error("failed to parse multipart form", "error", err)
		response.RespondError(w, h.logger, apperror.Validation("Invalid multipart form data: "+err.Error()))
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		response.RespondError(w, h.logger, apperror.Validation("File is required under 'file' form field"))
		return
	}
	defer file.Close()

	title := strings.TrimSpace(r.FormValue("title"))
	doc, err := h.ai.UploadDocument(r.Context(), userID, title, fileHeader.Filename, fileHeader.Size, file)
	if err != nil {
		h.logger.Error("document upload failed", "user_id", userID, "filename", fileHeader.Filename, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusAccepted, "Document uploaded and queued for processing", doc)
}

// GetDocumentStatus handles GET /api/v1/ai/documents/{id}/status (and /api/v1/documents/{id}/status).
func (h *Handler) GetDocumentStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		response.RespondError(w, h.logger, apperror.Validation("Document ID is required"))
		return
	}

	statusResp, err := h.ai.GetDocumentStatus(r.Context(), id, userID)
	if err != nil {
		h.logger.Error("get document status failed", "document_id", id, "user_id", userID, "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", statusResp)
}
