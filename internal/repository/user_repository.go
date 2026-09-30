package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/model"
)

// UserRepository is the persistence contract for the auth domain. It is the
// only layer allowed to touch the users table.
//
// TODO: CLIENT REQUIREMENT - add repository interfaces only for domains the
// client actually needs (never pre-create product/order/... repositories).
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
}

// PostgresUserRepository implements UserRepository on PostgreSQL using
// parameterized queries (never string concatenation).
type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) UserRepository {
	return &PostgresUserRepository{pool: pool}
}

func (r *PostgresUserRepository) Create(ctx context.Context, user *model.User) error {
	const q = `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())`

	_, err := r.pool.Exec(ctx, q, user.ID, user.Email, user.PasswordHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return apperror.Conflict("An account with this email already exists", err)
		}
		return apperror.Database("Failed to create user", err)
	}
	return nil
}

func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	const q = `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users WHERE email = $1`

	var u model.User
	err := r.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Database("Failed to look up user", err)
	}
	return &u, nil
}

func (r *PostgresUserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	const q = `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users WHERE id = $1`

	var u model.User
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Database("Failed to look up user", err)
	}
	return &u, nil
}
