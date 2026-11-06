package handler

import (
	"net/http"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler exposes the AI diagnostic endpoint. The legacy chat, conversation,
// document, and attachment endpoints were removed together with the chatbot
// and RAG modules.
type Handler struct {
	cfg config.AIConfig
}

// New creates a new AI Handler.
func New(cfg config.AIConfig) *Handler {
	return &Handler{cfg: cfg}
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
