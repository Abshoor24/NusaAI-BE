package database

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	// Register the "pgx5" database driver for golang-migrate (import for side effects).
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/migrations"
)

// migrationDSN builds a connection URL consumable by golang-migrate using the
// pgx/v5 database driver. A full DATABASE_URL is reused (only its scheme is
// rewritten to the driver scheme), otherwise the individual connection fields
// are assembled and URL-escaped to avoid breaking on special characters in
// passwords.
func migrationDSN(cfg config.DBConfig) (string, error) {
	if cfg.URL != "" {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return "", fmt.Errorf("database: parse DATABASE_URL: %w", err)
		}
		switch u.Scheme {
		case "postgres", "postgresql":
			u.Scheme = "pgx5"
			return u.String(), nil
		default:
			return "", fmt.Errorf("database: DATABASE_URL must use a postgres/postgresql scheme, got %q", u.Scheme)
		}
	}

	q := url.Values{}
	q.Set("sslmode", cfg.SSLMode)
	u := url.URL{
		Scheme:   "pgx5",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, cfg.Port),
		Path:     "/" + cfg.Name,
		RawQuery: q.Encode(),
	}
	return u.String(), nil
}

// openMigrate builds a golang-migrate instance over the embedded SQL files.
func openMigrate(cfg config.DBConfig) (*migrate.Migrate, error) {
	dsn, err := migrationDSN(cfg)
	if err != nil {
		return nil, err
	}

	src, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: load source: %w", err)
	}
	defer src.Close()

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return nil, fmt.Errorf("migrations: init: %w", err)
	}
	return m, nil
}

// RunMigrations applies all pending embedded migrations at startup.
func RunMigrations(cfg config.DBConfig) error {
	m, err := openMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// MigrateTo moves the schema to exactly the given migration version,
// running up or down files as needed (e.g. MigrateTo(9) rolls back 0010
// by executing its down file). It exists for migration round-trip tests;
// production startup keeps using RunMigrations.
func MigrateTo(cfg config.DBConfig, version uint) error {
	m, err := openMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: migrate to %d: %w", version, err)
	}
	return nil
}
