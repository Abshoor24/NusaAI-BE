package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/service"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockService simulates the AI service without any provider call.
type mockService struct {
	content string
	model   string
	err     error
}

func (m *mockService) GenerateText(_ context.Context, req model.GenerateTextRequest) (model.GenerateTextResponse, error) {
	if m.err != nil {
		return model.GenerateTextResponse{}, m.err
	}
	return model.GenerateTextResponse{Content: "answer to: " + req.Prompt, Model: m.model}, nil
}

func (m *mockService) Chat(_ context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	if m.err != nil {
		return model.ChatResponse{}, m.err
	}
	return model.ChatResponse{Response: "chat reply to: " + req.Message, Model: m.model}, nil
}

func TestGenerateSuccess(t *testing.T) {
	h := New(&mockService{model: "m"}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/generate", strings.NewReader(`{"prompt":"Explain AI"}`))

	h.Generate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Content string `json:"content"`
			Model   string `json:"model"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success {
		t.Error("success = false, want true")
	}
	if body.Data.Content != "answer to: Explain AI" {
		t.Errorf("content = %q", body.Data.Content)
	}
}

func TestGenerateValidationError(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/generate", strings.NewReader(`{}`))

	h.Generate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if strings.Contains(rec.Body.String(), "AI_PROVIDER_ERROR") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestGenerateProviderError(t *testing.T) {
	h := New(&mockService{err: apperror.AIProvider("The AI provider could not be reached", errors.New("boom"))}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/generate", strings.NewReader(`{"prompt":"x"}`))

	h.Generate(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response leaks provider cause: %s", rec.Body.String())
	}
}

func TestHealthNotConfigured(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{Provider: "openai"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ai/health", nil)

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Configured bool `json:"configured"`
			Checks     struct {
				APIKey bool `json:"api_key"`
			} `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.Configured {
		t.Error("configured = true, want false when API key and model are missing")
	}
	if body.Data.Checks.APIKey {
		t.Error("checks.api_key = true, want false")
	}
	// The API key must never appear in the response.
	if strings.Contains(rec.Body.String(), "sk-") {
		t.Errorf("response leaks API key: %s", rec.Body.String())
	}
}

func TestHealthConfigured(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{Provider: "openai", APIKey: "sk-secret", Model: "gpt-4o-mini"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ai/health", nil)

	h.Health(rec, req)

	var body struct {
		Data struct {
			Configured bool `json:"configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Data.Configured {
		t.Error("configured = false, want true")
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Errorf("response leaks API key: %s", rec.Body.String())
	}
}

var _ service.Service = (*mockService)(nil)

func TestChatSuccess(t *testing.T) {
	h := New(&mockService{model: "m"}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/chat", strings.NewReader(`{"message":"Halo","history":[{"role":"user","content":"Hai"},{"role":"assistant","content":"Halo!"}]}`))

	h.Chat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Response string `json:"response"`
			Model    string `json:"model"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success {
		t.Error("success = false, want true")
	}
	if body.Data.Response != "chat reply to: Halo" {
		t.Errorf("response = %q", body.Data.Response)
	}
	if body.Data.Model != "m" {
		t.Errorf("model = %q, want m", body.Data.Model)
	}
}

func TestChatValidationError(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/chat", strings.NewReader(`{"message":""}`))

	h.Chat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChatProviderError(t *testing.T) {
	h := New(&mockService{err: apperror.AIProvider("The AI provider could not be reached", errors.New("boom"))}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/chat", strings.NewReader(`{"message":"x"}`))

	h.Chat(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response leaks provider cause: %s", rec.Body.String())
	}
}