package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/modules/execution/model"
)

type dummyExecutionService struct {
	execResult *model.ExecuteResponse
	execErr    error
	getDetail  *model.CodeExecution
	getErr     error
	listResult []model.CodeExecution
	listErr    error
}

func (d *dummyExecutionService) Execute(_ context.Context, _ string, _ model.ExecuteRequest) (*model.ExecuteResponse, error) {
	return d.execResult, d.execErr
}

func (d *dummyExecutionService) GetExecution(_ context.Context, _, _ string) (*model.CodeExecution, error) {
	return d.getDetail, d.getErr
}

func (d *dummyExecutionService) ListExecutions(_ context.Context, _ string, _, _ int) ([]model.CodeExecution, error) {
	return d.listResult, d.listErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestExecute_Unauthorized(t *testing.T) {
	svc := &dummyExecutionService{}
	h := New(svc, testLogger(), config.ExecutionConfig{MaxCodeBytes: 65536})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(1)"}`))
	rec := httptest.NewRecorder()

	h.Execute(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestExecute_Success(t *testing.T) {
	svc := &dummyExecutionService{
		execResult: &model.ExecuteResponse{
			ID:              "exec-123",
			Language:        "python",
			Stdout:          "Hello\n",
			Stderr:          "",
			ExitCode:        0,
			ExecutionTimeMs: 30,
			MemoryKB:        2048,
			Status:          "Accepted",
		},
	}
	h := New(svc, testLogger(), config.ExecutionConfig{MaxCodeBytes: 65536})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print('Hello')"}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-abc"))

	rec := httptest.NewRecorder()
	h.Execute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp struct {
		Success bool                  `json:"success"`
		Data    model.ExecuteResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success = true")
	}
	if resp.Data.Stdout != "Hello\n" {
		t.Errorf("expected stdout 'Hello\n', got %q", resp.Data.Stdout)
	}
}

func TestExecute_MalformedJSON(t *testing.T) {
	svc := &dummyExecutionService{}
	h := New(svc, testLogger(), config.ExecutionConfig{MaxCodeBytes: 65536})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{invalid json`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-abc"))

	rec := httptest.NewRecorder()
	h.Execute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", rec.Code)
	}
}

func TestExecute_CodeExceedsSizeLimit_HandlerLevel(t *testing.T) {
	svc := &dummyExecutionService{}
	// Limit to 50 bytes
	h := New(svc, testLogger(), config.ExecutionConfig{MaxCodeBytes: 50})

	oversizedPayload := model.ExecuteRequest{
		Language: "python",
		Code:     strings.Repeat("a = 1\n", 20),
	}
	b, _ := json.Marshal(oversizedPayload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader(b))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-abc"))

	rec := httptest.NewRecorder()
	h.Execute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for oversized code, got %d", rec.Code)
	}
}

func TestListHistory_Success(t *testing.T) {
	svc := &dummyExecutionService{
		listResult: []model.CodeExecution{
			{
				ID:        "exec-1",
				UserID:    "user-abc",
				Language:  "go",
				Code:      "package main",
				Stdout:    "ok",
				Status:    "Accepted",
				CreatedAt: time.Now(),
			},
		},
	}
	h := New(svc, testLogger(), config.ExecutionConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/execute/history?limit=10&offset=0", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-abc"))

	rec := httptest.NewRecorder()
	h.ListHistory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}
