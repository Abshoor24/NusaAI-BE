package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/modules/execution/model"
)

// Repository defines data access operations for code executions.
type Repository interface {
	CreateExecution(ctx context.Context, exec *model.CodeExecution) error
	GetExecutionByID(ctx context.Context, id, userID string) (*model.CodeExecution, error)
	ListExecutions(ctx context.Context, userID string, limit, offset int) ([]model.CodeExecution, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL repository for code executions.
func New(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) CreateExecution(ctx context.Context, exec *model.CodeExecution) error {
	const q = `
		INSERT INTO code_executions (
			id, user_id, language, code, stdin, stdout, stderr,
			exit_code, execution_time_ms, memory_kb, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.pool.Exec(ctx, q,
		exec.ID,
		exec.UserID,
		exec.Language,
		exec.Code,
		exec.Stdin,
		exec.Stdout,
		exec.Stderr,
		exec.ExitCode,
		exec.ExecutionTimeMs,
		exec.MemoryKB,
		exec.Status,
		exec.CreatedAt,
	)
	if err != nil {
		return apperror.Database("Failed to save code execution record", err)
	}
	return nil
}

func (r *postgresRepository) GetExecutionByID(ctx context.Context, id, userID string) (*model.CodeExecution, error) {
	const q = `
		SELECT id, user_id, language, code, COALESCE(stdin, ''), stdout, stderr,
		       exit_code, execution_time_ms, memory_kb, status, created_at
		FROM code_executions
		WHERE id = $1 AND user_id = $2`

	var exec model.CodeExecution
	err := r.pool.QueryRow(ctx, q, id, userID).Scan(
		&exec.ID,
		&exec.UserID,
		&exec.Language,
		&exec.Code,
		&exec.Stdin,
		&exec.Stdout,
		&exec.Stderr,
		&exec.ExitCode,
		&exec.ExecutionTimeMs,
		&exec.MemoryKB,
		&exec.Status,
		&exec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Database("Failed to get code execution record", err)
	}
	return &exec, nil
}

func (r *postgresRepository) ListExecutions(ctx context.Context, userID string, limit, offset int) ([]model.CodeExecution, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	const q = `
		SELECT id, user_id, language, code, COALESCE(stdin, ''), stdout, stderr,
		       exit_code, execution_time_ms, memory_kb, status, created_at
		FROM code_executions
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, apperror.Database("Failed to list code executions", err)
	}
	defer rows.Close()

	executions := make([]model.CodeExecution, 0)
	for rows.Next() {
		var exec model.CodeExecution
		if err := rows.Scan(
			&exec.ID,
			&exec.UserID,
			&exec.Language,
			&exec.Code,
			&exec.Stdin,
			&exec.Stdout,
			&exec.Stderr,
			&exec.ExitCode,
			&exec.ExecutionTimeMs,
			&exec.MemoryKB,
			&exec.Status,
			&exec.CreatedAt,
		); err != nil {
			return nil, apperror.Database("Failed to scan code execution", err)
		}
		executions = append(executions, exec)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed iterating code executions", err)
	}

	return executions, nil
}
