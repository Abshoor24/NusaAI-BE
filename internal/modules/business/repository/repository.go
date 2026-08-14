package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// BusinessRepository is the persistence contract for this module. It is the
// only layer allowed to touch this domain's tables.
//
// TODO: CLIENT REQUIREMENT - define the interface methods only after the
// client's business entities are known, e.g.:
//
//	FindByID(ctx context.Context, id string) (*model.Item, error)
//	List(ctx context.Context, limit, offset int) ([]model.Item, error)
type BusinessRepository interface{}

// PostgresBusinessRepository implements BusinessRepository on PostgreSQL using
// parameterized queries (never string concatenation).
//
// TODO: CLIENT REQUIREMENT - implement methods once the table schema exists.
// Follow the error-mapping pattern in internal/repository/user_repository.go
// (map pgx.ErrNoRows and pgconn unique_violation to apperror types).
type PostgresBusinessRepository struct {
	pool *pgxpool.Pool
}

func NewBusinessRepository(pool *pgxpool.Pool) BusinessRepository {
	return &PostgresBusinessRepository{pool: pool}
}