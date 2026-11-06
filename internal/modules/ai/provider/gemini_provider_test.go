package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
)

func TestGeminiProviderBuildsChatRequest(t *testing.T) {
	var gotPath, gotAuth string
	var gotPayload map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("x-goog-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"halo"},{"text":" dunia"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, testLogger())

	ch, err := p.CompleteStream(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a bakery assistant."},
			{Role: "user", Content: "Hai"},
			{Role: "assistant", Content: "Halo!"},
			{Role: "user", Content: "Jam buka?"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}

	if gotPath != "/models/gemini-2.5-flash:streamGenerateContent" {
		t.Errorf("path = %q, want streamGenerateContent path", gotPath)
	}
	if gotAuth != "test-key" {
		t.Errorf("x-goog-api-key = %q, want test-key", gotAuth)
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		sb.WriteString(chunk.Token)
	}
	if sb.String() != "halo dunia" {
		t.Errorf("content = %q, want \"halo dunia\"", sb.String())
	}

	// systemInstruction holds the system message.
	si, ok := gotPayload["systemInstruction"].(map[string]interface{})
	if !ok {
		t.Fatalf("systemInstruction missing: %v", gotPayload)
	}
	if si["parts"].([]interface{})[0].(map[string]interface{})["text"] != "You are a bakery assistant." {
		t.Errorf("systemInstruction = %v", si)
	}

	// contents hold user/model turns in order (system excluded).
	contents := gotPayload["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("contents length = %d, want 3", len(contents))
	}
	wantRoles := []string{"user", "model", "user"}
	for i, c := range contents {
		m := c.(map[string]interface{})
		if m["role"] != wantRoles[i] {
			t.Errorf("contents[%d] role = %v, want %q", i, m["role"], wantRoles[i])
		}
	}
}

func TestGeminiProviderMergesConsecutiveSameRole(t *testing.T) {
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "k", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())

	ch, err := p.CompleteStream(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: "a"},
			{Role: "user", Content: "b"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	for range ch {
	}

	contents := gotPayload["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("contents length = %d, want 1 (merged)", len(contents))
	}
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	if len(parts) != 2 {
		t.Errorf("parts length = %d, want 2", len(parts))
	}
}

func TestGeminiProviderRequiresModel(t *testing.T) {
	p := NewGeminiProvider(config.AIConfig{APIKey: "k", BaseURL: "http://localhost", Timeout: time.Second}, testLogger())
	_, err := p.CompleteStream(context.Background(), Request{})
	if err == nil {
		t.Fatal("CompleteStream() expected error when model is empty")
	}
}

func TestGeminiProviderCompleteStream(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("x-goog-api-key")
		gotAccept = r.Header.Get("Accept")

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}

		lines := []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`,
			`data: {"candidates":[{"content":{"parts":[{"text":" streaming"}]}}]}`,
			`data: {"candidates":[{"content":{"parts":[{"text":" world"}]}}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			fmt.Fprintf(w, "%s\n\n", l)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}

	if gotPath != "/models/gemini-2.5-flash:streamGenerateContent" {
		t.Errorf("path = %q, want streamGenerateContent", gotPath)
	}
	if gotAuth != "test-key" {
		t.Errorf("auth = %q, want test-key", gotAuth)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("accept = %q, want text/event-stream", gotAccept)
	}

	var tokens []string
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error chunk: %v", chunk.Err)
		}
		if chunk.Done {
			done = true
			break
		}
		if chunk.Token != "" {
			tokens = append(tokens, chunk.Token)
		}
	}

	if !done {
		t.Error("expected stream to end with done=true")
	}
	full := strings.Join(tokens, "")
	if full != "Hello streaming world" {
		t.Errorf("stream content = %q, want 'Hello streaming world'", full)
	}
}

