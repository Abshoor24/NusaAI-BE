package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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