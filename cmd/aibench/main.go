package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/service"
)

type BenchmarkConfig struct {
	Model           string
	ThinkingLevel   string
	MaxOutputTokens int
	Brief           string
	Runs            int
	OutFolder       string
}

// RawRunData menyimpan metadata mentah per run untuk dilaporkan as-is.
type RawRunData struct {
	RunNumber     int                     `json:"run_number"`
	FirstChunkMs  int64                   `json:"first_chunk_ms"`
	FirstTextMs   int64                   `json:"first_text_ms"`
	TotalMs       int64                   `json:"total_ms"`
	UsageMetadata *provider.UsageMetadata `json:"usage_metadata,omitempty"`
	FinishReason  string                  `json:"finish_reason"`
	HTMLFile      string                  `json:"html_file"`
	AttemptCount  int                     `json:"attempt_count"`
	Error         string                  `json:"error,omitempty"`
}

type BenchmarkResult struct {
	// Definisi (lihat juga komentar di runBenchmark):
	//   FirstChunkMs : waktu dari request dikirim sampai chunk apa pun pertama.
	//   FirstTextMs  : waktu dari request dikirim sampai token teks non-thought pertama.
	//   Total        : waktu dari request dikirim sampai chunk Done (ms).
	//   TokenPerSec  : candidatesTokenCount / ((Total - FirstTextMs)/1000).
	//                  Penyebut dihitung sejak teks pertama tampil, bukan sejak
	//                  request dikirim, karena fase thinking bukan bagian dari
	//                  produksi teks.
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
	Error              string
	HTMLPath           string
	RawJSONPath        string
}

