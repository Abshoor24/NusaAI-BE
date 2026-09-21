package provider

import (
	"context"
	"strings"
)

// ExecutionRequest represents the code execution request passed to an execution provider.
type ExecutionRequest struct {
	Language string `json:"language"`
	Code     string `json:"code"`
	Stdin    string `json:"stdin,omitempty"`
}

// ExecutionResult represents the normalized result and performance metrics of a code run.
type ExecutionResult struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	ExecutionTimeMs int64  `json:"execution_time_ms"`
	MemoryKB        int64  `json:"memory_kb"`
	Status          string `json:"status"`
}

// ExecutionProvider defines the contract for code execution sandboxes.
type ExecutionProvider interface {
	Execute(ctx context.Context, req ExecutionRequest) (ExecutionResult, error)
}

// NormalizeLanguage validates whether the requested programming language is in the allowed
// whitelist (python, javascript, go) and returns its canonical identifier and Judge0 language ID.
func NormalizeLanguage(lang string) (canonical string, languageID int, ok bool) {
	norm := strings.ToLower(strings.TrimSpace(lang))
	switch norm {
	case "python", "py", "python3":
		return "python", 71, true
	case "javascript", "js", "node", "nodejs":
		return "javascript", 63, true
	case "go", "golang":
		return "go", 60, true
	default:
		return "", 0, false
	}
}
