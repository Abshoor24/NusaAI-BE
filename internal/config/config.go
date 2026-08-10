package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from environment variables.
// Secrets (database password, JWT secret, AI API key) are only read from the
// environment and never hardcoded or written into source code.
type Config struct {
	App    AppConfig
	Server ServerConfig
	DB     DBConfig
	JWT    JWTConfig
	AI     AIConfig
	CORS   CORSConfig
	Rate   RateLimitConfig
}

type AppConfig struct {
	Env  string // development | production
	Name string
}

type ServerConfig struct {
	Host         string
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type DBConfig struct {
	// URL is a full PostgreSQL connection string (e.g. Supabase). When set it
	// takes precedence over the individual Host/Port/User/Password/Name/SSLMode
	// fields and is passed through to the driver as-is.
	URL               string
	Host              string
	Port              string
	User              string
	Password          string
	Name              string
	SSLMode           string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	ConnectTimeout    time.Duration
	HealthCheckPeriod time.Duration
}

type JWTConfig struct {
	Secret     string
	Expiration time.Duration
	Issuer     string
}

type AIConfig struct {
	APIKey   string
	Model    string
	BaseURL  string
	Timeout  time.Duration
	MaxBytes int64
}

type CORSConfig struct {
	AllowedOrigins []string
}

type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
}

// loadEnvFile loads the local .env file for development when it is present.
// It never crashes when the file is missing and it never overrides variables
// that are already set in the process environment, so in production the real
// environment variables always win.
func loadEnvFile() {
	// Only local development loads .env; an unset APP_ENV defaults to
	// development so the file is still picked up in a fresh shell.
	if env := os.Getenv("APP_ENV"); env != "" && env != "development" {
		return
	}
	if _, err := os.Stat(".env"); err != nil {
		return
	}
	if err := godotenv.Load(); err != nil {
		log.Printf("config: warning: could not load .env: %v", err)
	}
}

// Load reads configuration from the environment and validates required values.
// It returns an error naming any missing or invalid variable so the app can
// fail fast at startup.
func Load() (*Config, error) {
	loadEnvFile()

	cfg := &Config{
		App: AppConfig{
			Env:  getEnv("APP_ENV", "development"),
			Name: getEnv("APP_NAME", "backend"),
		},
		Server: ServerConfig{
			Host:         getEnv("APP_HOST", "0.0.0.0"),
			Port:         getEnv("APP_PORT", "8080"),
			ReadTimeout:  getDuration("SERVER_READ_TIMEOUT", 5*time.Second),
			WriteTimeout: getDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:  getDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
		},
		DB: DBConfig{
			URL:               getEnv("DATABASE_URL", ""),
			Host:              getEnv("DATABASE_HOST", "localhost"),
			Port:              getEnv("DATABASE_PORT", "5432"),
			User:              getEnv("DATABASE_USER", "postgres"),
			Password:          getEnv("DATABASE_PASSWORD", ""),
			Name:              getEnv("DATABASE_NAME", "postgres"),
			SSLMode:           getEnv("DATABASE_SSLMODE", "disable"),
			MaxConns:          int32(getInt("DATABASE_MAX_CONNS", 10)),
			MinConns:          int32(getInt("DATABASE_MIN_CONNS", 0)),
			MaxConnLifetime:   getDuration("DATABASE_MAX_CONN_LIFETIME", time.Hour),
			ConnectTimeout:    getDuration("DATABASE_CONNECT_TIMEOUT", 10*time.Second),
			HealthCheckPeriod: getDuration("DATABASE_HEALTH_CHECK_PERIOD", time.Minute),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", ""),
			Expiration: getDuration("JWT_EXPIRATION", 24*time.Hour),
			Issuer:     getEnv("JWT_ISSUER", "backend"),
		},
		AI: AIConfig{
			APIKey:   getEnv("AI_API_KEY", ""),
			Model:    getEnv("AI_MODEL", ""),
			BaseURL:  getEnv("AI_BASE_URL", "https://api.openai.com/v1"),
			Timeout:  getDuration("AI_TIMEOUT", 30*time.Second),
			MaxBytes: getInt64("AI_MAX_REQUEST_BYTES", 1<<20), // 1 MB default
		},
		CORS: CORSConfig{
			AllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		},
		Rate: RateLimitConfig{
			Enabled:   getBool("RATE_LIMIT_ENABLED", true),
			PerMinute: getInt("RATE_LIMIT_PER_MINUTE", 120),
			Burst:     getInt("RATE_LIMIT_BURST", 120),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.JWT.Secret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	// AI config is intentionally optional: the AI provider is only required
	// when the /ai/chat prototype endpoint is used. TODO: CLIENT REQUIREMENT
	// decide whether AI is mandatory for the final product.
	if err := c.App.valid(); err != nil {
		return err
	}
	if err := c.Server.valid(); err != nil {
		return err
	}
	if err := c.DB.valid(); err != nil {
		return err
	}
	if c.Rate.Enabled && c.Rate.PerMinute <= 0 {
		return fmt.Errorf("config: RATE_LIMIT_PER_MINUTE must be greater than 0 when rate limiting is enabled")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required environment variable(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c AppConfig) valid() error {
	switch c.Env {
	case "development", "production", "test":
		return nil
	default:
		return fmt.Errorf("config: APP_ENV must be one of development|production|test, got %q", c.Env)
	}
}

func (c ServerConfig) valid() error {
	if c.Port == "" {
		return fmt.Errorf("config: APP_PORT must not be empty")
	}
	return nil
}

func (c DBConfig) valid() error {
	// A full connection string carries its own credentials, so the per-field
	// password requirement only applies to the individual-variable path.
	if c.URL == "" && c.Password == "" && !isLoopbackHost(c.Host) {
		return fmt.Errorf("config: DATABASE_PASSWORD is required when connecting to a non-loopback host")
	}
	return nil
}

// HostLabel returns the host being connected to for log messages without
// exposing credentials. When DATABASE_URL is used, the host is extracted from
// the URL and the password is never included.
func (c DBConfig) HostLabel() string {
	if c.URL != "" {
		if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
			return u.Host
		}
		return "<from DATABASE_URL>"
	}
	return c.Host
}

// isLoopbackHost reports whether a host string refers to the local machine.
func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getInt64(key string, fallback int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
