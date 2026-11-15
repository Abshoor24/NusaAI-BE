package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

type BenchmarkConfig struct {
	Model           string
	ThinkingLevel   string
	MaxOutputTokens int
	Brief           string
	Runs            int
	OutFolder       string
	Lint            bool
	LintDir         string
	Parallel        int
}

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr)
		return 2
	}
	if cfg == nil {
		printUsage(os.Stdout)
		return 0
	}

	briefs, err := loadBriefs("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if cfg.LintDir != "" {
		return runLintDirectory(cfg.LintDir, cfg.OutFolder, briefs)
	}

	selected, err := selectBriefs(briefs, cfg.Brief)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "Brief tersedia: %s\n", strings.Join(availableBriefNames(briefs), ", "))
		return 2
	}
	if cfg.Parallel < 0 || cfg.Parallel > 5 {
		fmt.Fprintln(os.Stderr, "--parallel harus antara 1 dan 5")
		return 2
	}
	if cfg.Parallel > 0 && len(selected) != 1 {
		fmt.Fprintln(os.Stderr, "--parallel membutuhkan tepat satu --brief")
		return 2
	}

	cfgFull, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		return 1
	}
	aiCfg := cfgFull.AI
	aiCfg.Model = cfg.Model
	aiCfg.Timeout = 300 * time.Second
	if cfg.ThinkingLevel != "" {
		level := strings.ToUpper(cfg.ThinkingLevel)
		aiCfg.ThinkingLevel = &level
		aiCfg.ThinkingBudget = nil
	} else {
		aiCfg.ThinkingLevel = nil
		aiCfg.ThinkingBudget = nil
	}
	if cfg.MaxOutputTokens > 0 {
		aiCfg.MaxOutputTokens = int64(cfg.MaxOutputTokens)
	} else {
		aiCfg.MaxOutputTokens = 32768
	}

	p, err := provider.New(aiCfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create provider: %v\n", err)
		return 1
	}
	if p == nil {
		fmt.Fprintln(os.Stderr, "AI provider is not configured")
		return 1
	}

	fmt.Println("=== AI Website Generator Benchmark ===")
	fmt.Printf("Model: %s\n", cfg.Model)
	fmt.Printf("Thinking: %s\n", cfg.ThinkingLevel)
	fmt.Printf("MaxOutputTokens: %d\n", cfg.MaxOutputTokens)
	fmt.Printf("Runs: %d\n", cfg.Runs)
	fmt.Printf("OutFolder: %s\n", cfg.OutFolder)
	fmt.Printf("Lint: %v\n", cfg.Lint)
	fmt.Println()

	totalFailures := 0
	var lintResults []lintFileResult
	if cfg.Parallel > 0 {
		return runParallelMode(p, selected[0], *cfg, &lintResults)
	}

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

	for _, brief := range selected {
		fmt.Printf("--- Brief: %s ---\n", brief.Name)
		var firstChunkSum, firstTextSum, totalSum, tpsSum float64
		var validCount int

		for run := 1; run <= cfg.Runs; run++ {
			fmt.Printf("Brief: %s chars=%d hash=%s\n", brief.Name, len(brief.Text), briefDigest(brief.Text))
			opt := defaultRunOptions()
			opt.Model = cfg.Model
			opt.ThinkingLevel = cfg.ThinkingLevel
			opt.MaxOutputTokens = cfg.MaxOutputTokens
			opt.OutFolder = cfg.OutFolder
			opt.BriefName = brief.Name
			opt.RunNumber = run
			result := runBenchmark(p, brief, opt)
			if err := persistResult(&result, opt); err != nil {
				fmt.Fprintf(os.Stderr, "Gagal menyimpan hasil: %v\n", err)
				return 1
			}
			printRunLine(run, result)
			if result.DailyQuota {
				printDailyQuotaStop(cfg.Model, result.Quota)
				return 1
			}
			if cfg.Lint {
				if result.Status == "error" {
					fmt.Printf("Lint %s: dilewati (run gagal)\n", outputBase(opt.BriefName, opt.Model, opt.ThinkingLevel, run)+".html")
				} else {
					lintResult := lintFileResult{
						File:     filepath.Base(result.HTMLPath),
						Brief:    brief.Name,
						Findings: lintHTML(result.HTML, brief),
					}
					printLintResult(os.Stdout, lintResult)
					lintResults = append(lintResults, lintResult)
				}
			}

			firstChunkSum += float64(result.FirstChunkMs) / 1000.0
			firstTextSum += float64(result.FirstTextMs) / 1000.0
			totalSum += float64(result.Total) / 1000.0
			tpsSum += result.TokenPerSec
			if result.Status == "ok" {
				validCount++
			}
		}

		n := float64(cfg.Runs)
		summary := BriefSummary{
			Name:          brief.Name,
			FirstChunkAvg: firstChunkSum / n,
			FirstTextAvg:  firstTextSum / n,
			TotalAvg:      totalSum / n,
			TPSAvg:        tpsSum / n,
			ValidCount:    validCount,
			ValidPercent:  float64(validCount) / n * 100,
		}
		summaries = append(summaries, summary)
		fmt.Printf("AVERAGE: firstChunk=%.3fs, firstText=%.3fs, Total=%.3fs, Tok/s=%.1f, Valid%%=%.0f%% (%d/%d)\n\n",
			summary.FirstChunkAvg, summary.FirstTextAvg, summary.TotalAvg, summary.TPSAvg,
			summary.ValidPercent, validCount, cfg.Runs)
	}

	fmt.Println("=== SUMMARY TABLE ===")
	fmt.Printf("%-15s %-13s %-13s %-10s %-10s %-8s\n", "Brief", "FirstChunk(s)", "FirstText(s)", "Total(s)", "Tok/s", "Valid%")
	for _, s := range summaries {
		fmt.Printf("%-15s %-13.3f %-13.3f %-10.3f %-10.1f %-8.0f\n",
			s.Name, s.FirstChunkAvg, s.FirstTextAvg, s.TotalAvg, s.TPSAvg, s.ValidPercent)
	}

	if cfg.Lint {
		totalFailures = finishLint(cfg.OutFolder, lintResults)
	}
	if totalFailures > 0 {
		return 1
	}
	return 0
}

