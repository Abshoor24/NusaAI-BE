// Package tests holds integration tests that require real infrastructure.
package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/database"
)

// TestDatabaseIntegration verifies the real PostgreSQL pool and the embedded
// migration runner. It is skipped by default and only runs when
// TEST_DB_INTEGRATION=true and the DATABASE_* environment variables point to
// a reachable database.
func TestDatabaseIntegration(t *testing.T) {
	if os.Getenv("TEST_DB_INTEGRATION") != "true" {
		t.Skip("set TEST_DB_INTEGRATION=true to run database integration tests")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := database.RunMigrations(cfg.DB); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	db, err := database.New(ctx, cfg.DB)
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		t.Fatalf("db.Ping() error = %v", err)
	}
}
