package handler

import (
	"log/slog"
	"net/http"

	"github.com/USERNAME/PROJECT_NAME/internal/ai"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
)

// AIHandler exposes the AI prototype endpoints.
type AIHandler struct {
	ai     ai.Service
	logger *slog.Logger
	cfg    config.AIConfig
}

func NewAIHandler(svc ai.Service, logger *slog.Logger, cfg config.AIConfig) *AIHandler {
	return &AIHandler{ai: svc, logger: logger, cfg: cfg}
}

// Chat handles POST /api/v1/ai/chat. This is a PROTOTYPE endpoint to prove
// the AI abstraction end-to-end. It is intentionally generic (no company
// prompt, no business context) until the client's AI use case is known.
//
// TODO: CLIENT REQUIREMENT - decide whether the real AI endpoint needs
// authentication, streaming, usage tracking and richer context once the
// client's use case is defined.
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req model.AIRequest
	if err := decodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.GenerateResponse(r.Context(), req)
	if err != nil {
		RespondError(w, h.logger, err)
		return
	}

	RespondSuccess(w, http.StatusOK, "Request successful", resp)
}
