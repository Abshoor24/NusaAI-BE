package provider

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Abshoor24/backend-go/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewDefaultIsOpenAI(t *testing.T) {
	p, err := New(config.AIConfig{Provider: ""}, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("provider = %T, want *OpenAIProvider", p)
	}
}

func TestNewOpenAI(t *testing.T) {
	p, err := New(config.AIConfig{Provider: "openai"}, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("provider = %T, want *OpenAIProvider", p)
	}
}

func TestNewUnsupportedProvider(t *testing.T) {
	_, err := New(config.AIConfig{Provider: "gemini"}, testLogger())
	if err == nil {
		t.Fatal("New() expected error for unsupported provider")
	}
}
