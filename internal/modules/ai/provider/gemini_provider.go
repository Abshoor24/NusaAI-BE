package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	baseURL      string
	apiKey       string
	client       *http.Client // used for non-streaming (complete) requests
	streamClient *http.Client // used for streaming — no hard timeout on body read
	// Optional generation knobs. Zero/nil means "omit generationConfig",
	// preserving the model defaults.
	maxOutputTokens int64
	thinkingBudget  *int
	logger          *slog.Logger
}

func NewGeminiProvider(cfg config.AIConfig, logger *slog.Logger) *GeminiProvider {
	return &GeminiProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		// Regular client: hard timeout covers the entire round-trip.
		client: &http.Client{Timeout: cfg.Timeout},
		// Stream client: no Timeout so we don't kill a long-running stream body.
		// ResponseHeaderTimeout protects against a server that accepts the TCP
		// connection but never sends response headers.
		streamClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: cfg.Timeout,
			},
		},
		maxOutputTokens: cfg.MaxOutputTokens,
		thinkingBudget:  cfg.ThinkingBudget,
		logger:          logger,
	}
}

// generationConfig returns the Gemini generationConfig payload, or nil when
// neither knob is set so the request is byte-identical to before.
func (p *GeminiProvider) generationConfig() map[string]interface{} {
	if p.maxOutputTokens <= 0 && p.thinkingBudget == nil {
		return nil
	}
	genCfg := map[string]interface{}{}
	if p.maxOutputTokens > 0 {
		genCfg["maxOutputTokens"] = p.maxOutputTokens
	}
	if p.thinkingBudget != nil {
		genCfg["thinkingConfig"] = map[string]interface{}{"thinkingBudget": *p.thinkingBudget}
	}
	return genCfg
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
				contents = appendGeminiContent(contents, "model", msg.Content, nil)
			default: // user
				contents = appendGeminiContent(contents, "user", msg.Content, msg.Images)
			}
		}
	} else {
		// Legacy single-shot path: Context becomes the system instruction,
		// Prompt becomes the user message.
		if len(req.Context) > 0 {
			systemParts = append(systemParts, strings.Join(req.Context, "\n\n"))
		}
		contents = appendGeminiContent(contents, "user", req.Prompt, nil)
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if len(systemParts) > 0 {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}
	if genCfg := p.generationConfig(); genCfg != nil {
		payload["generationConfig"] = genCfg
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

// CompleteStream sends a streamGenerateContent request to the Gemini API and
// returns a read-only channel of StreamChunk values. The caller MUST drain the
// channel until it is closed (or cancel ctx) to avoid goroutine leaks.
// The channel will receive a terminal chunk with Done=true on success, or a
// chunk with Err set on failure. No further chunks are sent after either.
func (p *GeminiProvider) CompleteStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if req.Model == "" {
		return nil, apperror.AIProvider("AI model is not configured", nil)
	}
	if p.apiKey == "" {
		return nil, apperror.AIProvider("AI API key is not configured", nil)
	}

	var systemParts []string
	contents := make([]map[string]interface{}, 0, len(req.Messages)+1)

	if len(req.Messages) > 0 {
		for _, msg := range req.Messages {
			switch msg.Role {
			case "system":
				systemParts = append(systemParts, msg.Content)
			case "assistant":
				contents = appendGeminiContent(contents, "model", msg.Content, nil)
			default: // user
				contents = appendGeminiContent(contents, "user", msg.Content, msg.Images)
			}
		}
	} else {
		if len(req.Context) > 0 {
			systemParts = append(systemParts, strings.Join(req.Context, "\n\n"))
		}
		contents = appendGeminiContent(contents, "user", req.Prompt, nil)
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if len(systemParts) > 0 {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}
	if genCfg := p.generationConfig(); genCfg != nil {
		payload["generationConfig"] = genCfg
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, apperror.AIProvider("Failed to build AI stream request", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", p.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, apperror.AIProvider("Failed to build AI stream request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use p.streamClient (no hard body-read timeout) so long streams are not
	// killed mid-response. ResponseHeaderTimeout is set on the transport.
	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, apperror.AIProvider("AI stream request was cancelled", ctx.Err())
		}
		p.logger.Error("ai provider stream request failed", "error", err)
		return nil, apperror.AIProvider("The AI provider could not be reached", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		p.logger.Error("ai provider stream returned error",
			"status", resp.StatusCode,
			"provider_body", string(errBody),
		)
		return nil, apperror.AIProvider("The AI provider returned an error", fmt.Errorf("status %d", resp.StatusCode))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		// geminiStreamResponse mirrors the JSON shape of each SSE data line.
		// FinishReason is read (not the text) so callers can tell a clean
		// STOP apart from truncation (MAX_TOKENS) or other terminal states.
		// promptFeedback and usageMetadata are captured for per-stream
		// diagnostics; a top-level error object ends the stream as failure.
		type geminiStreamResponse struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			PromptFeedback struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
			UsageMetadata UsageMetadata `json:"usageMetadata"`
			Error         *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}

		scanner := bufio.NewScanner(resp.Body)
		// A single SSE data line can carry a large text part (a full HTML
		// page approaches PROJECT_MAX_HTML_BYTES). Cap lines at 1 MiB so a
		// huge chunk is still read whole instead of failing the stream.
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		var finishReason, blockReason string
		var usage UsageMetadata
		var hasUsage bool

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			// SSE data lines are prefixed with "data: "
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimSpace(data)
			if data == "" || data == "[DONE]" {
				continue
			}

			var sr geminiStreamResponse
			if err := json.Unmarshal([]byte(data), &sr); err != nil {
				p.logger.Error("ai provider stream decode failed", "error", err, "data", data)
				select {
				case ch <- StreamChunk{Err: apperror.AIProvider("Failed to decode AI stream response", err)}:
				case <-ctx.Done():
				}
				return
			}

			// A mid-stream error object is terminal: surface it instead of
			// silently skipping the line and ending as a clean Done.
			if sr.Error != nil {
				p.logger.Error("ai provider stream returned error object",
					"code", sr.Error.Code,
					"status", sr.Error.Status,
					"message", sr.Error.Message,
				)
				select {
				case ch <- StreamChunk{Err: apperror.AIProvider("The AI provider returned an error", fmt.Errorf("status %s", sr.Error.Status))}:
				case <-ctx.Done():
				}
				return
			}
			if sr.PromptFeedback.BlockReason != "" {
				blockReason = sr.PromptFeedback.BlockReason
			}
			if sr.UsageMetadata != (UsageMetadata{}) {
				usage = sr.UsageMetadata
				hasUsage = true
			}

			for _, cand := range sr.Candidates {
				if cand.FinishReason != "" {
					finishReason = cand.FinishReason
				}
				for _, part := range cand.Content.Parts {
					if part.Text == "" {
						continue
					}
					select {
					case ch <- StreamChunk{Token: part.Text}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return // Normal cancellation
			}
			p.logger.Error("ai provider stream scan error", "error", err)
			select {
			case ch <- StreamChunk{Err: apperror.AIProvider("AI stream read error", err)}:
			case <-ctx.Done():
			}
			return
		}

		// Stream ended cleanly.
		done := StreamChunk{Done: true, FinishReason: finishReason, BlockReason: blockReason}
		if hasUsage {
			u := usage
			done.Usage = &u
		}
		select {
		case ch <- done:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func buildGeminiParts(text string, images []ImageAttachment) []map[string]interface{} {
	parts := make([]map[string]interface{}, 0, 1+len(images))
	if text != "" {
		parts = append(parts, map[string]interface{}{"text": text})
	}
	for _, img := range images {
		if len(img.Data) > 0 {
			parts = append(parts, map[string]interface{}{
				"inlineData": map[string]string{
					"mimeType": img.MimeType,
					"data":     base64.StdEncoding.EncodeToString(img.Data),
				},
			})
		}
	}
	return parts
}

// appendGeminiContent appends a content entry to the contents slice and
// returns the (possibly reallocated) slice. Messages that come in consecutive
// same-role pairs are merged into a single content entry so the generated
// request is always valid for the Gemini API.
func appendGeminiContent(contents []map[string]interface{}, role, text string, images []ImageAttachment) []map[string]interface{} {
	newParts := buildGeminiParts(text, images)
	if len(newParts) == 0 {
		return contents
	}

	if n := len(contents); n > 0 {
		last := contents[n-1]
		if lastRole, _ := last["role"].(string); lastRole == role {
			if existingParts, ok := last["parts"].([]map[string]interface{}); ok {
				last["parts"] = append(existingParts, newParts...)
				contents[n-1] = last
				return contents
			}
		}
	}
	return append(contents, map[string]interface{}{
		"role":  role,
		"parts": newParts,
	})
}

// Embed sends an embedContent request to the Gemini API (models/{model}:embedContent).
func (p *GeminiProvider) Embed(ctx context.Context, model string, text string) ([]float32, error) {
	if model == "" {
		model = "text-embedding-004"
	}
	if p.apiKey == "" {
		return nil, apperror.AIProvider("AI API key is not configured", nil)
	}

	modelName := strings.TrimPrefix(model, "models/")
	payload := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", modelName),
		"content": map[string]interface{}{
			"parts": []map[string]string{
				{"text": text},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, apperror.AIProvider("Failed to build AI embed request", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent", p.baseURL, modelName)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, apperror.AIProvider("Failed to build AI embed request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, apperror.AIProvider("AI embed request was cancelled", ctx.Err())
		}
		p.logger.Error("ai provider embed request failed", "error", err)
		return nil, apperror.AIProvider("The AI provider could not be reached", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		p.logger.Error("ai provider embed returned error",
			"status", resp.StatusCode,
			"provider_body", string(respBody),
		)
		return nil, apperror.AIProvider("The AI provider returned an error", fmt.Errorf("status %d", resp.StatusCode))
	}

	var out struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		p.logger.Error("ai provider embed decode failed", "error", err)
		return nil, apperror.AIProvider("Failed to read AI embed response", err)
	}

	if len(out.Embedding.Values) == 0 {
		return nil, apperror.AIProvider("The AI provider returned no embeddings", nil)
	}

	return out.Embedding.Values, nil
}
