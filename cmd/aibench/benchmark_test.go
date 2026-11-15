package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

type scriptedProvider struct {
	mu     sync.Mutex
	calls  int
	errors []error
	html   string
}

func (p *scriptedProvider) CompleteStream(_ context.Context, _ provider.Request) (<-chan provider.StreamChunk, error) {
	p.mu.Lock()
	idx := p.calls
	p.calls++
	var scripted error
	if idx < len(p.errors) {
		scripted = p.errors[idx]
	}
	p.mu.Unlock()

	if scripted != nil {
		return nil, scripted
	}
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Token: p.html}
	ch <- provider.StreamChunk{
		Done:         true,
		FinishReason: "STOP",
		Usage:        &provider.UsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 20, TotalTokenCount: 30},
	}
	close(ch)
	return ch, nil
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestRunBenchmarkCounts429And503(t *testing.T) {
	p := &scriptedProvider{
		errors: []error{
			errors.New("429 RESOURCE_EXHAUSTED"),
			errors.New("503 UNAVAILABLE"),
			nil,
		},
		html: "<!DOCTYPE html><html><body>ok</body></html>",
	}
	brief := loadBriefByName(t, "full")
	opt := defaultRunOptions()
	opt.Model = "test-model"
	opt.ThinkingLevel = "LOW"
	opt.OutFolder = t.TempDir()
	opt.BriefName = brief.Name
	opt.RunNumber = 1
	opt.Backoffs = []time.Duration{time.Millisecond}

	result := runBenchmark(p, brief, opt)
	if result.Status != "ok" {
		t.Fatalf("status = %s (%s), want ok", result.Status, result.Error)
	}
	if result.AttemptCount != 3 {
		t.Errorf("attempts = %d, want 3", result.AttemptCount)
	}
	if result.Error429 != 1 || result.Error503 != 1 {
		t.Errorf("errors 429/503 = %d/%d, want 1/1", result.Error429, result.Error503)
	}
}

func TestRunParallelReturnsOneResultPerRequest(t *testing.T) {
	p := &scriptedProvider{html: "<!DOCTYPE html><html><body>ok</body></html>"}
	brief := loadBriefByName(t, "full")
	opt := defaultRunOptions()
	opt.Model = "test-model"
	opt.ThinkingLevel = "LOW"
	opt.OutFolder = t.TempDir()
	opt.BriefName = brief.Name

	results := runParallel(p, brief, opt, 3)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if p.callCount() != 3 {
		t.Fatalf("provider calls = %d, want 3", p.callCount())
	}
	for i, result := range results {
		if result.Status != "ok" {
			t.Errorf("request %d status = %s (%s)", i+1, result.Status, result.Error)
		}
		if result.AttemptCount != 1 {
			t.Errorf("request %d attempts = %d, want 1", i+1, result.AttemptCount)
		}
		if result.Error429 != 0 || result.Error503 != 0 {
			t.Errorf("request %d errors 429/503 = %d/%d", i+1, result.Error429, result.Error503)
		}
	}
}

func TestPersistResultNeverOverwritesHTML(t *testing.T) {
	brief := loadBriefByName(t, "full")
	opt := defaultRunOptions()
	opt.Model = "gemini-3.8-flash"
	opt.ThinkingLevel = "LOW"
	opt.OutFolder = t.TempDir()
	opt.BriefName = brief.Name
	opt.RunNumber = 1

	first := BenchmarkResult{HTML: "<html>one</html>", Status: "ok"}
	if err := persistResult(&first, opt); err != nil {
		t.Fatalf("persist first: %v", err)
	}
	second := BenchmarkResult{HTML: "<html>two</html>", Status: "ok"}
	if err := persistResult(&second, opt); err != nil {
		t.Fatalf("persist second: %v", err)
	}
	if first.HTMLPath == second.HTMLPath {
		t.Fatalf("second run overwrote %s", first.HTMLPath)
	}
	if !strings.Contains(first.HTMLPath, "full_gemini-3.8-flash_low_run1.html") {
		t.Errorf("unexpected filename %s", first.HTMLPath)
	}
	if _, err := os.Stat(first.HTMLPath); err != nil {
		t.Errorf("first html missing: %v", err)
	}
	if _, err := os.Stat(second.HTMLPath); err != nil {
		t.Errorf("second html missing: %v", err)
	}
	if first.RawJSONPath == second.RawJSONPath {
		t.Errorf("raw JSON path reused: %s", first.RawJSONPath)
	}
}

func TestParseArgsValidation(t *testing.T) {
	if _, err := parseArgs([]string{"--parallel", "6"}); err == nil {
		t.Error("--parallel 6: want error")
	}
	if _, err := parseArgs([]string{"--parallel", "0"}); err == nil {
		t.Error("--parallel 0: want error")
	}
	if _, err := parseArgs([]string{"--brief"}); err == nil {
		t.Error("--brief without value: want error")
	}
	if _, err := parseArgs([]string{"--unknown"}); err == nil {
		t.Error("unknown flag: want error")
	}
	cfg, err := parseArgs([]string{"--brief", "minimal", "--runs", "10", "--lint", "--parallel", "5"})
	if err != nil {
		t.Fatalf("parse valid args: %v", err)
	}
	if cfg.Brief != "minimal" || cfg.Runs != 10 || !cfg.Lint || cfg.Parallel != 5 {
		t.Errorf("parsed config = %+v", cfg)
	}
}

func TestRunCLIUnknownBriefFailsBeforeProvider(t *testing.T) {
	if code := runCLI([]string{"--brief", "does-not-exist"}); code == 0 {
		t.Fatal("unknown brief exit code = 0, want non-zero")
	}
}