func runParallelMode(p provider.Provider, brief Brief, cfg BenchmarkConfig, lintResults *[]lintFileResult) int {
	fmt.Printf("WARNING: uji paralel ini memakai kuota API (%d request sekaligus).\n\n", cfg.Parallel)
	opt := defaultRunOptions()
	opt.Model = cfg.Model
	opt.ThinkingLevel = cfg.ThinkingLevel
	opt.MaxOutputTokens = cfg.MaxOutputTokens
	opt.OutFolder = cfg.OutFolder
	opt.BriefName = brief.Name

	fmt.Printf("Brief: %s chars=%d hash=%s\n", brief.Name, len(brief.Text), briefDigest(brief.Text))
	results := runParallel(p, brief, opt, cfg.Parallel)
	for _, result := range results {
		if result.DailyQuota {
			printDailyQuotaStop(cfg.Model, result.Quota)
			return 1
		}
	}
	failures := 0
	for i, result := range results {
		runOpt := opt
		runOpt.RunNumber = i + 1
		if err := persistResult(&result, runOpt); err != nil {
			fmt.Fprintf(os.Stderr, "Gagal menyimpan hasil request %d: %v\n", i+1, err)
			return 1
		}
		fmt.Printf("Request %d: firstChunk=%.3fs total=%.3fs status=%s attempts=%d errors429=%d errors503=%d\n",
			i+1, float64(result.FirstChunkMs)/1000.0, float64(result.Total)/1000.0,
			result.Status, result.AttemptCount, result.Error429, result.Error503)
		if result.Error != "" {
			fmt.Printf("  ERROR: %s\n", result.Error)
		}
		if cfg.Lint {
			if result.Status == "error" {
				fmt.Printf("Lint %s: dilewati (run gagal)\n", outputBase(opt.BriefName, opt.Model, opt.ThinkingLevel, i+1)+".html")
			} else {
				lintResult := lintFileResult{
					File:     filepath.Base(result.HTMLPath),
					Brief:    brief.Name,
					Findings: lintHTML(result.HTML, brief),
				}
				printLintResult(os.Stdout, lintResult)
				*lintResults = append(*lintResults, lintResult)
			}
		}
	}
	if cfg.Lint {
		failures = finishLint(cfg.OutFolder, *lintResults)
	}
	if failures > 0 {
		return 1
	}
	return 0
}

func printDailyQuotaStop(requestedModel string, info provider.QuotaInfo) {
	bodyModel := info.QuotaModel
	if bodyModel == "" {
		bodyModel = info.RequestedModel
	}
	retryDelay := "tidak disebut body 429"
	if info.RetryDelay > 0 {
		retryDelay = info.RetryDelay.String()
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "KUOTA HARIAN HABIS: run dihentikan, tidak ada retry.")
	fmt.Fprintf(os.Stderr, "Model --model: %s\n", requestedModel)
	if bodyModel != "" && bodyModel != requestedModel {
		fmt.Fprintf(os.Stderr, "Model pada body 429: %s\n", bodyModel)
	}
	quotaID := info.QuotaID
	if quotaID == "" {
		quotaID = "tidak disebut body 429"
	}
	fmt.Fprintf(os.Stderr, "quotaId: %s\n", quotaID)
	fmt.Fprintf(os.Stderr, "retryDelay: %s\n", retryDelay)
	fmt.Fprintf(os.Stderr, "Perkiraan reset: %s\n", estimateQuotaReset(info, time.Now()))
}