func TestGeminiProviderBuildsMultimodalRequest(t *testing.T) {
	var gotPayload map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"That is a golden retriever puppy."}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{
		APIKey:  "test-api-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, testLogger())

	fakeData := []byte("raw-image-bytes")
	ch, err := p.CompleteStream(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "Describe this puppy image",
				Images: []ImageAttachment{
					{
						MimeType: "image/png",
						Data:     fakeData,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream() multimodal failed: %v", err)
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		sb.WriteString(chunk.Token)
	}
	if sb.String() != "That is a golden retriever puppy." {
		t.Errorf("content = %q, want 'That is a golden retriever puppy.'", sb.String())
	}

	contents, ok := gotPayload["contents"].([]interface{})
	if !ok || len(contents) != 1 {
		t.Fatalf("expected contents of length 1, got %v", gotPayload["contents"])
	}

	userTurn := contents[0].(map[string]interface{})
	if userTurn["role"] != "user" {
		t.Errorf("role = %v, want user", userTurn["role"])
	}

	parts, ok := userTurn["parts"].([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + inlineData), got %d parts: %v", len(parts), parts)
	}

	// Part 0: Text
	part0 := parts[0].(map[string]interface{})
	if part0["text"] != "Describe this puppy image" {
		t.Errorf("part0 text = %v, want 'Describe this puppy image'", part0["text"])
	}

	// Part 1: Inline Data
	part1 := parts[1].(map[string]interface{})
	inlineData, ok := part1["inlineData"].(map[string]interface{})
	if !ok {
		t.Fatalf("part1 missing inlineData: %v", part1)
	}
	if inlineData["mimeType"] != "image/png" {
		t.Errorf("inlineData mimeType = %v, want image/png", inlineData["mimeType"])
	}
	// "raw-image-bytes" base64 is "cmF3LWltYWdlLWJ5dGVz"
	if inlineData["data"] != "cmF3LWltYWdlLWJ5dGVz" {
		t.Errorf("inlineData data = %v, want 'cmF3LWltYWdlLWJ5dGVz'", inlineData["data"])
	}
}

func TestGeminiProviderCompleteStreamFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		lines := []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"<html>"}]},"finishReason":"STOP"}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			fmt.Fprintf(w, "%s\n\n", l)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	var reason string
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error chunk: %v", chunk.Err)
		}
		if chunk.FinishReason != "" {
			reason = chunk.FinishReason
		}
		if chunk.Done {
			done = true
			if chunk.FinishReason == "" {
				t.Error("Done chunk should carry FinishReason")
			}
		}
	}
	if !done {
		t.Error("expected stream to end with done=true")
	}
	if reason != "STOP" {
		t.Errorf("finish reason = %q, want STOP", reason)
	}
}

func TestGeminiProviderCompleteStreamLargeLine(t *testing.T) {
	// One SSE data line larger than bufio.Scanner's 64 KiB default must be
	// read whole, not fail the stream.
	big := strings.Repeat("x", 100*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}]},\"finishReason\":\"STOP\"}]}\n\n", big)
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	var sb strings.Builder
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("large line must not error the stream: %v", chunk.Err)
		}
		sb.WriteString(chunk.Token)
		if chunk.Done {
			done = true
		}
	}
	if !done {
		t.Error("expected stream to end with done=true")
	}
	if sb.String() != big {
		t.Errorf("token bytes = %d, want %d (line truncated?)", sb.Len(), len(big))
	}
}

func TestGeminiProviderCompleteStreamErrorObject(t *testing.T) {
	// A mid-stream {"error": ...} object must surface as an Err chunk,
	// never as silent tokens followed by a clean Done.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`)
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	var sawErr, sawDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			sawErr = true
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if !sawErr {
		t.Error("mid-stream error object must produce an Err chunk")
	}
	if sawDone {
		t.Error("errored stream must not end with Done")
	}
}

