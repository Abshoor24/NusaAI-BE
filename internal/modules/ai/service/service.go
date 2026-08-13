package service

import (
	"context"
	"log/slog"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

// Service is the AI abstraction used by HTTP handlers. It decouples the
// application from any concrete LLM API, so the AI can later be used for
// chatbot, recommendation, document Q&A, semantic search, content assistance,
// lead qualification, etc., without touching the handler layer.
//
// TODO: CLIENT REQUIREMENT - this interface may need to grow (streaming,
// structured output, embeddings) once the client's AI use case is decided.
type Service interface {
	GenerateText(ctx context.Context, req model.GenerateTextRequest) (model.GenerateTextResponse, error)
	Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error)
}

type service struct {
	provider provider.Provider
	cfg      config.AIConfig
	logger   *slog.Logger
}

func New(provider provider.Provider, cfg config.AIConfig, logger *slog.Logger) Service {
	return &service{provider: provider, cfg: cfg, logger: logger}
}

// GenerateText passes a request to the configured provider and maps the
// result back to the model layer. No company-specific prompt is built here;
// timeout and transport concerns are handled by the provider.
//
// TODO: CLIENT REQUIREMENT - build system prompts / RAG context assembly for
// the actual client's AI use case once requirements are gathered.
func (s *service) GenerateText(ctx context.Context, req model.GenerateTextRequest) (model.GenerateTextResponse, error) {
	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:   s.cfg.Model,
		Prompt:  req.Prompt,
		Context: req.Context,
	})
	if err != nil {
		return model.GenerateTextResponse{}, err
	}
	return model.GenerateTextResponse{Content: resp.Content, Model: resp.Model}, nil
}

func (s *service) Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	var msgs []provider.ChatMessage

	// 1. Add System Prompt (use default if none is provided)
	systemPrompt := "You are a helpful customer service chatbot assistant."
	if req.System != "" {
		systemPrompt = req.System
	}
	msgs = append(msgs, provider.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// 2. Add history
	for _, msg := range req.History {
		msgs = append(msgs, provider.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// 3. Add latest user message
	msgs = append(msgs, provider.ChatMessage{
		Role:    "user",
		Content: req.Message,
	})

	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:    s.cfg.Model,
		Messages: msgs,
	})
	if err != nil {
		return model.ChatResponse{}, err
	}

	return model.ChatResponse{
		Response: resp.Content,
		Model:    resp.Model,
	}, nil
}
