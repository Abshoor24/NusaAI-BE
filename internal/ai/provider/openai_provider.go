package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/USERNAME/PROJECT_NAME/internal/apperror"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
)

// OpenAIProvider talks to any OpenAI-compatible chat completions API. The
// base URL is configurable so providers like OpenAI, OpenRouter, local
// Ollama/vLLM or other compatible gateways can be used without code changes.
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *slog.Logger
}

func NewOpenAIProvider(cfg config.AIConfig, logger *slog.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: cfg.Timeout},
		logger:  logger,
	}
}

// Complete sends a chat completion request. The API key is only used
// server-side and is never logged or returned to the client.
func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if req.Model == "" {
		return Response{}, apperror.AIProvider("AI model is not configured", nil)
	}

	messages := make([]map[string]string, 0, len(req.Context)+1)
	// TODO: CLIENT REQUIREMENT - replace generic context injection with a
	// real system prompt / RAG strategy once the client's AI use case exists.
	if len(req.Context) > 0 {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": strings.Join(req.Context, "\n\n"),
		})
	}
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.Prompt,
	})

	payload, err := json.Marshal(map[string]interface{}{
		"model":    req.Model,
		"messages": messages,
	})
	if err != nil {
		return Response{}, apperror.AIProvider("Failed to build AI request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperror.AIProvider("Failed to build AI request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// Context cancellation/timeout is common and not a provider bug.
		if ctx.Err() != nil {
			return Response{}, apperror.AIProvider("AI request was cancelled", ctx.Err())
		}
		p.logger.Error("ai provider request failed", "error", err)
		return Response{}, apperror.AIProvider("The AI provider could not be reached", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read a limited slice for server-side diagnostics only. The raw
		// provider error is never exposed to the client.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		p.logger.Error("ai provider returned error",
			"status", resp.StatusCode,
			"provider_body", string(body),
		)
		return Response{}, apperror.AIProvider("The AI provider returned an error", fmt.Errorf("status %d", resp.StatusCode))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		p.logger.Error("ai provider decode failed", "error", err)
		return Response{}, apperror.AIProvider("Failed to read AI provider response", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return Response{}, apperror.AIProvider("The AI provider returned no response", nil)
	}

	return Response{
		Content: out.Choices[0].Message.Content,
		Model:   out.Model,
	}, nil
}
