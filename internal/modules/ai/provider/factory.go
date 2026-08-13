package provider

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/Abshoor24/backend-go/internal/config"
)

// New returns the Provider implementation selected by AI_PROVIDER. This keeps
// the AI service and handlers decoupled from any concrete LLM SDK: adding a
// new provider (OpenAI, Anthropic, ...) only means adding a case here.
func New(cfg config.AIConfig, logger *slog.Logger) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "gemini":
		return NewGeminiProvider(cfg, logger), nil
	default:
		return nil, fmt.Errorf("ai: unsupported AI provider %q", cfg.Provider)
	}
}