func main() {
	args := os.Args[1:]
	var cfg BenchmarkConfig

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				cfg.Model = args[i+1]
				i++
			}
		case "--thinking":
			if i+1 < len(args) {
				cfg.ThinkingLevel = args[i+1]
				i++
			}
		case "--max-output":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &cfg.MaxOutputTokens)
				i++
			}
		case "--brief":
			if i+1 < len(args) {
				cfg.Brief = args[i+1]
				i++
			}
		case "--runs":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &cfg.Runs)
				i++
			}
		case "--out":
			if i+1 < len(args) {
				cfg.OutFolder = args[i+1]
				i++
			}
		}
	}

	if cfg.Model == "" {
		cfg.Model = "gemini-3.8-flash"
	}
	if cfg.Runs <= 0 {
		cfg.Runs = 3
	}
	if cfg.OutFolder == "" {
		cfg.OutFolder = "./aibench-results"
	}

	// Load config to get AI settings (including API key)
	cfgFull, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	aiCfg := cfgFull.AI
	aiCfg.Model = cfg.Model
	aiCfg.Timeout = 300 * time.Second

	// Handle thinking level for Gemini 3.x
	if cfg.ThinkingLevel != "" {
		switch strings.ToUpper(cfg.ThinkingLevel) {
		case "MINIMAL", "LOW", "MEDIUM", "HIGH":
			aiCfg.ThinkingLevel = &cfg.ThinkingLevel
			aiCfg.ThinkingBudget = nil
		default:
			// Unknown level: try to use it, let provider handle it
			aiCfg.ThinkingLevel = &cfg.ThinkingLevel
			aiCfg.ThinkingBudget = nil
		}
	} else {
		aiCfg.ThinkingLevel = nil
		aiCfg.ThinkingBudget = nil
	}

	if cfg.MaxOutputTokens > 0 {
		aiCfg.MaxOutputTokens = int64(cfg.MaxOutputTokens)
	} else {
		aiCfg.MaxOutputTokens = 32768
	}

	os.MkdirAll(cfg.OutFolder, 0755)

	// Create provider - New returns (Provider, error)
	p, err := provider.New(aiCfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	if p == nil {
		log.Fatal("AI provider is not configured")
	}

	briefs := []string{
		loadBrief("brief_full"),
		loadBrief("brief_minimal"),
		loadBrief("brief_no_color"),
	}

	if cfg.Brief != "" {
		briefs = []string{cfg.Brief}
	}

	fmt.Printf("=== AI Website Generator Benchmark ===\n")
	fmt.Printf("Model: %s\n", cfg.Model)
	fmt.Printf("Thinking: %s\n", cfg.ThinkingLevel)
	fmt.Printf("MaxOutputTokens: %d\n", cfg.MaxOutputTokens)
	fmt.Printf("Runs: %d\n", cfg.Runs)
	fmt.Printf("OutFolder: %s\n", cfg.OutFolder)
	fmt.Println()

	type BriefSummary struct {
		Name          string
		FirstChunkAvg float64
		FirstTextAvg  float64
		TotalAvg      float64
		TPSAvg        float64
		ValidCount    int
		ValidPercent  float64
	}

	var summaries []BriefSummary

	for briefIdx, brief := range briefs {
		briefName := getBriefName(briefIdx)
		fmt.Printf("--- Brief: %s ---\n", briefName)

		var firstChunkSum, firstTextSum, totalSum, tpsSum float64
		var validCount int

		for run := 0; run < cfg.Runs; run++ {
			result := runBenchmark(p, cfg.Model, brief, cfg.ThinkingLevel, cfg.MaxOutputTokens, cfg.OutFolder, briefName, run+1)

			// Tulis file HTML hanya jika belum ada (jangan pernah menimpa)
			if _, err := os.Stat(result.HTMLPath); err == nil {
				log.Printf("PERINGATAN: file %s sudah ada, tidak ditimpa", result.HTMLPath)
			} else {
				if err := os.WriteFile(result.HTMLPath, []byte(result.HTML), 0644); err != nil {
					log.Printf("Gagal tulis HTML %s: %v", result.HTMLPath, err)
				}
			}

			raw := RawRunData{
				RunNumber:    run + 1,
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
				HTMLFile:     filepath.Base(result.HTMLPath),
				AttemptCount: result.AttemptCount,
				Error:        result.Error,
			}
			rawJSONPath := filepath.Join(cfg.OutFolder, fmt.Sprintf("%s_%s_run%d_raw.json", briefName, thinkingLabel(cfg.ThinkingLevel), run+1))
			rawBytes, _ := json.MarshalIndent(raw, "", "  ")
			if err := os.WriteFile(rawJSONPath, rawBytes, 0644); err != nil {
				log.Printf("Gagal tulis JSON %s: %v", rawJSONPath, err)
			}
			result.RawJSONPath = rawJSONPath

			csvPath := filepath.Join(cfg.OutFolder, fmt.Sprintf("%s_%s_runs.csv", briefName, thinkingLabel(cfg.ThinkingLevel)))
			appendRunCSV(csvPath, raw)

			firstChunkSum += float64(result.FirstChunkMs) / 1000.0
			firstTextSum += float64(result.FirstTextMs) / 1000.0
			totalSum += float64(result.Total) / 1000.0
			tpsSum += result.TokenPerSec
			if result.ValidHTML {
				validCount++
			}

			fmt.Printf("Run %d: firstChunk=%.3fs, firstText=%.3fs, Total=%.3fs, PromptTok=%d, CandTok=%d, ThinkTok=%d, Tok/s=%.1f, FinishReason=%s, Valid=%v, Attempts=%d\n",
				run+1, float64(result.FirstChunkMs)/1000.0, float64(result.FirstTextMs)/1000.0, float64(result.Total)/1000.0, result.PromptTokens,
				result.CandidatesTokens, result.ThinkingTokenCount, result.TokenPerSec, result.FinishReason, result.ValidHTML, result.AttemptCount)
			if result.Error != "" {
				fmt.Printf("  ERROR: %s\n", result.Error)
			}
		}

		n := float64(cfg.Runs)
		firstChunkAvg := firstChunkSum / n
		firstTextAvg := firstTextSum / n
		totalAvg := totalSum / n
		tpsAvg := tpsSum / n
		validPct := float64(validCount) / n * 100

		summaries = append(summaries, BriefSummary{
			Name:          briefName,
			FirstChunkAvg: firstChunkAvg,
			FirstTextAvg:  firstTextAvg,
			TotalAvg:      totalAvg,
			TPSAvg:        tpsAvg,
			ValidCount:    validCount,
			ValidPercent:  validPct,
		})

		fmt.Printf("AVERAGE: firstChunk=%.3fs, firstText=%.3fs, Total=%.3fs, Tok/s=%.1f, Valid%%=%.0f%% (%d/%d)\n\n",
			firstChunkAvg, firstTextAvg, totalAvg, tpsAvg, validPct, validCount, cfg.Runs)
	}

	fmt.Println("=== SUMMARY TABLE ===")
	fmt.Printf("%-15s %-13s %-13s %-10s %-10s %-8s\n", "Brief", "FirstChunk(s)", "FirstText(s)", "Total(s)", "Tok/s", "Valid%")
	for _, s := range summaries {
		fmt.Printf("%-15s %-13.3f %-13.3f %-10.3f %-10.1f %-8.0f\n",
			s.Name, s.FirstChunkAvg, s.FirstTextAvg, s.TotalAvg, s.TPSAvg, s.ValidPercent)
	}
}

