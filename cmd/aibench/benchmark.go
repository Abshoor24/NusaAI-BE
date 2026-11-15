package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/service"
)

type RawRunData struct {
	RunNumber     int                     `json:"run_number"`
	FirstChunkMs  int64                   `json:"first_chunk_ms"`
	FirstTextMs   int64                   `json:"first_text_ms"`
	TotalMs       int64                   `json:"total_ms"`
	UsageMetadata *provider.UsageMetadata `json:"usage_metadata,omitempty"`
	FinishReason  string                  `json:"finish_reason"`
	Status        string                  `json:"status"`
	HTMLFile      string                  `json:"html_file"`
	AttemptCount  int                     `json:"attempt_count"`
	Error429      int                     `json:"error_429"`
	Error503      int                     `json:"error_503"`
	Error         string                  `json:"error,omitempty"`
}

type BenchmarkResult struct {
	FirstChunkMs       int64
	FirstTextMs        int64
	Total              int64
	OutputTokenCount   int
	ThinkingTokenCount int
	TokenPerSec        float64
	FinishReason       string
	ValidHTML          bool
	HTML               string
	PromptTokens       int
	CandidatesTokens   int
	TotalTokens        int
	AttemptCount       int
	Error429           int
	Error503           int
	Status             string
	Error              string
	HTMLPath           string
	RawJSONPath        string
}

type runOptions struct {
	Model           string
	ThinkingLevel   string
	MaxOutputTokens int
	OutFolder       string
	BriefName       string
	RunNumber       int
	Backoffs        []time.Duration
	IdleTimeout     time.Duration
	Context         context.Context
}

func defaultRunOptions() runOptions {
	return runOptions{
		Backoffs:    []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second},
		IdleTimeout: 120 * time.Second,
		Context:     context.Background(),
	}
}

