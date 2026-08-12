// Package storage holds Supabase-specific integrations. Today that means the
// PostgreSQL connection; future Supabase features (Auth, Storage buckets)
// belong here too, keeping internal/ generic.
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/database"
)

const dbWaitTimeout = 30 * time.Second

// Supabase is the project's data integration point. It owns the PostgreSQL
// connection pool (from DATABASE_URL or individual fields) and exposes it to
// repositories.
type Supabase struct {
	db *database.Postgres
}

// New connects to Supabase, waits for it to be reachable, opens the pool and
// applies embedded migrations. It fails fast so the server never starts with
// an unreachable database.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Supabase, error) {
	if err := database.WaitForDB(ctx, cfg.DB, dbWaitTimeout); err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	db, err := database.New(ctx, cfg.DB)
	if err != nil {
		return nil, err
	}

	if err := database.RunMigrations(cfg.DB); err != nil {
		db.Close()
		return nil, err
	}

	log.Info("database connected", "host", cfg.DB.HostLabel())
	return &Supabase{db: db}, nil
}

// Ping verifies the connection pool is healthy.
func (s *Supabase) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// Pool exposes the underlying pgx pool for repositories.
func (s *Supabase) Pool() *pgxpool.Pool {
	return s.db.Pool()
}

// Close gracefully closes all pooled connections.
func (s *Supabase) Close() {
	s.db.Close()
}