// thinkingLabel mengembalikan label thinking untuk nama file.
func thinkingLabel(level string) string {
	if level == "" {
		return "default"
	}
	return strings.ToLower(level)
}

// appendRunCSV menambahkan baris data run ke file CSV (membuat header jika belum ada).
func appendRunCSV(path string, raw RawRunData) {
	fileExists := false
	if _, err := os.Stat(path); err == nil {
		fileExists = true
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Gagal buka CSV %s: %v", path, err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if !fileExists {
		w.Write([]string{"run_number", "first_chunk_ms", "first_text_ms", "total_ms",
			"prompt_tokens", "candidates_tokens", "thoughts_tokens", "total_tokens",
			"finish_reason", "html_file", "attempt_count", "error"})
	}
	w.Write([]string{
		fmt.Sprintf("%d", raw.RunNumber),
		fmt.Sprintf("%d", raw.FirstChunkMs),
		fmt.Sprintf("%d", raw.FirstTextMs),
		fmt.Sprintf("%d", raw.TotalMs),
		fmt.Sprintf("%d", raw.UsageMetadata.PromptTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.CandidatesTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.ThoughtsTokenCount),
		fmt.Sprintf("%d", raw.UsageMetadata.TotalTokenCount),
		raw.FinishReason,
		raw.HTMLFile,
		fmt.Sprintf("%d", raw.AttemptCount),
		raw.Error,
	})
	w.Flush()
}

func loadBrief(name string) string {
	path := filepath.Join("cmd", "aibench", "briefs", name+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return "Description: test brief\n"
	}
	return string(data)
}

func getBriefName(idx int) string {
	names := []string{"full", "minimal", "no_color"}
	if idx >= 0 && idx < len(names) {
		return names[idx]
	}
	return "unknown"
}