func runBenchmark(p provider.Provider, brief Brief, opt runOptions) BenchmarkResult {
	if opt.Context == nil {
		opt.Context = context.Background()
	}
	if len(opt.Backoffs) == 0 {
		opt.Backoffs = []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	}
	if opt.IdleTimeout <= 0 {
		opt.IdleTimeout = 120 * time.Second
	}
	start := time.Now()

	userPrompt := service.BuildGeneratePrompt(brief.Text)
	req := provider.Request{
		Model: opt.Model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: service.SystemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	var streamCh <-chan provider.StreamChunk
	var streamErr error
	attemptCount := 0
	err429, err503 := 0, 0

	for attempt := 0; attempt < 3; attempt++ {
		attemptCount = attempt + 1
		streamCh, streamErr = p.CompleteStream(opt.Context, req)
		if streamErr == nil {
			break
		}
		countError(streamErr, &err429, &err503)
		if !isOverloadError(streamErr) {
			break
		}
		if attempt < 2 {
			backoff := opt.Backoffs[attempt%len(opt.Backoffs)]
			log.Printf("Percobaan %d gagal (overload), mencoba lagi dalam %v: %v", attemptCount, backoff, streamErr)
			select {
			case <-time.After(backoff):
			case <-opt.Context.Done():
				return BenchmarkResult{
					Total: time.Since(start).Milliseconds(), Error: opt.Context.Err().Error(),
					AttemptCount: attemptCount, Error429: err429, Error503: err503, Status: "error",
				}
			}
		}
	}

	if streamErr != nil {
		log.Printf("Provider error setelah %d percobaan: %v", attemptCount, streamErr)
		return BenchmarkResult{
			Total: time.Since(start).Milliseconds(), Error: streamErr.Error(),
			AttemptCount: attemptCount, Error429: err429, Error503: err503, Status: "error",
		}
	}

	var buf strings.Builder
	var firstChunkTime, firstTextTime time.Time
	var firstChunkReceived, firstTextReceived bool
	var finishReason string
	var usage *provider.UsageMetadata
	var streamFailure error

	idleTimer := time.NewTimer(opt.IdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case chunk, ok := <-streamCh:
			if !ok {
				goto streamFinished
			}
			if !firstChunkReceived {
				firstChunkReceived = true
				firstChunkTime = time.Now()
			}
			if chunk.Err != nil {
				streamFailure = chunk.Err
				countError(chunk.Err, &err429, &err503)
				goto streamFinished
			}
			if chunk.Token != "" && !firstTextReceived {
				firstTextReceived = true
				firstTextTime = time.Now()
			}
			if chunk.Token != "" {
				buf.WriteString(chunk.Token)
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.Done {
				goto streamFinished
			}
			idleTimer.Reset(opt.IdleTimeout)
		case <-idleTimer.C:
			streamFailure = fmt.Errorf("AI stream stalled for %v", opt.IdleTimeout)
			goto streamFinished
		case <-opt.Context.Done():
			streamFailure = opt.Context.Err()
			goto streamFinished
		}
	}

streamFinished:
	totalMs := time.Since(start).Milliseconds()
	firstChunkMs := totalMs
	firstTextMs := totalMs
	if firstChunkReceived {
		firstChunkMs = firstChunkTime.Sub(start).Milliseconds()
	}
	if firstTextReceived {
		firstTextMs = firstTextTime.Sub(start).Milliseconds()
	}

	denominator := float64(totalMs-firstTextMs) / 1000.0
	if denominator <= 0 {
		denominator = 0
	}
	var tokenPerSec float64
	if usage != nil && usage.CandidatesTokenCount > 0 && denominator > 0 {
		tokenPerSec = float64(usage.CandidatesTokenCount) / denominator
	}

	var promptTokens, candidatesTokens, thinkingTokens, totalTokens int
	if usage != nil {
		promptTokens = usage.PromptTokenCount
		candidatesTokens = usage.CandidatesTokenCount
		thinkingTokens = usage.ThoughtsTokenCount
		totalTokens = usage.TotalTokenCount
	}

	htmlText := buf.String()
	lower := strings.ToLower(htmlText)
	validHTML := (strings.Contains(lower, "<!doctype html>") || strings.Contains(lower, "<html>")) &&
		strings.Contains(lower, "</html>")

	status := "ok"
	errorText := ""
	if streamFailure != nil {
		status = "error"
		errorText = streamFailure.Error()
	} else if !validHTML {
		status = "invalid_html"
		errorText = "output is not a complete HTML document"
	}

	return BenchmarkResult{
		FirstChunkMs:       firstChunkMs,
		FirstTextMs:        firstTextMs,
		Total:              totalMs,
		OutputTokenCount:   candidatesTokens,
		ThinkingTokenCount: thinkingTokens,
		TokenPerSec:        tokenPerSec,
		FinishReason:       finishReason,
		ValidHTML:          validHTML,
		HTML:               htmlText,
		PromptTokens:       promptTokens,
		CandidatesTokens:   candidatesTokens,
		TotalTokens:        totalTokens,
		AttemptCount:       attemptCount,
		Error429:           err429,
		Error503:           err503,
		Status:             status,
		Error:              errorText,
	}
}

func countError(err error, err429, err503 *int) {
	if err == nil {
		return
	}
	text := err.Error()
	if strings.Contains(text, "429") || strings.Contains(text, "RESOURCE_EXHAUSTED") {
		*err429++
	}
	if strings.Contains(text, "503") || strings.Contains(text, "UNAVAILABLE") || strings.Contains(strings.ToLower(text), "overload") {
		*err503++
	}
}

func isOverloadError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "503") ||
		strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "UNAVAILABLE") ||
		strings.Contains(strings.ToLower(s), "overloaded")
}

func thinkingLabel(level string) string {
	if strings.TrimSpace(level) == "" {
		return "default"
	}
	return strings.ToLower(level)
}

var unsafeFilenameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeFilename(value string) string {
	value = unsafeFilenameRE.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-._")
	if value == "" {
		return "unknown"
	}
	return value
}

func outputBase(briefName, model, thinking string, run int) string {
	return fmt.Sprintf("%s_%s_%s_run%d",
		sanitizeFilename(briefName), sanitizeFilename(model), thinkingLabel(thinking), run)
}

var pathMu sync.Mutex

func uniquePath(path string) string {
	pathMu.Lock()
	defer pathMu.Unlock()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func persistResult(result *BenchmarkResult, opt runOptions) error {
	if err := os.MkdirAll(opt.OutFolder, 0o755); err != nil {
		return err
	}
	base := outputBase(opt.BriefName, opt.Model, opt.ThinkingLevel, opt.RunNumber)
	htmlPath := uniquePath(filepath.Join(opt.OutFolder, base+".html"))
	if err := os.WriteFile(htmlPath, []byte(result.HTML), 0o644); err != nil {
		return err
	}
	result.HTMLPath = htmlPath

	raw := RawRunData{
		RunNumber:    opt.RunNumber,
		FirstChunkMs: result.FirstChunkMs,
		FirstTextMs:  result.FirstTextMs,
		TotalMs:      result.Total,
		UsageMetadata: &provider.UsageMetadata{
			PromptTokenCount:     result.PromptTokens,
			CandidatesTokenCount: result.CandidatesTokens,
			ThoughtsTokenCount:   result.ThinkingTokenCount,
			TotalTokenCount:      result.TotalTokens,
		},
		FinishReason: result.FinishReason,
		Status:       result.Status,
		HTMLFile:     filepath.Base(htmlPath),
		AttemptCount: result.AttemptCount,
		Error429:     result.Error429,
		Error503:     result.Error503,
		Error:        result.Error,
	}
	rawPath := uniquePath(strings.TrimSuffix(htmlPath, ".html") + "_raw.json")
	rawBytes, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(rawPath, rawBytes, 0o644); err != nil {
		return err
	}
	result.RawJSONPath = rawPath

	csvPath := filepath.Join(opt.OutFolder, fmt.Sprintf("%s_%s_%s_runs.csv",
		sanitizeFilename(opt.BriefName), sanitizeFilename(opt.Model), thinkingLabel(opt.ThinkingLevel)))
	appendRunCSV(csvPath, raw)
	return nil
}

func appendRunCSV(path string, raw RawRunData) {
	fileExists := false
	if _, err := os.Stat(path); err == nil {
		fileExists = true
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("Gagal buka CSV %s: %v", path, err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if !fileExists {
		_ = w.Write([]string{"run_number", "first_chunk_ms", "first_text_ms", "total_ms",
			"prompt_tokens", "candidates_tokens", "thoughts_tokens", "total_tokens",
			"finish_reason", "status", "html_file", "attempt_count", "error_429", "error_503", "error"})
	}
	_ = w.Write([]string{
		fmt.Sprintf("%d", raw.RunNumber),
		fmt.Sprintf("%d", raw.FirstChunkMs),
		fmt.Sprintf("%d", raw.FirstTextMs),
		fmt.Sprintf("%d", raw.TotalMs),
		fmt.Sprintf("%d", raw.UsageMetadata.PromptTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.CandidatesTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.ThoughtsTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.TotalTokenCount),
		raw.FinishReason,
		raw.Status,
		raw.HTMLFile,
		fmt.Sprintf("%d", raw.AttemptCount),
		fmt.Sprintf("%d", raw.Error429),
		fmt.Sprintf("%d", raw.Error503),
		raw.Error,
	})
	w.Flush()
}

func runParallel(p provider.Provider, brief Brief, opt runOptions, n int) []BenchmarkResult {
	results := make([]BenchmarkResult, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			opts := opt
			opts.RunNumber = idx + 1
			results[idx] = runBenchmark(p, brief, opts)
		}(i)
	}
	wg.Wait()
	return results
}
