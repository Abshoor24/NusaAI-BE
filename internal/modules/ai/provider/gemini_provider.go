package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
)

// GeminiProvider talks to the Google Gemini Generative Language API
// (models/{model}:streamGenerateContent). The base URL must point at the API
// root, e.g. https://generativelanguage.googleapis.com/v1beta. The API key is
// sent via the x-goog-api-key header and never logged or returned to the
// client.
type GeminiProvider struct {
	baseURL      string
	apiKey       string
	streamClient *http.Client // no hard timeout on body read
	// Optional generation knobs. Zero/nil means "omit generationConfig",
	// preserving the model defaults.
	maxOutputTokens int64
	thinkingBudget  *int
	// thinkingLevel is the Gemini 3.x thinking level (MINIMAL/LOW/MEDIUM/HIGH).
	// nil means omit thinkingConfig from the payload entirely.
	thinkingLevel *string
	logger          *slog.Logger
}

func NewGeminiProvider(cfg config.AIConfig, logger *slog.Logger) *GeminiProvider {
	return &GeminiProvider{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
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
		thinkingLevel:   cfg.ThinkingLevel,
		logger:          logger,
	}
}

// OverloadedMessage is the client-safe message used when Gemini reports
// load shedding (HTTP 429/503, or a mid-stream error object with status
// RESOURCE_EXHAUSTED/UNAVAILABLE). The error code stays AI_PROVIDER_ERROR
// and the HTTP mapping stays 502 pre-stream / SSE error event mid-stream;
// only the message changes so the frontend can suggest retrying.
const OverloadedMessage = "The AI provider is temporarily overloaded, please try again"

// GenericProviderMessage is the client-safe message for all other provider
// failures (bad key, bad request, decode errors, ...).
const GenericProviderMessage = "The AI provider returned an error"

// IsOverloadedError reports whether err is a provider-overload failure and
// therefore eligible for the limited pre-stream retry. It recognizes both
// the overloaded message and the wrapped provider status ("status 429",
// "status 503", "status RESOURCE_EXHAUSTED", "status UNAVAILABLE") so
// callers using hand-built fakes stay classifiable.
func IsOverloadedError(err error) bool {
	if err == nil {
		return false
	}
	var ae *apperror.Error
	if !isAIProviderError(err, &ae) {
		return false
	}
	if ae.Message == OverloadedMessage {
		return true
	}
	s := ""
	if ae.Err != nil {
		s = ae.Err.Error()
	}
	return strings.Contains(s, "429") || strings.Contains(s, "503") ||
		strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "UNAVAILABLE")
}

func isAIProviderError(err error, ae **apperror.Error) bool {
	var e *apperror.Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Code() != "AI_PROVIDER_ERROR" {
		return false
	}
	*ae = e
	return true
}

// overloadedStatus reports whether an HTTP status code means "try again
// later" rather than a client/payload problem.
func overloadedStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable
}

// overloadedObjectStatus reports whether a mid-stream error object status
// means overload. Gemini uses RESOURCE_EXHAUSTED (429 class) and
// UNAVAILABLE (503 class) for load shedding.
func overloadedObjectStatus(status string, code int) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RESOURCE_EXHAUSTED", "UNAVAILABLE":
		return true
	}
	return code == 429 || code == 503
}

// providerStatusError builds the pre-stream provider error for a non-200
// response: overloaded message + reason for 429/503, generic message with
// PROVIDER_ERROR otherwise. The raw provider body is never included: it
// stays in server logs only.
func providerStatusError(code int) error {
	if overloadedStatus(code) {
		return apperror.AIProviderReason(apperror.ReasonProviderOverloaded, OverloadedMessage, fmt.Errorf("status %d", code))
	}
	return apperror.AIProviderReason(apperror.ReasonProviderError, GenericProviderMessage, fmt.Errorf("status %d", code))
}