func runBenchmark(p provider.Provider, model string, brief string, thinkingLevel string, maxOutputTokens int, outFolder, briefName string, runNumber int) BenchmarkResult {
	ctx := context.Background()
	start := time.Now()

	briefText := brief
	userPrompt := service.BuildGeneratePrompt(briefText)

	systemPrompt := service.SystemPrompt
	var messages []provider.ChatMessage
	messages = append(messages, provider.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})
	messages = append(messages, provider.ChatMessage{
		Role:    "user",
		Content: userPrompt,
		Images:  nil,
	})

	req := provider.Request{
		Model:    model,
		Messages: messages,
	}

	// Retry logic: 3 attempts with backoff 5s, 10s, 20s for 503/429.
	// Tidak ada fallback model: jika semua percobaan gagal, run dilaporkan gagal.
	var streamCh <-chan provider.StreamChunk
	var streamErr error
	attemptCount := 0
	backoffs := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}

	for attempt := 0; attempt < 3; attempt++ {
		attemptCount = attempt + 1
		streamCh, streamErr = p.CompleteStream(ctx, req)
		if streamErr == nil {
			break
		}
		if !isOverloadError(streamErr) {
			// Bukan overload (misal 400 bad request), jangan retry.
			break
		}
		if attempt < 2 {
			backoff := backoffs[attempt]
			log.Printf("Percobaan %d gagal (overload), mencoba lagi dalam %v: %v", attemptCount, backoff, streamErr)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return BenchmarkResult{Total: time.Since(start).Milliseconds(), Error: ctx.Err().Error(), AttemptCount: attemptCount}
			}
		}
	}

	if streamErr != nil {
		log.Printf("Provider error setelah %d percobaan: %v", attemptCount, streamErr)
		return BenchmarkResult{Total: time.Since(start).Milliseconds(), Error: streamErr.Error(), AttemptCount: attemptCount}
	}

	var buf strings.Builder
	var firstChunkTime time.Time
	var firstTextTime time.Time
	var firstChunkReceived bool
	var firstTextReceived bool
	var finishReason string
	var usage *provider.UsageMetadata

	idleTimer := time.NewTimer(120 * time.Second)
	defer idleTimer.Stop()

	for chunk := range streamCh {
		// Chunk apa pun pertama (termasuk thinking / kosong / usage-only)
		if !firstChunkReceived {
			firstChunkReceived = true
			firstChunkTime = time.Now()
		}
		// Token teks pertama: provider hanya meneruskan teks non-thought
		// (lihat gemini_provider.go: bagian Thought dan Text kosong dilewati),
		// jadi chunk pertama yang membawa Token adalah teks.
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
			break
		}
		idleTimer.Reset(120 * time.Second)
	}

	totalMs := time.Since(start).Milliseconds()

	var firstChunkMs int64
	var firstTextMs int64
	if firstChunkReceived {
		firstChunkMs = firstChunkTime.Sub(start).Milliseconds()
	} else {
		firstChunkMs = totalMs
	}
	if firstTextReceived {
		firstTextMs = firstTextTime.Sub(start).Milliseconds()
	} else {
		firstTextMs = totalMs
	}

	// Token/detik = candidatesTokenCount / ((total_ms - first_text_ms)/1000).
	// Penyebut dimulai saat teks pertama tampil (setelah fase thinking),
	// sehingga mengukur laju produksi teks, bukan laju seluruh respons.
	totalSeconds := float64(totalMs) / 1000.0
	firstTextSeconds := float64(firstTextMs) / 1000.0
	denominator := totalSeconds - firstTextSeconds
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

	lower := strings.ToLower(buf.String())
	validHTML := strings.Contains(lower, "<!doctype html>") || strings.Contains(lower, "<html>")
	if !strings.Contains(lower, "</html>") {
		validHTML = false
	}

	htmlPath := filepath.Join(outFolder, fmt.Sprintf("%s_%s_run%d.html", briefName, thinkingLabel(thinkingLevel), runNumber))

	return BenchmarkResult{
		FirstChunkMs:       firstChunkMs,
		FirstTextMs:        firstTextMs,
		Total:              totalMs,
		OutputTokenCount:   candidatesTokens,
		ThinkingTokenCount: thinkingTokens,
		TokenPerSec:        tokenPerSec,
		FinishReason:       finishReason,
		ValidHTML:          validHTML,
		HTML:               buf.String(),
		PromptTokens:       promptTokens,
		CandidatesTokens:   candidatesTokens,
		TotalTokens:        totalTokens,
		AttemptCount:       attemptCount,
		HTMLPath:           htmlPath,
	}
}

// isOverloadError cek apakah error adalah overload (503/429) yang layak di-retry.
func isOverloadError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "503") ||
		strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "UNAVAILABLE") ||
		strings.Contains(s, "overloaded") || strings.Contains(s, "Overloaded")
}
