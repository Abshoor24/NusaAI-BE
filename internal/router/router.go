package router

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/USERNAME/PROJECT_NAME/internal/ai"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/internal/database"
	"github.com/USERNAME/PROJECT_NAME/internal/handler"
	"github.com/USERNAME/PROJECT_NAME/internal/jwt"
	"github.com/USERNAME/PROJECT_NAME/internal/middleware"
	"github.com/USERNAME/PROJECT_NAME/internal/service"
)

// Dependencies are explicitly injected into the router from main.go to avoid
// global state and make the wiring testable.
type Dependencies struct {
	Config *config.Config
	Logger *slog.Logger
	DB     *database.Postgres
	Auth   service.AuthService
	AI     ai.Service
	Tokens *jwt.Manager
}

// New builds the full HTTP handler with all routes and middleware.
//
// Middleware order matters: RequestID and Recovery run first so that logging
// has an ID and panics are contained. CORS is applied at the top level so that
// every endpoint (including /health) can be reached from the browser during
// development. SecurityHeaders and RateLimit are applied to the API group;
// /health stays thin and is never throttled.
func New(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(deps.Logger))
	r.Use(middleware.Recovery(deps.Logger))
	r.Use(middleware.CORS(deps.Config.CORS.AllowedOrigins))
	r.Use(middleware.SecurityHeaders)

	healthH := handler.NewHealthHandler(deps.DB, deps.Logger)
	authH := handler.NewAuthHandler(deps.Auth, deps.Logger)
	aiH := handler.NewAIHandler(deps.AI, deps.Logger, deps.Config.AI)

	r.Get("/health", healthH.Health)
	r.Get("/health/db", healthH.DBHealth)

	r.Route("/api/v1", func(r chi.Router) {
		if deps.Config.Rate.Enabled {
			r.Use(middleware.RateLimit(deps.Config.Rate))
		}

		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(deps.Tokens))
			r.Get("/auth/me", authH.Me)
			r.Post("/ai/chat", aiH.Chat)
		})

		// Admin namespace foundation.
		// RequireAuth + RequireRole("admin") compose here when the first
		// admin endpoint is added. No admin endpoints exist yet.
		//
		// TODO: CLIENT REQUIREMENT - add admin routes under an authenticated,
		// admin-only group once the client's admin needs are known:
		//   r.Group(func(r chi.Router) {
		//       r.Use(middleware.RequireAuth(deps.Tokens))
		//       r.Use(middleware.RequireRole(model.RoleAdmin))
		//       r.Get("/admin/...", handler)
		//   })
	})

	return r
}
