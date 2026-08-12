package handler

import (
	"log/slog"
	"net/http"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler exposes the AI foundation endpoints. It depends only on the AI
// Service interface, never on a concrete provider SDK.
type Handler struct {
	ai     service.Service
	logger *slog.Logger
	cfg    config.AIConfig
}

func New(svc service.Service, logger *slog.Logger, cfg config.AIConfig) *Handler {
	return &Handler{ai: svc, logger: logger, cfg: cfg}
}

// Generate handles POST /ai/generate. This is a FOUNDATION endpoint to prove
// the AI abstraction end-to-end. It is intentionally generic (no company
// prompt, no business context) until the client's AI use case is known.
//
// TODO: CLIENT REQUIREMENT - decide whether the real AI endpoint needs
// authentication, streaming, usage tracking and richer context once the
// client's use case is defined.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req model.GenerateTextRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.GenerateText(r.Context(), req)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
}

// Health handles GET /ai/health. It only inspects the local AI configuration
// and never calls the external provider, so it does not consume API quota.
// The API key itself is never returned.
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