// generationConfig returns the Gemini generationConfig payload, or nil when
// neither knob is set so the request is byte-identical to before.
func (p *GeminiProvider) generationConfig() map[string]interface{} {
	if p.maxOutputTokens <= 0 && p.thinkingBudget == nil && p.thinkingLevel == nil {
		return nil
	}
	genCfg := map[string]interface{}{}
	if p.maxOutputTokens > 0 {
		genCfg["maxOutputTokens"] = p.maxOutputTokens
	}
	if p.thinkingLevel != nil {
		genCfg["thinkingConfig"] = map[string]interface{}{"thinkingLevel": *p.thinkingLevel}
	} else if p.thinkingBudget != nil {
		genCfg["thinkingConfig"] = map[string]interface{}{"thinkingBudget": *p.thinkingBudget}
	}
	return genCfg
}

// CompleteStream sends a streamGenerateContent request to the Gemini API and
// returns a read-only channel of StreamChunk values. The caller MUST drain the
// channel until it is closed (or cancel ctx) to avoid goroutine leaks.
// The channel will receive a terminal chunk with Done=true on success, or a
// chunk with Err set on failure. No further chunks are sent after either.
func (p *GeminiProvider) CompleteStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if req.Model == "" {
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI model is not configured", nil)
	}
	if p.apiKey == "" {
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI API key is not configured", nil)
	}
	if len(req.Messages) == 0 {
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI request has no messages", nil)
	}

	var systemParts []string
	contents := make([]map[string]interface{}, 0, len(req.Messages))

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
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "Failed to build AI stream request", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", p.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "Failed to build AI stream request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use p.streamClient (no hard body-read timeout) so long streams are not
	// killed mid-response. ResponseHeaderTimeout is set on the transport.
	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "AI stream request was cancelled", ctx.Err())
		}
		p.logger.Error("ai provider stream request failed", "error", err)
		return nil, apperror.AIProviderReason(apperror.ReasonProviderError, "The AI provider could not be reached", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		p.logger.Error("ai provider stream returned error",
			"status", resp.StatusCode,
			"provider_body", string(errBody),
		)
		return nil, providerStatusError(resp.StatusCode)
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		start := time.Now()
		// Per-stream diagnostics (Task 2, provider half): counts only, never
		// content. chunks = SSE data objects decoded, textBytes = emitted
		// text bytes. The project layer adds project_id + duration.
		var chunks, textBytes int
		logSummary := func(ended, errKind string, err error, finishReason, blockReason string, usage *UsageMetadata) {
			attrs := []any{
				"ended", ended,
				"chunks", chunks,
				"text_bytes", textBytes,
				"finish_reason", finishReason,
				"block_reason", blockReason,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if usage != nil {
				attrs = append(attrs,
					"prompt_tokens", usage.PromptTokenCount,
					"candidates_tokens", usage.CandidatesTokenCount,
					"thoughts_tokens", usage.ThoughtsTokenCount,
					"total_tokens", usage.TotalTokenCount,
				)
			}
			if errKind != "" {
				attrs = append(attrs, "error_kind", errKind)
			}
			if err != nil {
				p.logger.Error("ai stream ended", append(attrs, "error", err)...)
				return
			}
			p.logger.Info("ai stream ended", attrs...)
		}

		// geminiStreamResponse mirrors the JSON shape of each SSE data line.
		// FinishReason is read (not the text) so callers can tell a clean
		// STOP apart from truncation (MAX_TOKENS) or other terminal states.
		// promptFeedback and usageMetadata are captured for per-stream
		// diagnostics; a top-level error object ends the stream as failure.
		// Parts carry an optional thought flag: thought text is reasoning,
		// not output, and must never be concatenated into the document.
		type geminiStreamResponse struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
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

		// bufio.Reader has no line-length cap, unlike bufio.Scanner (64 KiB
		// default, 1 MiB after tuning). A single SSE data line can carry a
		// large text part, so ReadString is used to read it whole.
		reader := bufio.NewReader(resp.Body)

		var finishReason, blockReason string
		var usage UsageMetadata
		var hasUsage bool

		for {
			if ctx.Err() != nil {
				logSummary("cancelled", "context", ctx.Err(), finishReason, blockReason, usageOrNil(usage, hasUsage))
				return
			}
			rawLine, readErr := reader.ReadString('\n')
			line := strings.TrimRight(rawLine, "\r\n")
			if readErr != nil && len(line) == 0 {
				if readErr == io.EOF {
					break // Normal EOF: fall through to Done below.
				}
				if ctx.Err() != nil {
					logSummary("cancelled", "context", ctx.Err(), finishReason, blockReason, usageOrNil(usage, hasUsage))
					return // Normal cancellation
				}
				p.logger.Error("ai provider stream read error", "error", readErr)
				logSummary("error", "read", readErr, finishReason, blockReason, usageOrNil(usage, hasUsage))
				select {
				case ch <- StreamChunk{Err: apperror.AIProviderReason(apperror.ReasonProviderError, "AI stream read error", readErr)}:
				case <-ctx.Done():
				}
				return
			}
			// SSE data lines are prefixed with "data: ". Anything else
			// (comments, heartbeats, blank separators) is ignored.
			if !strings.HasPrefix(line, "data: ") {
				if readErr == io.EOF {
					break
				}
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data == "" || data == "[DONE]" {
				if readErr == io.EOF {
					break
				}
				continue
			}

			var sr geminiStreamResponse
			if err := json.Unmarshal([]byte(data), &sr); err != nil {
				p.logger.Error("ai provider stream decode failed", "error", err)
				logSummary("error", "decode", err, finishReason, blockReason, usageOrNil(usage, hasUsage))
				select {
				case ch <- StreamChunk{Err: apperror.AIProviderReason(apperror.ReasonProviderError, "Failed to decode AI stream response", err)}:
				case <-ctx.Done():
				}
				return
			}

			// A mid-stream error object is terminal: surface it instead of
			// silently skipping the line and ending as a clean Done.
			// Overload objects (UNAVAILABLE / RESOURCE_EXHAUSTED) carry the
			// retry-suggesting message; everything else stays generic.
			if sr.Error != nil {
				p.logger.Error("ai provider stream returned error object",
					"code", sr.Error.Code,
					"status", sr.Error.Status,
					"message", sr.Error.Message,
				)
				msg := GenericProviderMessage
				reason := apperror.ReasonProviderError
				if overloadedObjectStatus(sr.Error.Status, sr.Error.Code) {
					msg = OverloadedMessage
					reason = apperror.ReasonProviderOverloaded
				}
				providerErr := apperror.AIProviderReason(reason, msg, fmt.Errorf("status %s", sr.Error.Status))
				logSummary("error", "provider_error_object", providerErr, finishReason, blockReason, usageOrNil(usage, hasUsage))
				select {
				case ch <- StreamChunk{Err: providerErr}:
				case <-ctx.Done():
				}
				return
			}
			chunks++
			if sr.PromptFeedback.BlockReason != "" {
				blockReason = sr.PromptFeedback.BlockReason
			}
			if sr.UsageMetadata != (UsageMetadata{}) {
				usage = sr.UsageMetadata
				hasUsage = true
			}

			// All candidates are scanned (normally exactly one). Only
			// non-thought text parts are emitted; non-text parts decode with
			// empty Text and are skipped, thought parts are skipped
			// explicitly so reasoning never pollutes the document.
			for _, cand := range sr.Candidates {
				if cand.FinishReason != "" {
					finishReason = cand.FinishReason
				}
				for _, part := range cand.Content.Parts {
					if part.Thought || part.Text == "" {
						continue
					}
					textBytes += len(part.Text)
					select {
					case ch <- StreamChunk{Token: part.Text}:
					case <-ctx.Done():
						logSummary("cancelled", "context", ctx.Err(), finishReason, blockReason, usageOrNil(usage, hasUsage))
						return
					}
				}
			}
			if readErr == io.EOF {
				break
			}
		}

		// EOF without transport error. An empty finishReason here means the
		// model never terminated cleanly (premature close); the Done chunk
		// still carries what was observed and FinishStream classifies it.
		logSummary("eof", "", nil, finishReason, blockReason, usageOrNil(usage, hasUsage))
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

// usageOrNil returns nil when the provider reported no usageMetadata, so
// diagnostic logs omit token fields instead of logging zeros.
func usageOrNil(u UsageMetadata, ok bool) *UsageMetadata {
	if !ok {
		return nil
	}
	out := u
	return &out
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
