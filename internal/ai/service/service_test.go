package ai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/USERNAME/PROJECT_NAME/internal/apperror"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockProvider simulates an LLM provider without any real API call.
type mockProvider struct {
	message string
	model   string
	err     error
}

func (m *mockProvider) Complete(_ context.Context, req providerRequest) (providerResponse, error) {
	if m.err != nil {
		return providerResponse{}, m.err
	}
	return providerResponse{Message: "echo: " + req.Message, Model: "mock-model"}, nil
}

func newTestService(p Provider) Service {
	return NewService(p, config.AIConfig{Model: "test-model"}, testLogger())
}

func TestGenerateResponseSuccess(t *testing.T) {
	svc := newTestService(&mockProvider{})
	ctx := context.Background()

	resp, err := svc.GenerateResponse(ctx, model.AIRequest{
		Message: "Hello",
		Context: []string{"context line"},
	})
	if err != nil {
		t.Fatalf("GenerateResponse() error = %v", err)
	}
	if resp.Message != "echo: Hello" {
		t.Errorf("message = %q, want %q", resp.Message, "echo: Hello")
	}
	if resp.Model != "mock-model" {
		t.Errorf("model = %q, want mock-model", resp.Model)
	}
}

func TestGenerateResponseProviderError(t *testing.T) {
	providerErr := apperror.AIProvider("The AI provider could not be reached", errors.New("connection refused"))
	svc := newTestService(&mockProvider{err: providerErr})

	_, err := svc.GenerateResponse(context.Background(), model.AIRequest{Message: "Hi"})
	if err == nil {
		t.Fatal("GenerateResponse() expected error")
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

func TestGenerateResponsePassesContext(t *testing.T) {
	var got providerRequest
	svc := NewService(&recordingProvider{onComplete: func(req providerRequest) providerResponse {
		got = req
		return providerResponse{Message: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	_, err := svc.GenerateResponse(context.Background(), model.AIRequest{
		Message: "Q",
		Context: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("GenerateResponse() error = %v", err)
	}
	if got.Model != "test-model" || got.Message != "Q" || len(got.Context) != 2 {
		t.Errorf("provider received %+v", got)
	}
}

type recordingProvider struct {
	onComplete func(req providerRequest) providerResponse
}

func (r *recordingProvider) Complete(_ context.Context, req providerRequest) (providerResponse, error) {
	return r.onComplete(req), nil
}
