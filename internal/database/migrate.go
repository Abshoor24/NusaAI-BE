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

	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/migrations"
)

// migrationDSN builds a connection URL consumable by golang-migrate using the
// pgx/v5 database driver. Values are URL-escaped to avoid breaking on special
// characters in passwords.
func migrationDSN(cfg config.DBConfig) string {
	q := url.Values{}
	q.Set("sslmode", cfg.SSLMode)
	u := url.URL{
		Scheme:   "pgx5",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, cfg.Port),
		Path:     "/" + cfg.Name,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// RunMigrations applies all pending embedded migrations at startup.
func RunMigrations(cfg config.DBConfig) error {
	src, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("migrations: load source: %w", err)
	}
	defer src.Close()

	m, err := migrate.NewWithSourceInstance("iofs", src, migrationDSN(cfg))
	if err != nil {
		return fmt.Errorf("migrations: init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}
