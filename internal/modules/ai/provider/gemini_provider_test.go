package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"halo"},{"text":" dunia"}]}}],"modelVersion":"gemini-2.5-flash"}`))
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, testLogger())

	resp, err := p.Complete(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a bakery assistant."},
			{Role: "user", Content: "Hai"},
			{Role: "assistant", Content: "Halo!"},
			{Role: "user", Content: "Jam buka?"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if gotPath != "/models/gemini-2.5-flash:generateContent" {
		t.Errorf("path = %q, want generateContent path", gotPath)
	}
	if gotAuth != "test-key" {
		t.Errorf("x-goog-api-key = %q, want test-key", gotAuth)
	}

	if resp.Content != "halo dunia" {
		t.Errorf("content = %q, want \"halo dunia\"", resp.Content)
	}
	if resp.Model != "gemini-2.5-flash" {
		t.Errorf("model = %q, want gemini-2.5-flash", resp.Model)
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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "k", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())

	_, err := p.Complete(context.Background(), Request{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: "a"},
			{Role: "user", Content: "b"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
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

func TestGeminiProviderLegacyPath(t *testing.T) {
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}`))
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{APIKey: "k", BaseURL: srv.URL, Timeout: 5 * time.Second}, testLogger())

	_, err := p.Complete(context.Background(), Request{
		Model:   "gemini-2.5-flash",
		Prompt:  "Question",
		Context: []string{"doc line"},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	si := gotPayload["systemInstruction"].(map[string]interface{})
	if si["parts"].([]interface{})[0].(map[string]interface{})["text"] != "doc line" {
		t.Errorf("legacy context not mapped to systemInstruction: %v", si)
	}
	contents := gotPayload["contents"].([]interface{})
	if len(contents) != 1 || contents[0].(map[string]interface{})["role"] != "user" {
		t.Errorf("legacy prompt not mapped to user content: %v", contents)
	}
}

func TestGeminiProviderRequiresModel(t *testing.T) {
	p := NewGeminiProvider(config.AIConfig{APIKey: "k", BaseURL: "http://localhost", Timeout: time.Second}, testLogger())
	_, err := p.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("Complete() expected error when model is empty")
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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"That is a golden retriever puppy."}]}}],"modelVersion":"gemini-2.5-flash"}`))
	}))
	defer srv.Close()

	p := NewGeminiProvider(config.AIConfig{
		APIKey:  "test-api-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, testLogger())

	fakeData := []byte("raw-image-bytes")
	resp, err := p.Complete(context.Background(), Request{
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
		t.Fatalf("Complete() multimodal failed: %v", err)
	}

	if resp.Content != "That is a golden retriever puppy." {
		t.Errorf("content = %q, want 'That is a golden retriever puppy.'", resp.Content)
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

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
