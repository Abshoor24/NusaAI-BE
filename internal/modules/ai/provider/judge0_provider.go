package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
)

// JudgeZeroProvider communicates with the Judge0 CE REST API to execute code sandboxed.
type JudgeZeroProvider struct {
	baseURL          string
	apiKey           string
	cpuTimeLimitSec  float64
	wallTimeLimitSec float64
	memoryLimitKB    int64
	pollInterval     time.Duration
	maxPollDuration  time.Duration
	client           *http.Client
	logger           *slog.Logger
}

// NewJudgeZeroProvider creates a new JudgeZeroProvider instance with validated configuration.
func NewJudgeZeroProvider(cfg config.ExecutionConfig, logger *slog.Logger) *JudgeZeroProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:2358"
	}
	cpuLimit := cfg.CPUTimeLimitSec
	if cpuLimit <= 0 {
		cpuLimit = 5.0
	}
	wallLimit := cfg.WallTimeLimitSec
	if wallLimit <= 0 {
		wallLimit = 10.0
	}
	memLimit := cfg.MemoryLimitKB
	if memLimit <= 0 {
		memLimit = 131072 // 128 MB in KB
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	maxPoll := cfg.MaxPollDuration
	if maxPoll <= 0 {
		maxPoll = 15 * time.Second
	}

	return &JudgeZeroProvider{
		baseURL:          baseURL,
		apiKey:           cfg.APIKey,
		cpuTimeLimitSec:  cpuLimit,
		wallTimeLimitSec: wallLimit,
		memoryLimitKB:    memLimit,
		pollInterval:     pollInterval,
		maxPollDuration:  maxPoll,
		client:           &http.Client{Timeout: 10 * time.Second},
		logger:           logger,
	}
}

type judge0SubmissionResponse struct {
	Token         string  `json:"token"`
	Stdout        *string `json:"stdout"`
	Stderr        *string `json:"stderr"`
	CompileOutput *string `json:"compile_output"`
	Message       *string `json:"message"`
	ExitCode      *int    `json:"exit_code"`
	Status        struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
	} `json:"status"`
	Time   interface{} `json:"time"`
	Memory interface{} `json:"memory"`
}

