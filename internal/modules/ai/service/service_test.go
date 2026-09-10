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

func TestChatAssemblesMessages(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "hi there", Model: "mock-model"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	resp, err := svc.Chat(context.Background(), model.ChatRequest{
		Message: "Halo",
		History: []model.ChatMessage{
			{Role: "user", Content: "Hai"},
			{Role: "assistant", Content: "Halo!"},
		},
		System: "You are a bakery assistant.",
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Response != "hi there" {
		t.Errorf("response = %q, want hi there", resp.Response)
	}
	if got.Model != "test-model" {
		t.Errorf("model = %q, want test-model", got.Model)
	}
	want := []provider.ChatMessage{
		{Role: "system", Content: "You are a bakery assistant."},
		{Role: "user", Content: "Hai"},
		{Role: "assistant", Content: "Halo!"},
		{Role: "user", Content: "Halo"},
	}
	if len(got.Messages) != len(want) {
		t.Fatalf("messages length = %d, want %d", len(got.Messages), len(want))
	}
	for i, w := range want {
		if got.Messages[i] != w {
			t.Errorf("messages[%d] = %+v, want %+v", i, got.Messages[i], w)
		}
	}
}

func TestChatDefaultSystemPrompt(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	_, err := svc.Chat(context.Background(), model.ChatRequest{Message: "Hai"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages length = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content == "" {
		t.Errorf("default system prompt missing: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "Hai" {
		t.Errorf("user message = %+v", got.Messages[1])
	}
}

func TestChatProviderError(t *testing.T) {
	providerErr := apperror.AIProvider("The AI provider could not be reached", errors.New("connection refused"))
	svc := New(&mockProvider{err: providerErr}, config.AIConfig{Model: "test-model"}, testLogger())

	_, err := svc.Chat(context.Background(), model.ChatRequest{Message: "Hi"})
	if err == nil {
		t.Fatal("Chat() expected error")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error = %T, want *apperror.Error", err)
	}
}

func TestChatWithSolutionContext(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	solutionCtx := model.SolutionContext{
		BusinessType:    "Restaurant",
		BusinessProblem: "Customers cannot order food online",
		BusinessGoal:    "Increase online sales",
		Solution: model.GeneratedSolution{
			SolutionName:          "Online Restaurant Ordering Platform",
			Summary:               "A platform for online food ordering",
			RecommendedFeatures:   []string{"Digital Menu", "Online Ordering", "Payment Integration"},
			RecommendedTechnology: []string{"React", "Go", "PostgreSQL"},
			Complexity:            "Medium",
		},
	}

	_, err := svc.Chat(context.Background(), model.ChatRequest{
		Message:         "Berapa lama kira-kira membuat platform ini?",
		SolutionContext: &solutionCtx,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if len(got.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(got.Messages))
	}

	// Verify system prompt contains solution context
	systemMsg := got.Messages[0]
	if systemMsg.Role != "system" {
		t.Errorf("first message should be system, got %s", systemMsg.Role)
	}
	if !contains(systemMsg.Content, "Generated Solution Context") {
		t.Errorf("system prompt should contain solution context marker")
	}
	if !contains(systemMsg.Content, "Restaurant") {
		t.Errorf("system prompt should contain business type")
	}
	if !contains(systemMsg.Content, "Online Restaurant Ordering Platform") {
		t.Errorf("system prompt should contain solution name")
	}
	if !contains(systemMsg.Content, "Digital Menu") {
		t.Errorf("system prompt should contain features")
	}
	if !contains(systemMsg.Content, "React") {
		t.Errorf("system prompt should contain technology")
	}
	if !contains(systemMsg.Content, "Medium") {
		t.Errorf("system prompt should contain complexity")
	}
}

func TestChatWithSolutionContextAndHistory(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	solutionCtx := model.SolutionContext{
		BusinessType:    "Restaurant",
		BusinessProblem: "Customers cannot order food online",
		BusinessGoal:    "Increase online sales",
		Solution: model.GeneratedSolution{
			SolutionName:          "Online Restaurant Ordering Platform",
			Summary:               "A platform for online food ordering",
			RecommendedFeatures:   []string{"Digital Menu"},
			RecommendedTechnology: []string{"React"},
			Complexity:            "Medium",
		},
	}

	_, err := svc.Chat(context.Background(), model.ChatRequest{
		Message: "Berapa lama?",
		History: []model.ChatMessage{
			{Role: "user", Content: "Halo"},
			{Role: "assistant", Content: "Hai! Ada yang bisa dibantu?"},
		},
		SolutionContext: &solutionCtx,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	// Should have: system, history user, history assistant, user
	if len(got.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "system" {
		t.Errorf("message 0 should be system")
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "Halo" {
		t.Errorf("message 1 should be history user")
	}
	if got.Messages[2].Role != "assistant" || got.Messages[2].Content != "Hai! Ada yang bisa dibantu?" {
		t.Errorf("message 2 should be history assistant")
	}
	if got.Messages[3].Role != "user" || got.Messages[3].Content != "Berapa lama?" {
		t.Errorf("message 3 should be current user message")
	}
}

func TestChatWithEmptySolutionContext(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	// Empty solution context (nil pointer) should work like before
	_, err := svc.Chat(context.Background(), model.ChatRequest{
		Message:         "Hai",
		SolutionContext: nil,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "system" {
		t.Errorf("message 0 should be system")
	}
	// Should not contain solution context marker
	if contains(got.Messages[0].Content, "Generated Solution Context") {
		t.Errorf("system prompt should not contain solution context when nil")
	}
}

func TestChatWithSolutionContextAndCustomSystem(t *testing.T) {
	var got provider.Request
	svc := New(&recordingProvider{onComplete: func(req provider.Request) provider.Response {
		got = req
		return provider.Response{Content: "ok", Model: "m"}
	}}, config.AIConfig{Model: "test-model"}, testLogger())

	solutionCtx := model.SolutionContext{
		BusinessType:    "Restaurant",
		BusinessProblem: "Customers cannot order food online",
		BusinessGoal:    "Increase online sales",
		Solution: model.GeneratedSolution{
			SolutionName:          "Online Restaurant Ordering Platform",
			Summary:               "A platform for online food ordering",
			RecommendedFeatures:   []string{"Digital Menu"},
			RecommendedTechnology: []string{"React"},
			Complexity:            "Medium",
		},
	}

	_, err := svc.Chat(context.Background(), model.ChatRequest{
		Message:         "Test",
		System:          "You are a specialized assistant.",
		SolutionContext: &solutionCtx,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	systemMsg := got.Messages[0]
	if !contains(systemMsg.Content, "You are a specialized assistant.") {
		t.Errorf("system prompt should contain custom system")
	}
	if !contains(systemMsg.Content, "Generated Solution Context") {
		t.Errorf("system prompt should contain solution context")
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