func estimateQuotaReset(info provider.QuotaInfo, now time.Time) string {
	if info.RetryDelay > 0 {
		return now.Add(info.RetryDelay).Format(time.RFC3339)
	}
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return nextMidnight.Format(time.RFC3339) + " (perkiraan tengah malam lokal)"
}

func printRunLine(run int, result BenchmarkResult) {
	fmt.Printf("Run %d: firstChunk=%.3fs, firstText=%.3fs, Total=%.3fs, PromptTok=%d, CandTok=%d, ThinkTok=%d, Tok/s=%.1f, FinishReason=%s, Valid=%v, Attempts=%d, Status=%s, 429=%d, 503=%d\n",
		run, float64(result.FirstChunkMs)/1000.0, float64(result.FirstTextMs)/1000.0,
		float64(result.Total)/1000.0, result.PromptTokens, result.CandidatesTokens,
		result.ThinkingTokenCount, result.TokenPerSec, result.FinishReason, result.ValidHTML,
		result.AttemptCount, result.Status, result.Error429, result.Error503)
	if result.Error != "" {
		fmt.Printf("  ERROR: %s\n", result.Error)
	}
}

func finishLint(outFolder string, results []lintFileResult) int {
	path, err := writeLintReport(outFolder, results)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Gagal menulis lint report: %v\n", err)
		return 1
	}
	fmt.Printf("\nLint report: %s\n", path)
	failures := 0
	for _, result := range results {
		if result.hasFailure() {
			failures++
		}
	}
	if failures > 0 {
		fmt.Printf("LINT FAILED: %d file memiliki pelanggaran.\n", failures)
		return failures
	}
	fmt.Println("LINT PASSED: tidak ada pelanggaran FAIL.")
	return 0
}

func runLintDirectory(dir, outFolder string, briefs []Brief) int {
	results, hasFailure, err := lintDirectory(dir, briefs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lint directory gagal: %v\n", err)
		return 1
	}
	for _, result := range results {
		printLintResult(os.Stdout, result)
	}
	path, err := writeLintReport(outFolder, results)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Gagal menulis lint report: %v\n", err)
		return 1
	}
	fmt.Printf("\nLint report: %s\n", path)
	if hasFailure {
		fmt.Println("LINT FAILED")
		return 1
	}
	fmt.Println("LINT PASSED")
	return 0
}

func parseArgs(args []string) (*BenchmarkConfig, error) {
	cfg := BenchmarkConfig{
		Model:     "gemini-3.8-flash",
		Runs:      3,
		OutFolder: "./aibench-results",
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--model membutuhkan nilai")
			}
			cfg.Model = args[i+1]
			i++
		case "--thinking":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--thinking membutuhkan nilai")
			}
			cfg.ThinkingLevel = args[i+1]
			i++
		case "--max-output":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--max-output membutuhkan nilai")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("--max-output harus bilangan positif")
			}
			cfg.MaxOutputTokens = value
			i++
		case "--brief":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--brief membutuhkan nilai")
			}
			cfg.Brief = args[i+1]
			i++
		case "--runs":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--runs membutuhkan nilai")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("--runs harus bilangan positif")
			}
			cfg.Runs = value
			i++
		case "--out":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--out membutuhkan nilai")
			}
			cfg.OutFolder = args[i+1]
			i++
		case "--lint":
			cfg.Lint = true
		case "--lint-dir":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--lint-dir membutuhkan nilai")
			}
			cfg.LintDir = args[i+1]
			i++
		case "--parallel":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--parallel membutuhkan nilai")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value < 1 || value > 5 {
				return nil, fmt.Errorf("--parallel harus antara 1 dan 5")
			}
			cfg.Parallel = value
			i++
		case "--help", "-h":
			return nil, nil
		default:
			return nil, fmt.Errorf("flag tidak dikenal: %s", args[i])
		}
	}
	return &cfg, nil
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "Usage: go run ./cmd/aibench [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --brief NAME        full|service|wa_only|minimal|nopalette (kosong = semua)")
	fmt.Fprintln(w, "  --model NAME        model provider (default gemini-3.8-flash)")
	fmt.Fprintln(w, "  --thinking LEVEL    MINIMAL|LOW|MEDIUM|HIGH (kosong = default)")
	fmt.Fprintln(w, "  --max-output N      max output tokens (default 32768)")
	fmt.Fprintln(w, "  --runs N            jumlah run per brief (default 3)")
	fmt.Fprintln(w, "  --out PATH          folder hasil (default ./aibench-results)")
	fmt.Fprintln(w, "  --lint              lint setiap HTML hasil dan tulis lint_report.md")
	fmt.Fprintln(w, "  --lint-dir PATH     lint semua HTML di folder tanpa memanggil API")
	fmt.Fprintln(w, "  --parallel N        N request paralel untuk satu brief (maks 5)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Brief tersedia: "+strings.Join(requiredBriefs, ", "))
}
