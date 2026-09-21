package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/execution/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type mockExecutionProvider struct {
	result  provider.ExecutionResult
	err     error
	lastReq provider.ExecutionRequest
}

func (m *mockExecutionProvider) Execute(_ context.Context, req provider.ExecutionRequest) (provider.ExecutionResult, error) {
	m.lastReq = req
	if m.err != nil {
		return provider.ExecutionResult{}, m.err
	}
	return m.result, nil
}

type mockRepository struct {
	created    []*model.CodeExecution
	createErr  error
	getByID    *model.CodeExecution
	getByIDErr error
	list       []model.CodeExecution
	listErr    error
}

func (m *mockRepository) CreateExecution(_ context.Context, exec *model.CodeExecution) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = append(m.created, exec)
	return nil
}

func (m *mockRepository) GetExecutionByID(_ context.Context, _, _ string) (*model.CodeExecution, error) {
	return m.getByID, m.getByIDErr
}

func (m *mockRepository) ListExecutions(_ context.Context, _ string, _, _ int) ([]model.CodeExecution, error) {
	return m.list, m.listErr
}

func TestExecute_Success_WhitelistedLanguages(t *testing.T) {
	languages := []string{"python", "javascript", "go"}

	for _, lang := range languages {
		t.Run(lang, func(t *testing.T) {
			mockProv := &mockExecutionProvider{
				result: provider.ExecutionResult{
					Stdout:          "output from " + lang,
					Stderr:          "",
					ExitCode:        0,
					ExecutionTimeMs: 42,
					MemoryKB:        3100,
					Status:          "Accepted",
				},
			}
			mockRepo := &mockRepository{}

			cfg := config.ExecutionConfig{
				MaxCodeBytes: 65536,
			}

			svc := New(mockProv, cfg, testLogger(), mockRepo)
			res, err := svc.Execute(context.Background(), "user-123", model.ExecuteRequest{
				Language: lang,
				Code:     "print('test')",
			})
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", lang, err)
			}

			if res.Language != lang {
				t.Errorf("expected canonical language %s, got %s", lang, res.Language)
			}
			if res.Stdout != "output from "+lang {
				t.Errorf("unexpected stdout: %s", res.Stdout)
			}
			if res.ExitCode != 0 {
				t.Errorf("expected exit code 0, got %d", res.ExitCode)
			}
			if len(mockRepo.created) != 1 {
				t.Fatalf("expected 1 execution record to be saved, got %d", len(mockRepo.created))
			}
			if mockRepo.created[0].UserID != "user-123" {
				t.Errorf("expected saved UserID user-123, got %s", mockRepo.created[0].UserID)
			}
		})
	}
}

func TestExecute_Rejected_NonWhitelistedLanguage(t *testing.T) {
	mockProv := &mockExecutionProvider{}
	mockRepo := &mockRepository{}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	_, err := svc.Execute(context.Background(), "user-1", model.ExecuteRequest{
		Language: "ruby",
		Code:     "puts 'hello'",
	})
	if err == nil {
		t.Fatal("expected error for non-whitelisted language, got nil")
	}
	if !strings.Contains(err.Error(), "ruby") {
		t.Errorf("expected error message to mention 'ruby', got: %v", err)
	}
}

func TestExecute_Rejected_EmptyCode(t *testing.T) {
	mockProv := &mockExecutionProvider{}
	mockRepo := &mockRepository{}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	_, err := svc.Execute(context.Background(), "user-1", model.ExecuteRequest{
		Language: "python",
		Code:     "   ",
	})
	if err == nil {
		t.Fatal("expected error for empty code, got nil")
	}
	if !strings.Contains(err.Error(), "Code is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_Rejected_CodeExceedsSizeLimit(t *testing.T) {
	mockProv := &mockExecutionProvider{}
	mockRepo := &mockRepository{}
	// Set small limit: 50 bytes
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 50}, testLogger(), mockRepo)

	largeCode := strings.Repeat("x := 1\n", 20)
	_, err := svc.Execute(context.Background(), "user-1", model.ExecuteRequest{
		Language: "go",
		Code:     largeCode,
	})
	if err == nil {
		t.Fatal("expected error for oversized code, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum limit") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_InfiniteLoopTimeoutHandling(t *testing.T) {
	mockProv := &mockExecutionProvider{
		result: provider.ExecutionResult{
			Stdout:          "",
			Stderr:          "Time Limit Exceeded",
			ExitCode:        124,
			ExecutionTimeMs: 10000,
			MemoryKB:        15000,
			Status:          "Time Limit Exceeded",
		},
	}
	mockRepo := &mockRepository{}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	res, err := svc.Execute(context.Background(), "user-1", model.ExecuteRequest{
		Language: "python",
		Code:     "while True: pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != "Time Limit Exceeded" {
		t.Errorf("expected status 'Time Limit Exceeded', got %s", res.Status)
	}
	if res.ExitCode != 124 {
		t.Errorf("expected exit code 124, got %d", res.ExitCode)
	}
	if res.Stderr != "Time Limit Exceeded" {
		t.Errorf("expected stderr 'Time Limit Exceeded', got %s", res.Stderr)
	}
}

func TestExecute_Unauthorized_MissingUserID(t *testing.T) {
	mockProv := &mockExecutionProvider{}
	mockRepo := &mockRepository{}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	_, err := svc.Execute(context.Background(), "", model.ExecuteRequest{
		Language: "python",
		Code:     "print('hi')",
	})
	if err == nil {
		t.Fatal("expected unauthorized error, got nil")
	}
}

func TestExecute_RepositoryError(t *testing.T) {
	mockProv := &mockExecutionProvider{
		result: provider.ExecutionResult{
			Stdout: "ok",
			Status: "Accepted",
		},
	}
	mockRepo := &mockRepository{
		createErr: errors.New("db connection failure"),
	}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	_, err := svc.Execute(context.Background(), "user-1", model.ExecuteRequest{
		Language: "python",
		Code:     "print('hi')",
	})
	if err == nil {
		t.Fatal("expected db error, got nil")
	}
}

func TestGetExecution_And_ListExecutions(t *testing.T) {
	mockProv := &mockExecutionProvider{}
	sample := &model.CodeExecution{
		ID:        "exec-1",
		UserID:    "user-1",
		Language:  "python",
		Code:      "print(1)",
		Stdout:    "1\n",
		Status:    "Accepted",
		CreatedAt: time.Now(),
	}
	mockRepo := &mockRepository{
		getByID: sample,
		list:    []model.CodeExecution{*sample},
	}
	svc := New(mockProv, config.ExecutionConfig{MaxCodeBytes: 65536}, testLogger(), mockRepo)

	got, err := svc.GetExecution(context.Background(), "exec-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "exec-1" {
		t.Errorf("expected ID exec-1, got %s", got.ID)
	}

	list, err := svc.ListExecutions(context.Background(), "user-1", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 item, got %d", len(list))
	}
}
