// Package routes wires every module into a single chi router. main.go only
// depends on this package, so HTTP wiring never leaks into the entrypoint.
package routes

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/examples"
	"github.com/Abshoor24/backend-go/internal/handler"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/middleware"
	aihandler "github.com/Abshoor24/backend-go/internal/modules/ai/handler"
	projecthandler "github.com/Abshoor24/backend-go/internal/modules/project/handler"
	projectservice "github.com/Abshoor24/backend-go/internal/modules/project/service"
	"github.com/Abshoor24/backend-go/internal/service"
	"github.com/Abshoor24/backend-go/pkg/response"
	"github.com/Abshoor24/backend-go/storage"
)

// Dependencies are explicitly injected into the router from main.go to avoid
// global state and make the wiring testable.
//
// The Gemini provider (used by the project module) and GET /ai/health remain
// active. The legacy AI chat/conversation/document handlers and the code
// execution module were removed; their routes return the router's standard
// 404.
type Dependencies struct {
	Config  *config.Config
	Logger  *slog.Logger
	DB      *storage.Supabase
	Auth    service.AuthService
	Project projectservice.Service
	Tokens  *jwt.Manager
}

// New builds the full HTTP handler with all routes and middleware.
func New(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(deps.Logger))
	r.Use(middleware.Recovery(deps.Logger))
	r.Use(middleware.CORS(deps.Config.CORS.AllowedOrigins))
	r.Use(middleware.SecurityHeaders)

	healthH := handler.NewHealthHandler(deps.DB, deps.Logger)
	authH := handler.NewAuthHandler(deps.Auth, deps.Logger)

	// Chi's default 404/405 bodies are plain text; the api.md contract
	// requires every error to use the JSON envelope instead.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		response.RespondError(w, deps.Logger, apperror.NotFound("Resource not found", nil))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		response.RespondError(w, deps.Logger, apperror.MethodNotAllowed("Method not allowed"))
	})

	r.Get("/health", healthH.Health)
	r.Get("/health/db", healthH.DBHealth)

	r.Route("/api/v1", func(r chi.Router) {
		if deps.Config.Rate.Enabled {
			r.Use(middleware.RateLimit(deps.Config.Rate))
		}

		// Auth endpoints (public)
		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)

		// AI diagnostic endpoints (public).
		var quotaReporter aihandler.QuotaReporter
		if deps.Project != nil {
			quotaReporter, _ = deps.Project.(aihandler.QuotaReporter)
		}
		aiH := aihandler.New(deps.Config.AI, quotaReporter)
		r.Get("/ai/health", aiH.Health)
		r.Get("/ai/quota-status", aiH.QuotaStatus)

		// Curated showcase sites (public, static HTML, no AI calls).
		examplesH := examples.NewHandler(deps.Logger)
		r.Get("/examples", examplesH.List)
		r.Get("/examples/{slug}", examplesH.HTML)

		// Protected endpoints requiring authentication
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(deps.Tokens))
			r.Get("/auth/me", authH.Me)

			if deps.Project != nil {
				projectH := projecthandler.New(deps.Project, deps.Logger, deps.Config.AI, deps.Config.Upload, deps.Config.Project)
				r.Post("/projects", projectH.Create)
				r.Get("/projects", projectH.List)
				r.Get("/projects/{id}", projectH.Get)
				r.Patch("/projects/{id}", projectH.UpdateTitle)
				r.Delete("/projects/{id}", projectH.Delete)
				r.Get("/projects/{id}/versions", projectH.ListVersions)
				r.Get("/projects/{id}/versions/{number}", projectH.GetVersion)
				r.Get("/projects/{id}/export", projectH.Export)
				r.Get("/projects/{id}/attachments", projectH.ListAttachments)
				r.Get("/projects/{id}/attachments/{attachmentId}", projectH.GetAttachment)
			}
		})

		// SSE streaming endpoints: Generate rate limit (on top of the General
		// limit above) + RequireAuthSSE so EventSource clients can pass the
		// JWT via ?token= query parameter. The provider stream is opened
		// BEFORE the SSE headers are sent, so an early provider failure is
		// still a JSON 502, not a broken stream.
		r.Group(func(r chi.Router) {
			if deps.Config.Generate.Enabled {
				r.Use(middleware.RateLimit(deps.Config.Generate))
			}
			r.Use(middleware.RequireAuthSSE(deps.Tokens))
			if deps.Project != nil {
				projectH := projecthandler.New(deps.Project, deps.Logger, deps.Config.AI, deps.Config.Upload, deps.Config.Project)
				r.Post("/projects/{id}/generate", projectH.Generate)
				r.Post("/projects/{id}/revise", projectH.Revise)
			}
		})
	})

	return r
}