// Execute submits code to Judge0, polls for completion, and extracts stdout, stderr, metrics, and exit code.
func (p *JudgeZeroProvider) Execute(ctx context.Context, req ExecutionRequest) (ExecutionResult, error) {
	canonicalLang, langID, ok := NormalizeLanguage(req.Language)
	if !ok {
		return ExecutionResult{}, apperror.Validation(fmt.Sprintf("Language '%s' is not supported. Supported languages are: python, javascript, go", req.Language))
	}

	// 1. Prepare submission payload
	payload := map[string]interface{}{
		"source_code":     req.Code,
		"language_id":     langID,
		"stdin":           req.Stdin,
		"cpu_time_limit":  p.cpuTimeLimitSec,
		"wall_time_limit": p.wallTimeLimitSec,
		"memory_limit":    p.memoryLimitKB,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ExecutionResult{}, apperror.ExecutionProvider("Failed to marshal submission payload", err)
	}

	// 2. Submit execution request to Judge0
	submitURL := fmt.Sprintf("%s/submissions?base64_encoded=false&wait=false", p.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(body))
	if err != nil {
		return ExecutionResult{}, apperror.ExecutionProvider("Failed to create execution request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("X-Auth-Token", p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ExecutionResult{}, apperror.ExecutionProvider("Code execution request was cancelled", ctx.Err())
		}
		p.logger.Error("judge0 submission request failed", "error", err)
		return ExecutionResult{}, apperror.ExecutionProvider("The code execution provider could not be reached", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		p.logger.Error("judge0 returned error on submission", "status", resp.StatusCode, "body", string(errBody))
		return ExecutionResult{}, apperror.ExecutionProvider("The code execution provider returned an error", fmt.Errorf("status %d", resp.StatusCode))
	}

	var submitResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&submitResp); err != nil {
		p.logger.Error("judge0 submission decode failed", "error", err)
		return ExecutionResult{}, apperror.ExecutionProvider("Failed to read execution submission response", err)
	}
	if submitResp.Token == "" {
		return ExecutionResult{}, apperror.ExecutionProvider("Judge0 did not return an execution token", nil)
	}

	token := submitResp.Token

	// 3. Poll Judge0 until submission finishes or timeout expires
	pollCtx, cancel := context.WithTimeout(ctx, p.maxPollDuration)
	defer cancel()

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	pollURL := fmt.Sprintf("%s/submissions/%s?base64_encoded=false", p.baseURL, token)

	for {
		select {
		case <-pollCtx.Done():
			if ctx.Err() != nil {
				return ExecutionResult{}, apperror.ExecutionProvider("Code execution request was cancelled", ctx.Err())
			}
			return ExecutionResult{}, apperror.ExecutionProvider("Code execution timed out waiting for results", pollCtx.Err())

		case <-ticker.C:
			pollReq, err := http.NewRequestWithContext(pollCtx, http.MethodGet, pollURL, nil)
			if err != nil {
				return ExecutionResult{}, apperror.ExecutionProvider("Failed to create poll request", err)
			}
			if p.apiKey != "" {
				pollReq.Header.Set("X-Auth-Token", p.apiKey)
			}

			pollResp, err := p.client.Do(pollReq)
			if err != nil {
				if pollCtx.Err() != nil {
					continue // loop will handle pollCtx.Done()
				}
				p.logger.Warn("judge0 poll request error, will retry", "token", token, "error", err)
				continue
			}

			if pollResp.StatusCode != http.StatusOK {
				pollResp.Body.Close()
				p.logger.Warn("judge0 poll non-200 status, will retry", "token", token, "status", pollResp.StatusCode)
				continue
			}

			var subResp judge0SubmissionResponse
			decErr := json.NewDecoder(pollResp.Body).Decode(&subResp)
			pollResp.Body.Close()

			if decErr != nil {
				p.logger.Warn("judge0 poll response decode error, will retry", "token", token, "error", decErr)
				continue
			}

			// Status ID 1 (In Queue) and 2 (Processing) mean still executing.
			if subResp.Status.ID <= 2 {
				continue
			}

			// Execution finished (Status ID >= 3)
			stdout := ""
			if subResp.Stdout != nil {
				stdout = *subResp.Stdout
			}

			var stderrParts []string
			if subResp.Stderr != nil && *subResp.Stderr != "" {
				stderrParts = append(stderrParts, *subResp.Stderr)
			}
			if subResp.CompileOutput != nil && *subResp.CompileOutput != "" {
				stderrParts = append(stderrParts, *subResp.CompileOutput)
			}
			if subResp.Message != nil && *subResp.Message != "" {
				// Avoid repeating message if already present in stderr
				msg := *subResp.Message
				if !strings.Contains(strings.Join(stderrParts, "\n"), msg) {
					stderrParts = append(stderrParts, msg)
				}
			}
			stderr := strings.Join(stderrParts, "\n")

			exitCode := 0
			if subResp.ExitCode != nil {
				exitCode = *subResp.ExitCode
			} else if subResp.Status.ID == 5 {
				// Time limit exceeded timeout default exit code
				exitCode = 124
			} else if subResp.Status.ID != 3 {
				// Non-zero status ID (errors, runtime crash)
				exitCode = 1
			}

			var execTimeMs int64
			if subResp.Time != nil {
				switch v := subResp.Time.(type) {
				case string:
					if sec, err := strconv.ParseFloat(v, 64); err == nil {
						execTimeMs = int64(sec * 1000.0)
					}
				case float64:
					execTimeMs = int64(v * 1000.0)
				}
			}

			var memoryKB int64
			if subResp.Memory != nil {
				switch v := subResp.Memory.(type) {
				case float64:
					memoryKB = int64(v)
				case string:
					if kb, err := strconv.ParseInt(v, 10, 64); err == nil {
						memoryKB = kb
					}
				}
			}

			statusDescription := subResp.Status.Description
			if statusDescription == "" {
				if subResp.Status.ID == 3 {
					statusDescription = "Accepted"
				} else {
					statusDescription = "Completed"
				}
			}

			_ = canonicalLang

			return ExecutionResult{
				Stdout:          stdout,
				Stderr:          stderr,
				ExitCode:        exitCode,
				ExecutionTimeMs: execTimeMs,
				MemoryKB:        memoryKB,
				Status:          statusDescription,
			}, nil
		}
	}
}
