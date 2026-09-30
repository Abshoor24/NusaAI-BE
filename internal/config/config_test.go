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
	if cfg.AI.BaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("AI.BaseURL = %q, want default", cfg.AI.BaseURL)
	}
	if cfg.AI.Provider != "gemini" {
		t.Errorf("AI.Provider = %q, want gemini", cfg.AI.Provider)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 ||
		cfg.CORS.AllowedOrigins[0] != "http://localhost:3000" ||
		cfg.CORS.AllowedOrigins[1] != "http://localhost:5173" {
		t.Errorf("CORS.AllowedOrigins = %v, want [http://localhost:3000 http://localhost:5173]", cfg.CORS.AllowedOrigins)
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

func TestLoadCORSDefaultsDependOnEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "a-very-long-production-secret-0123456789")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("APP_ENV", "production")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.CORS.AllowedOrigins) != 0 {
		t.Errorf("production CORS default = %v, want empty (no cross-site origin)", cfg.CORS.AllowedOrigins)
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

// The deferred modules (Judge0 execution, RAG) must not block startup:
// the server boots with none of their configuration set.
func TestLoadWithoutDeferredModuleConfig(t *testing.T) {
	t.Setenv("APP_ENV", "test") // skip local .env so the test is hermetic
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AI_API_KEY", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("EXEC_API_KEY", "")
	t.Setenv("EXEC_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() without Judge0/RAG config error = %v", err)
	}
	if cfg.Generate.PerMinute != 5 {
		t.Errorf("Generate.PerMinute = %d, want 5", cfg.Generate.PerMinute)
	}
	if cfg.Project.MaxImages != 4 {
		t.Errorf("Project.MaxImages = %d, want 4", cfg.Project.MaxImages)
	}
	if cfg.Project.MaxHTMLBytes != 524288 {
		t.Errorf("Project.MaxHTMLBytes = %d, want 524288", cfg.Project.MaxHTMLBytes)
	}
	if cfg.AI.MaxGenerateBodyBytes != 32<<20 {
		t.Errorf("AI.MaxGenerateBodyBytes = %d, want %d", cfg.AI.MaxGenerateBodyBytes, 32<<20)
	}
}

func TestLoadRejectsWeakJWTProduction(t *testing.T) {
	for _, secret := range []string{
		"",
		"short",
		"your_jwt_secret_key",
		"replace-with-a-long-random-string",
	} {
		t.Setenv("APP_ENV", "production")
		t.Setenv("JWT_SECRET", secret)
		if _, err := Load(); err == nil {
			t.Errorf("production JWT_SECRET=%q: want startup error, got nil", secret)
		}
	}
}

func TestLoadAcceptsStrongJWTProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "a-very-long-production-secret-0123456789")
	if _, err := Load(); err != nil {
		t.Errorf("production strong secret: Load() error = %v", err)
	}
}

func TestLoadWarnsWeakJWTDevelopment(t *testing.T) {
	// Development stays frictionless: weak secrets warn (see logs) but boot.
	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_SECRET", "test-secret")
	if _, err := Load(); err != nil {
		t.Errorf("development weak secret: Load() error = %v", err)
	}
}

func TestLoadGenerationKnobsUnsetByDefault(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "")
	t.Setenv("AI_THINKING_BUDGET", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.MaxOutputTokens != 0 {
		t.Errorf("MaxOutputTokens = %d, want 0 (unset)", cfg.AI.MaxOutputTokens)
	}
	if cfg.AI.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget = %v, want nil (unset)", *cfg.AI.ThinkingBudget)
	}
}

func TestLoadGenerationKnobsWhenSet(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "8192")
	t.Setenv("AI_THINKING_BUDGET", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.MaxOutputTokens != 8192 {
		t.Errorf("MaxOutputTokens = %d, want 8192", cfg.AI.MaxOutputTokens)
	}
	if cfg.AI.ThinkingBudget == nil || *cfg.AI.ThinkingBudget != 0 {
		t.Errorf("ThinkingBudget = %v, want pointer to 0", cfg.AI.ThinkingBudget)
	}
}

func TestLoadRejectsBadGenerationKnobs(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "-5")
	t.Setenv("AI_THINKING_BUDGET", "")
	if _, err := Load(); err == nil {
		t.Error("negative AI_MAX_OUTPUT_TOKENS: want error, got nil")
	}
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "")
	t.Setenv("AI_THINKING_BUDGET", "-1")
	if _, err := Load(); err == nil {
		t.Error("negative AI_THINKING_BUDGET: want error, got nil")
	}
	t.Setenv("AI_THINKING_BUDGET", "not-a-number")
	if _, err := Load(); err == nil {
		t.Error("malformed AI_THINKING_BUDGET: want error, got nil")
	}
}

func TestLoadDebugEdgesGatedToDevelopment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AI_DEBUG_LOG_OUTPUT_EDGES", "true")
	t.Setenv("APP_ENV", "development")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AI.DebugLogOutputEdges {
		t.Error("development: DebugLogOutputEdges should stay true")
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "a-very-long-production-secret-0123456789")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.DebugLogOutputEdges {
		t.Error("production: DebugLogOutputEdges must be forced false")
	}
}
