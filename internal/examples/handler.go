package examples

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler serves the showcase catalog and the bundled static sites.
type Handler struct {
	logger *slog.Logger
}

// NewHandler creates the showcase handler.
func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{logger: logger}
}

// List handles GET /api/v1/examples.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	response.RespondSuccess(w, http.StatusOK, "Request successful", List())
}

// HTML handles GET /api/v1/examples/{slug}. It returns raw HTML, not the JSON
// envelope, because the frontend renders it inside a sandboxed srcDoc frame.
func (h *Handler) HTML(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	_, html, ok := Get(slug)
	if !ok {
		response.RespondError(w, h.logger, apperror.NotFound("Example not found", nil))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(html); err != nil {
		h.logger.Warn("write example html", "slug", slug, "error", err)
	}
}
