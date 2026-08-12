package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// DBPinger is the minimal dependency the health handler needs. It keeps the
// handler decoupled from the concrete database package for testability.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler exposes liveness and database readiness endpoints.
type HealthHandler struct {
	db     DBPinger
	logger *slog.Logger
}

func NewHealthHandler(db DBPinger, logger *slog.Logger) *HealthHandler {
	return &HealthHandler{db: db, logger: logger}
}

// Health reports liveness of the process. It does not touch the database.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response.RespondSuccess(w, http.StatusOK, "Request successful", map[string]string{"status": "ok"})
}

// DBHealth reports whether the database connection pool can be pinged.
// Failures return HTTP 503 and never leak internal database errors.
func (h *HealthHandler) DBHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.logger.Error("database health check failed", "error", err)
		response.RespondError(w, h.logger, apperror.Unavailable("Database is unavailable", err))
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", map[string]string{
		"status":   "ok",
		"database": "connected",
	})
}
