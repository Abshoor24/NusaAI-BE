package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Abshoor24/backend-go/internal/apperror"
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
	GenerateSolution(ctx context.Context, req model.GenerateSolutionRequest) (model.GenerateSolutionResponse, error)
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

func (s *service) GenerateSolution(ctx context.Context, req model.GenerateSolutionRequest) (model.GenerateSolutionResponse, error) {
	systemPrompt := `You are an expert digital solutions architect. Analyze the user's business type, problem, and goal. Return ONLY a valid JSON object with this exact structure:

{
  "solution_name": "string",
  "summary": "string", 
  "recommended_features": ["string", ...],
  "recommended_technology": ["string", ...],
  "complexity": "Low" | "Medium" | "High"
}

Rules:
- NO markdown, NO extra text, NO explanations
- Output ONLY the JSON object
- solution_name: concise product/platform name
- summary: 1-2 sentence description
- recommended_features: 3-7 specific features
- recommended_technology: 3-6 technologies (React, Go, PostgreSQL, etc.)
- complexity: "Low", "Medium", or "High" based on scope`

	userPrompt := fmt.Sprintf(`Business Type: %s
Business Problem: %s
Business Goal: %s`, req.BusinessType, req.BusinessProblem, req.BusinessGoal)

	resp, err := s.provider.Complete(ctx, provider.Request{
		Model: s.cfg.Model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return model.GenerateSolutionResponse{}, err
	}

	var solution model.GeneratedSolution
	if err := json.Unmarshal([]byte(resp.Content), &solution); err != nil {
		s.logger.Error("failed to parse AI solution response", "error", err, "raw", resp.Content)
		return model.GenerateSolutionResponse{}, apperror.AIProvider("The AI provider returned an invalid response format", err)
	}

	// Validate required fields
	if solution.SolutionName == "" || solution.Summary == "" || len(solution.RecommendedFeatures) == 0 || len(solution.RecommendedTechnology) == 0 || solution.Complexity == "" {
		s.logger.Error("AI solution response missing required fields", "solution", solution)
		return model.GenerateSolutionResponse{}, apperror.AIProvider("The AI provider returned an incomplete response", nil)
	}

	return model.GenerateSolutionResponse{
		Solution: solution,
		Model:    resp.Model,
	}, nil
}
