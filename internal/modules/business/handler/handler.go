package handler

import (
	"log/slog"

	"github.com/Abshoor24/backend-go/internal/modules/business/service"
)

// Handler exposes the HTTP endpoints of this business module. It depends only
// on the Service interface, never on a concrete repository.
//
// TODO: CLIENT REQUIREMENT - add handler methods once the client's endpoints
// are known. See internal/modules/ai/handler for the established pattern
// (response.DecodeAndValidate + response.RespondSuccess/RespondError).
type Handler struct {
	svc    service.Service
	logger *slog.Logger
}

func New(svc service.Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// TODO: CLIENT REQUIREMENT - example shape:
//
// func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
// 	resp, err := h.svc.List(r.Context(), req)
// 	if err != nil {
// 		response.RespondError(w, h.logger, err)
// 		return
// 	}
// 	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
// }