func TestGeminiProviderOverloadedPreStream(t *testing.T) {
	// HTTP 503/429 before the stream opens must use the overloaded message
	// (same code AI_PROVIDER_ERROR, still surfaced as 502 JSON upstream);
	// other statuses keep the generic message. The provider body is only
	// logged, never exposed.
	for _, tc := range []struct {
		name       string
		status     int
		wantLoaded bool
	}{
		{"unavailable", http.StatusServiceUnavailable, true},
		{"rate limited", http.StatusTooManyRequests, true},
		{"bad request", http.StatusBadRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprintln(w, `{"error":{"code":503,"message":"high demand","status":"UNAVAILABLE"}}`)
			}))
			defer srv.Close()

			p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
			_, err := p.CompleteStream(context.Background(), Request{
				Model:    "gemini-2.5-flash",
				Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
			})
			if err == nil {
				t.Fatalf("CompleteStream() expected error for status %d", tc.status)
			}
			msg := errorMessage(t, err)
			want := GenericProviderMessage
			if tc.wantLoaded {
				want = OverloadedMessage
			}
			if msg != want {
				t.Errorf("message = %q, want %q", msg, want)
			}
			if !IsOverloadedError(err) != !tc.wantLoaded {
				t.Errorf("IsOverloadedError = %v, want %v", IsOverloadedError(err), tc.wantLoaded)
			}
		})
	}
}

