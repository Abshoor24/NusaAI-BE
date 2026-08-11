package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.App.Env != "development" {
		t.Errorf("App.Env = %q, want development", cfg.App.Env)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %q, want 8080", cfg.Server.Port)
	}
	if cfg.DB.SSLMode != "disable" {
		t.Errorf("DB.SSLMode = %q, want disable", cfg.DB.SSLMode)
	}
	if cfg.AI.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("AI.BaseURL = %q, want default", cfg.AI.BaseURL)
	}
	if cfg.AI.Provider != "openai" {
		t.Errorf("AI.Provider = %q, want openai", cfg.AI.Provider)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 ||
		cfg.CORS.AllowedOrigins[0] != "http://localhost:5173" ||
		cfg.CORS.AllowedOrigins[1] != "http://127.0.0.1:5173" {
		t.Errorf("CORS.AllowedOrigins = %v, want [http://localhost:5173 http://127.0.0.1:5173]", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadRequiresJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error when JWT_SECRET is empty")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error = %q, want it to mention JWT_SECRET", err.Error())
	}
}

func TestLoadRejectsInvalidEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("APP_ENV", "staging")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid APP_ENV")
	}
}

func TestLoadRejectsInvalidRateLimit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("RATE_LIMIT_ENABLED", "true")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "0")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid rate limit")
	}
}

func TestLoadCORSMultipleOrigins(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://a.test, http://b.test, ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("CORS.AllowedOrigins = %v, want 2 entries", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadDurationParsing(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("JWT_EXPIRATION", "2h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWT.Expiration != 2*time.Hour {
		t.Errorf("JWT.Expiration = %v, want 2h", cfg.JWT.Expiration)
	}
}

func TestLoadDatabaseURLPrecedence(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("DATABASE_URL", "postgresql://user:p@ss@db.example.com:5432/backend?sslmode=require")
	t.Setenv("DATABASE_HOST", "127.0.0.1")
	t.Setenv("DATABASE_PASSWORD", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DB.URL == "" {
		t.Fatal("DB.URL = empty, want the DATABASE_URL value")
	}
	// Password is optional when a full URL is provided.
	if cfg.DB.HostLabel() != "db.example.com:5432" {
		t.Errorf("DB.HostLabel() = %q, want db.example.com:5432", cfg.DB.HostLabel())
	}
	if strings.Contains(cfg.DB.HostLabel(), "p@ss") {
		t.Error("DB.HostLabel() must not expose the password")
	}
}
