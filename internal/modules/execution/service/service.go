package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/execution/model"
	"github.com/Abshoor24/backend-go/internal/modules/execution/repository"
)

// Service defines the business logic operations for code execution.
type Service interface {
	Execute(ctx context.Context, userID string, req model.ExecuteRequest) (*model.ExecuteResponse, error)
	GetExecution(ctx context.Context, id, userID string) (*model.CodeExecution, error)
	ListExecutions(ctx context.Context, userID string, limit, offset int) ([]model.CodeExecution, error)
}

type service struct {
	provider provider.ExecutionProvider
	cfg      config.ExecutionConfig
	logger   *slog.Logger
	repo     repository.Repository
}

// New creates a new instance of the execution Service.
func New(execProvider provider.ExecutionProvider, cfg config.ExecutionConfig, logger *slog.Logger, repo repository.Repository) Service {
	if cfg.MaxCodeBytes <= 0 {
		cfg.MaxCodeBytes = 65536 // 64 KB default
	}

	return &service{
		provider: execProvider,
		cfg:      cfg,
		logger:   logger,
		repo:     repo,
	}
}

// Execute validates language and payload size, runs code via the ExecutionProvider,
// saves execution history to the database, and returns the execution results.
func (s *service) Execute(ctx context.Context, userID string, req model.ExecuteRequest) (*model.ExecuteResponse, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}

	code := req.Code
	if strings.TrimSpace(code) == "" {
		return nil, apperror.Validation("Code is required and cannot be empty")
	}

	maxBytes := s.cfg.MaxCodeBytes
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	if int64(len([]byte(code))) > maxBytes {
		return nil, apperror.Validation(fmt.Sprintf("Code size exceeds maximum limit of %d KB", maxBytes/1024))
	}

	canonicalLang, _, ok := provider.NormalizeLanguage(req.Language)
	if !ok {
		return nil, apperror.Validation(fmt.Sprintf("Language '%s' is not supported. Supported languages are: python, javascript, go", req.Language))
	}

	// Submit to execution sandbox provider (Judge0)
	execRes, err := s.provider.Execute(ctx, provider.ExecutionRequest{
		Language: canonicalLang,
		Code:     code,
		Stdin:    req.Stdin,
	})
	if err != nil {
		s.logger.Error("code execution provider failed", "user_id", userID, "language", canonicalLang, "error", err)
		return nil, err
	}

	// Persist execution history with user ownership
	execID := uuid.New().String()
	execRecord := &model.CodeExecution{
		ID:              execID,
		UserID:          userID,
		Language:        canonicalLang,
		Code:            code,
		Stdin:           req.Stdin,
		Stdout:          execRes.Stdout,
		Stderr:          execRes.Stderr,
		ExitCode:        execRes.ExitCode,
		ExecutionTimeMs: execRes.ExecutionTimeMs,
		MemoryKB:        execRes.MemoryKB,
		Status:          execRes.Status,
		CreatedAt:       time.Now(),
	}

	if err := s.repo.CreateExecution(ctx, execRecord); err != nil {
		s.logger.Error("failed to persist code execution record", "id", execID, "user_id", userID, "error", err)
		return nil, err
	}

	return &model.ExecuteResponse{
		ID:              execRecord.ID,
		Language:        canonicalLang,
		Stdout:          execRes.Stdout,
		Stderr:          execRes.Stderr,
		ExitCode:        execRes.ExitCode,
		ExecutionTimeMs: execRes.ExecutionTimeMs,
		MemoryKB:        execRes.MemoryKB,
		Status:          execRes.Status,
	}, nil
}

func (s *service) GetExecution(ctx context.Context, id, userID string) (*model.CodeExecution, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	if strings.TrimSpace(id) == "" {
		return nil, apperror.Validation("Execution ID is required")
	}

	exec, err := s.repo.GetExecutionByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if exec == nil {
		return nil, apperror.NotFound("Code execution not found", nil)
	}
	return exec, nil
}

func (s *service) ListExecutions(ctx context.Context, userID string, limit, offset int) ([]model.CodeExecution, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	return s.repo.ListExecutions(ctx, userID, limit, offset)
}
