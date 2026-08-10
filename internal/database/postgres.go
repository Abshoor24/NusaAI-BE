package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/USERNAME/PROJECT_NAME/internal/config"
)

// Postgres manages the connection pool. It is created once at startup and
// closed gracefully during shutdown.
type Postgres struct {
	pool *pgxpool.Pool
}

// DSN builds the PostgreSQL connection string from config. A full DATABASE_URL
// (e.g. Supabase) is passed through untouched; otherwise a libpq keyword/value
// string is assembled from the individual fields.
func DSN(cfg config.DBConfig) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
	)
}

// New creates the connection pool, configures pooling limits and pings the
// database to fail fast when it is unreachable.
func New(ctx context.Context, cfg config.DBConfig) (*Postgres, error) {
	poolCfg, err := pgxpool.ParseConfig(DSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnLifetime / 2
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping failed: %w", err)
	}

	return &Postgres{pool: pool}, nil
}

// Ping verifies the connection pool is healthy.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Pool exposes the underlying pgxpool for repositories.
func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

// Close gracefully closes all pooled connections.
func (p *Postgres) Close() {
	p.pool.Close()
}

// WaitForDB polls the database until it is reachable or the timeout expires.
// Used at startup so the app can tolerate the DB container still booting.
func WaitForDB(ctx context.Context, cfg config.DBConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		poolCfg, err := pgxpool.ParseConfig(DSN(cfg))
		if err != nil {
			return fmt.Errorf("database: parse config: %w", err)
		}
		poolCfg.ConnConfig.ConnectTimeout = 2 * time.Second

		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			pool.Close()
			if err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database: not reachable after %s: %w", timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
