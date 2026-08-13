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

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
)

// GeminiProvider talks to the Google Gemini Generative Language API
// (models/{model}:generateContent). The base URL must point at the API root,
// e.g. https://generativelanguage.googleapis.com/v1beta. The API key is sent
// via the x-goog-api-key header and never logged or returned to the client.
type GeminiProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *slog.Logger
}

func NewGeminiProvider(cfg config.AIConfig, logger *slog.Logger) *GeminiProvider {
	return &GeminiProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: cfg.Timeout},
		logger:  logger,
	}
}

// Complete sends a generateContent request to the Gemini API.
func (p *GeminiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if req.Model == "" {
		return Response{}, apperror.AIProvider("AI model is not configured", nil)
	}
	if p.apiKey == "" {
		return Response{}, apperror.AIProvider("AI API key is not configured", nil)
	}

	var systemParts []string
	contents := make([]map[string]interface{}, 0, len(req.Messages)+1)

	if len(req.Messages) > 0 {
		for _, msg := range req.Messages {
			switch msg.Role {
			case "system":
				systemParts = append(systemParts, msg.Content)
			case "assistant":
				contents = appendGeminiContent(contents, "model", msg.Content)
			default: // user
				contents = appendGeminiContent(contents, "user", msg.Content)
			}
		}
	} else {
		// Legacy single-shot path: Context becomes the system instruction,
		// Prompt becomes the user message.
		if len(req.Context) > 0 {
			systemParts = append(systemParts, strings.Join(req.Context, "\n\n"))
		}
		contents = appendGeminiContent(contents, "user", req.Prompt)
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if len(systemParts) > 0 {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, apperror.AIProvider("Failed to build AI request", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", p.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, apperror.AIProvider("Failed to build AI request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

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
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		ModelVersion string `json:"modelVersion"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		p.logger.Error("ai provider decode failed", "error", err)
		return Response{}, apperror.AIProvider("Failed to read AI provider response", err)
	}

	var sb strings.Builder
	for _, cand := range out.Candidates {
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
		}
	}
	if sb.Len() == 0 {
		return Response{}, apperror.AIProvider("The AI provider returned no response", nil)
	}

	return Response{
		Content: sb.String(),
		Model:   out.ModelVersion,
	}, nil
}

// appendGeminiContent appends a content entry to the contents slice and
// returns the (possibly reallocated) slice. Messages that come in consecutive
// same-role pairs are merged into a single content entry so the generated
// request is always valid for the Gemini API.
func appendGeminiContent(contents []map[string]interface{}, role, text string) []map[string]interface{} {
	if n := len(contents); n > 0 {
		last := contents[n-1]
		if lastRole, _ := last["role"].(string); lastRole == role {
			parts, _ := last["parts"].([]map[string]string)
			parts = append(parts, map[string]string{"text": text})
			last["parts"] = parts
			contents[n-1] = last
			return contents
		}
	}
	return append(contents, map[string]interface{}{
		"role":  role,
		"parts": []map[string]string{{"text": text}},
	})
}