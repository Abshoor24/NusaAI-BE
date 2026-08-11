// Package ai is the AI foundation module. It is intentionally business-agnostic:
// no company prompt, no chat history, no client-specific behavior. Client AI
// features are added later under this same module structure
// (internal/ai/{handler,service,provider,model}) without touching main.go.
package ai

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/USERNAME/PROJECT_NAME/internal/ai/handler"
	"github.com/USERNAME/PROJECT_NAME/internal/ai/service"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
)

// ModuleDeps carries the dependencies the AI module needs. It is wired from
// internal/routes so main.go only sees a single router.
type ModuleDeps struct {
	Service service.Service
	Config  config.AIConfig
	Logger  *slog.Logger
}

// RegisterRoutes mounts the AI endpoints onto a router. It is called from
// internal/routes inside the /api/v1 group, so rate limiting and CORS apply.
func RegisterRoutes(r chi.Router, deps ModuleDeps) {
	h := handler.New(deps.Service, deps.Logger, deps.Config)
	r.Get("/ai/health", h.Health)
	r.Post("/ai/generate", h.Generate)
}
