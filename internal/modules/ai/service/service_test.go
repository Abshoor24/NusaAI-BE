package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockProvider simulates an LLM provider without any real API call.
type mockProvider struct {
	content string
	model   string
	err     error
}

func (m *mockProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	if m.err != nil {
		return provider.Response{}, m.err
	}
	return provider.Response{Content: "echo: " + req.Prompt, Model: "mock-model"}, nil
}

func newTestService(p provider.Provider) Service {
	return New(p, config.AIConfig{Model: "test-model"}, testLogger())
}

func TestGenerateTextSuccess(t *testing.T) {
	svc := newTestService(&mockProvider{})
	ctx := context.Background()

	resp, err := svc.GenerateText(ctx, model.GenerateTextRequest{
		Prompt:  "Hello",
		Context: []string{"context line"},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if resp.Content != "echo: Hello" {
		t.Errorf("content = %q, want %q", resp.Content, "echo: Hello")
	}
	if resp.Model != "mock-model" {
		t.Errorf("model = %q, want mock-model", resp.Model)
	}
}

func TestGenerateTextProviderError(t *testing.T) {
	providerErr := apperror.AIProvider("The AI provider could not be reached", errors.New("connection refused"))
	svc := newTestService(&mockProvider{err: providerErr})

	_, err := svc.GenerateText(context.Background(), model.GenerateTextRequest{Prompt: "Hi"})
	if err == nil {
		t.Fatal("GenerateText() expected error")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error = %T, want *apperror.Error", err)
	}
	if ae.Code() != "AI_PROVIDER_ERROR" {
		t.Errorf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
	}
	// The underlying cause must stay server-side only.
	if ae.Err == nil {
		t.Error("expected the cause to be wrapped for server-side logging")
	}
}

func TestGenerateTextPassesContext(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	_, err := svc.GenerateText(context.Background(), model.GenerateTextRequest{
		Prompt:  "Q",
		Context: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got.Model != "test-model" || got.Prompt != "Q" || len(got.Context) != 2 {
		t.Errorf("provider received %+v", got)
	}
}

type recordingProvider struct {
	onComplete func(req provider.Request) provider.Response
}

func (r *recordingProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	return r.onComplete(req), nil
}
