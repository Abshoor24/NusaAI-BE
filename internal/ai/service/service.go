package ai

import (
	"context"
	"log/slog"

	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
)

// Service is the AI abstraction used by HTTP handlers. It decouples the
// application from any concrete LLM API, so the AI can later be used for
// chatbot, recommendation, document Q&A, semantic search, content assistance,
// lead qualification, etc., without touching the handler layer.
//
// TODO: CLIENT REQUIREMENT - this interface may need to grow (streaming,
// structured output, embeddings) once the client's AI use case is decided.
type Service interface {
	GenerateResponse(ctx context.Context, req model.AIRequest) (model.AIResponse, error)
}

type service struct {
	provider Provider
	cfg      config.AIConfig
	logger   *slog.Logger
}

func NewService(provider Provider, cfg config.AIConfig, logger *slog.Logger) Service {
	return &service{provider: provider, cfg: cfg, logger: logger}
}

// GenerateResponse passes a request to the configured provider and maps the
// result back to the model layer. No company-specific prompt is built here.
//
// TODO: CLIENT REQUIREMENT - build system prompts / RAG context assembly for
// the actual client's AI use case once requirements are gathered.
func (s *service) GenerateResponse(ctx context.Context, req model.AIRequest) (model.AIResponse, error) {
	resp, err := s.provider.Complete(ctx, providerRequest{
		Model:   s.cfg.Model,
		Message: req.Message,
		Context: req.Context,
	})
	if err != nil {
		return model.AIResponse{}, err
	}
	return model.AIResponse{Message: resp.Message, Model: resp.Model}, nil
}
