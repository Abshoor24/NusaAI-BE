package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
)


func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		input       string
		expectedCan string
		expectedID  int
		expectedOK  bool
	}{
		{"python", "python", 71, true},
		{"Python", "python", 71, true},
		{"PY", "python", 71, true},
		{"python3", "python", 71, true},
		{"javascript", "javascript", 63, true},
		{"JS", "javascript", 63, true},
		{"node", "javascript", 63, true},
		{"nodejs", "javascript", 63, true},
		{"go", "go", 60, true},
		{"Go", "go", 60, true},
		{"golang", "go", 60, true},
		{"ruby", "", 0, false},
		{"cpp", "", 0, false},
		{"bash", "", 0, false},
		{"", "", 0, false},
	}

	for _, tt := range tests {
		can, id, ok := NormalizeLanguage(tt.input)
		if ok != tt.expectedOK {
			t.Errorf("NormalizeLanguage(%q) ok = %v, expected %v", tt.input, ok, tt.expectedOK)
		}
		if ok {
			if can != tt.expectedCan {
				t.Errorf("NormalizeLanguage(%q) canonical = %q, expected %q", tt.input, can, tt.expectedCan)
			}
			if id != tt.expectedID {
				t.Errorf("NormalizeLanguage(%q) id = %d, expected %d", tt.input, id, tt.expectedID)
			}
		}
	}
}

func TestJudgeZeroProvider_Execute_Success(t *testing.T) {
	var pollCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/submissions") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"token": "fake-token-1",
			})
			return
		}

		if r.Method == http.MethodGet && r.URL.Path == "/submissions/fake-token-1" {
			count := atomic.AddInt32(&pollCount, 1)
			if count == 1 {
				// Still processing
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": map[string]interface{}{
						"id":          2,
						"description": "Processing",
					},
				})
				return
			}

			// Finished successfully
			stdout := "Hello, Judge0!\n"
			exitCode := 0
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": map[string]interface{}{
					"id":          3,
					"description": "Accepted",
				},
				"stdout":    &stdout,
				"exit_code": &exitCode,
				"time":      "0.035",
				"memory":    2400,
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config.ExecutionConfig{
		BaseURL:          server.URL,
		CPUTimeLimitSec:  5,
		WallTimeLimitSec: 10,
		MemoryLimitKB:    131072,
		PollInterval:     10 * time.Millisecond,
		MaxPollDuration:  2 * time.Second,
	}

	p := NewJudgeZeroProvider(cfg, testLogger())
	res, err := p.Execute(context.Background(), ExecutionRequest{
		Language: "python",
		Code:     "print('Hello, Judge0!')",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != "Accepted" {
		t.Errorf("expected status 'Accepted', got %q", res.Status)
	}
	if res.Stdout != "Hello, Judge0!\n" {
		t.Errorf("expected stdout 'Hello, Judge0!\n', got %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit_code 0, got %d", res.ExitCode)
	}
	if res.ExecutionTimeMs != 35 {
		t.Errorf("expected execution_time_ms 35, got %d", res.ExecutionTimeMs)
	}
	if res.MemoryKB != 2400 {
		t.Errorf("expected memory_kb 2400, got %d", res.MemoryKB)
	}
}

func TestJudgeZeroProvider_Execute_TimeLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "timeout-token"})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/submissions/timeout-token" {
			msg := "Time Limit Exceeded"
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": map[string]interface{}{
					"id":          5,
					"description": "Time Limit Exceeded",
				},
				"message": &msg,
				"time":    "5.001",
				"memory":  12000,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config.ExecutionConfig{
		BaseURL:         server.URL,
		PollInterval:    10 * time.Millisecond,
		MaxPollDuration: 2 * time.Second,
	}

	p := NewJudgeZeroProvider(cfg, testLogger())
	res, err := p.Execute(context.Background(), ExecutionRequest{
		Language: "python",
		Code:     "while True: pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != "Time Limit Exceeded" {
		t.Errorf("expected status 'Time Limit Exceeded', got %q", res.Status)
	}
	if res.ExitCode != 124 {
		t.Errorf("expected timeout exit_code 124, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "Time Limit Exceeded") {
		t.Errorf("expected stderr to contain 'Time Limit Exceeded', got %q", res.Stderr)
	}
}

func TestJudgeZeroProvider_Execute_CompileError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "compile-err-token"})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/submissions/compile-err-token" {
			compOut := "syntax error: unexpected newline"
			exitCode := 1
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": map[string]interface{}{
					"id":          6,
					"description": "Compilation Error",
				},
				"compile_output": &compOut,
				"exit_code":      &exitCode,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config.ExecutionConfig{
		BaseURL:         server.URL,
		PollInterval:    10 * time.Millisecond,
		MaxPollDuration: 2 * time.Second,
	}

	p := NewJudgeZeroProvider(cfg, testLogger())
	res, err := p.Execute(context.Background(), ExecutionRequest{
		Language: "go",
		Code:     "package main\nfunc main() { invalid syntax",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != "Compilation Error" {
		t.Errorf("expected status 'Compilation Error', got %q", res.Status)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit_code 1, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "syntax error") {
		t.Errorf("expected stderr to contain 'syntax error', got %q", res.Stderr)
	}
}

func TestJudgeZeroProvider_Execute_UnsupportedLanguage(t *testing.T) {
	p := NewJudgeZeroProvider(config.ExecutionConfig{}, testLogger())
	_, err := p.Execute(context.Background(), ExecutionRequest{
		Language: "ruby",
		Code:     "puts 'hello'",
	})
	if err == nil {
		t.Fatal("expected error for unsupported language, got nil")
	}
	if !strings.Contains(err.Error(), "ruby") {
		t.Errorf("expected error to mention 'ruby', got %v", err)
	}
}