func TestGeminiProviderOverloadedMidStream(t *testing.T) {
	// Mid-stream error objects with UNAVAILABLE/RESOURCE_EXHAUSTED carry the
	// overloaded message; other statuses stay generic. Neither leaks the
	// provider body to the client.
	for _, tc := range []struct {
		name       string
		object     string
		wantLoaded bool
	}{
		{"unavailable", `{"error":{"code":503,"message":"high demand","status":"UNAVAILABLE"}}`, true},
		{"resource exhausted", `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, true},
		{"permission denied", `{"error":{"code":403,"message":"bad key","status":"PERMISSION_DENIED"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "no flusher", http.StatusInternalServerError)
					return
				}
				fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`)
				flusher.Flush()
				fmt.Fprintln(w, "data: "+tc.object)
				flusher.Flush()
			}))
			defer srv.Close()

			p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
			ch, err := p.CompleteStream(context.Background(), Request{
				Model:    "gemini-2.5-flash",
				Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
			})
			if err != nil {
				t.Fatalf("CompleteStream() failed: %v", err)
			}
			var chunkErr error
			var sawDone bool
			for chunk := range ch {
				if chunk.Err != nil {
					chunkErr = chunk.Err
				}
				if chunk.Done {
					sawDone = true
				}
			}
			if chunkErr == nil {
				t.Fatal("mid-stream error object must produce an Err chunk")
			}
			if sawDone {
				t.Error("errored stream must not end with Done")
			}
			want := GenericProviderMessage
			if tc.wantLoaded {
				want = OverloadedMessage
			}
			if got := errorMessage(t, chunkErr); got != want {
				t.Errorf("message = %q, want %q", got, want)
			}
			if strings.Contains(chunkErr.Error(), "high demand") || strings.Contains(chunkErr.Error(), "bad key") {
				t.Errorf("provider body leaked into client error: %v", chunkErr)
			}
		})
	}
}

func TestGeminiProviderGenerationConfigOmittedByDefault(t *testing.T) {
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	for range ch {
	}
	if _, ok := gotPayload["generationConfig"]; ok {
		t.Errorf("generationConfig must be omitted when unset, got %v", gotPayload["generationConfig"])
	}
}

func TestGeminiProviderGenerationConfigWhenSet(t *testing.T) {
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	thinking := 0
	p := NewGeminiProvider(config.AIConfig{
		APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second,
		MaxOutputTokens: 8192, ThinkingBudget: &thinking,
	}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	for range ch {
	}
	genCfg, ok := gotPayload["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("generationConfig missing, got payload keys: %v", keysOf(gotPayload))
	}
	if genCfg["maxOutputTokens"] != float64(8192) {
		t.Errorf("maxOutputTokens = %v, want 8192", genCfg["maxOutputTokens"])
	}
	thinkingCfg, ok := genCfg["thinkingConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("thinkingConfig missing in %v", genCfg)
	}
	if thinkingCfg["thinkingBudget"] != float64(0) {
		t.Errorf("thinkingBudget = %v, want 0", thinkingCfg["thinkingBudget"])
	}
}

// errorMessage extracts the client-safe message from an apperror error,
// asserting the code stays AI_PROVIDER_ERROR.
func errorMessage(t *testing.T, err error) string {
	t.Helper()
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apperror.Error, got %T (%v)", err, err)
	}
	if ae.Code() != "AI_PROVIDER_ERROR" {
		t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
	}
	return ae.Message
}

func TestGeminiProviderErrorReasons(t *testing.T) {
	// Reason classification at creation: overload (pre-stream 503 and
	// mid-stream UNAVAILABLE) vs generic PROVIDER_ERROR (400). The code
	// stays AI_PROVIDER_ERROR everywhere.
	t.Run("prestream overloaded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, `{"error":{"status":"UNAVAILABLE"}}`)
		}))
		defer srv.Close()

		p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
		_, err := p.CompleteStream(context.Background(), Request{
			Model:    "gemini-2.5-flash",
			Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
		})
		if err == nil {
			t.Fatal("expected error for status 503")
		}
		var ae *apperror.Error
		if !errors.As(err, &ae) {
			t.Fatalf("expected *apperror.Error, got %T", err)
		}
		if ae.Reason != apperror.ReasonProviderOverloaded {
			t.Errorf("reason = %q, want PROVIDER_OVERLOADED", ae.Reason)
		}
	})

	t.Run("prestream generic", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, `{"error":{"status":"INVALID_ARGUMENT"}}`)
		}))
		defer srv.Close()

		p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
		_, err := p.CompleteStream(context.Background(), Request{
			Model:    "gemini-2.5-flash",
			Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
		})
		if err == nil {
			t.Fatal("expected error for status 400")
		}
		var ae *apperror.Error
		if !errors.As(err, &ae) {
			t.Fatalf("expected *apperror.Error, got %T", err)
		}
		if ae.Reason != apperror.ReasonProviderError {
			t.Errorf("reason = %q, want PROVIDER_ERROR", ae.Reason)
		}
	})

	t.Run("midstream overloaded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", http.StatusInternalServerError)
				return
			}
			fmt.Fprintln(w, `data: {"error":{"code":503,"message":"high demand","status":"UNAVAILABLE"}}`)
			flusher.Flush()
		}))
		defer srv.Close()

		p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
		ch, err := p.CompleteStream(context.Background(), Request{
			Model:    "gemini-2.5-flash",
			Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
		})
		if err != nil {
			t.Fatalf("CompleteStream() failed: %v", err)
		}
		var chunkErr error
		for chunk := range ch {
			if chunk.Err != nil {
				chunkErr = chunk.Err
			}
		}
		var ae *apperror.Error
		if !errors.As(chunkErr, &ae) {
			t.Fatalf("expected *apperror.Error chunk, got %T", chunkErr)
		}
		if ae.Reason != apperror.ReasonProviderOverloaded {
			t.Errorf("reason = %q, want PROVIDER_OVERLOADED", ae.Reason)
		}
	})
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGeminiProviderSkipsThoughtParts(t *testing.T) {
	// Parts with thought=true are reasoning, not output: only real text
	// must be emitted, never the thinking trace.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"parts":[{"text":"secret reasoning","thought":true},{"text":"<html>"}]},"finishReason":"STOP"}]}`)
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Done {
			continue
		}
		sb.WriteString(chunk.Token)
	}
	if sb.String() != "<html>" {
		t.Errorf("tokens = %q, want only non-thought text %q", sb.String(), "<html>")
	}
}

func TestGeminiProviderHugeLineBeyondScannerCap(t *testing.T) {
	// bufio.Reader has no line cap: a 2 MiB data line (beyond both the old
	// 64 KiB Scanner default and the 1 MiB tuned cap) must be read whole.
	big := strings.Repeat("y", 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}]},\"finishReason\":\"STOP\"}]}\n\n", big)
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "test-key", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())
	ch, err := p.CompleteStream(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream() failed: %v", err)
	}
	var sb strings.Builder
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("huge line must not error the stream: %v", chunk.Err)
		}
		sb.WriteString(chunk.Token)
		if chunk.Done {
			done = true
		}
	}
	if !done {
		t.Error("expected stream to end with done=true")
	}
	if sb.String() != big {
		t.Errorf("token bytes = %d, want %d (line truncated?)", sb.Len(), len(big))
	}
}
