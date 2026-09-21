// Package routes wires every module into a single chi router. main.go only
// depends on this package, so HTTP wiring never leaks into the entrypoint.
package routes

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/handler"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/middleware"
	aihandler "github.com/Abshoor24/backend-go/internal/modules/ai/handler"
	aiservice "github.com/Abshoor24/backend-go/internal/modules/ai/service"
	executionhandler "github.com/Abshoor24/backend-go/internal/modules/execution/handler"
	executionservice "github.com/Abshoor24/backend-go/internal/modules/execution/service"
	"github.com/Abshoor24/backend-go/internal/service"
	"github.com/Abshoor24/backend-go/storage"
)

// Dependencies are explicitly injected into the router from main.go to avoid
// global state and make the wiring testable.
type Dependencies struct {
	Config    *config.Config
	Logger    *slog.Logger
	DB        *storage.Supabase
	Auth      service.AuthService
	AI        aiservice.Service
	Execution executionservice.Service
	Tokens    *jwt.Manager
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

	r.Get("/health", healthH.Health)
	r.Get("/health/db", healthH.DBHealth)

	r.Route("/api/v1", func(r chi.Router) {
		if deps.Config.Rate.Enabled {
			r.Use(middleware.RateLimit(deps.Config.Rate))
		}

		// Auth endpoints (public)
		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)

		// AI diagnostic endpoint (public)
		aiH := aihandler.New(deps.AI, deps.Logger, deps.Config.AI, deps.Config.Upload)
		r.Get("/ai/health", aiH.Health)

		// Protected endpoints requiring authentication
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(deps.Tokens))
			r.Get("/auth/me", authH.Me)

			// Code Sandbox Execution: dedicated stricter rate limit
			if deps.Execution != nil {
				execH := executionhandler.New(deps.Execution, deps.Logger, deps.Config.Execution)
				r.Group(func(r chi.Router) {
					if deps.Config.Execution.RateLimitEnabled {
						r.Use(middleware.RateLimit(config.RateLimitConfig{
							Enabled:   true,
							PerMinute: deps.Config.Execution.RateLimitPerMin,
							Burst:     deps.Config.Execution.RateLimitBurst,
						}))
					}
					r.Post("/execute", execH.Execute)
					r.Get("/execute/history", execH.ListHistory)
					r.Get("/execute/{id}", execH.GetDetail)
				})
			}

			// AI Assistant, Conversation, & Document interaction: rate limited
			r.Group(func(r chi.Router) {
				if deps.Config.AI.ChatRateLimitEnabled {
					r.Use(middleware.RateLimit(config.RateLimitConfig{
						Enabled:   true,
						PerMinute: deps.Config.AI.ChatRateLimitPerMin,
						Burst:     deps.Config.AI.ChatRateLimitPerMin,
					}))
				}
				r.Post("/ai/chat", aiH.Chat)
				r.Post("/ai/chat/stream", aiH.ChatStream)
				r.Get("/ai/conversations", aiH.ListConversations)
				r.Get("/ai/conversations/{id}", aiH.GetConversation)
				r.Delete("/ai/conversations/{id}", aiH.DeleteConversation)

				// Multimodal Attachment endpoints
				r.Get("/ai/attachments/{id}", aiH.GetAttachment)
				r.Get("/attachments/{id}", aiH.GetAttachment)

				// RAG Knowledge Document endpoints
				r.Post("/ai/documents", aiH.IngestDocument)
				r.Post("/ai/documents/upload", aiH.UploadDocument)
				r.Get("/ai/documents", aiH.ListDocuments)
				r.Get("/ai/documents/{id}", aiH.GetDocument)
				r.Get("/ai/documents/{id}/status", aiH.GetDocumentStatus)
				r.Delete("/ai/documents/{id}", aiH.DeleteDocument)

				// Aliases without /ai prefix as specified in requirements
				r.Post("/documents/upload", aiH.UploadDocument)
				r.Get("/documents/{id}/status", aiH.GetDocumentStatus)
			})
		})
	})

	return r
}
