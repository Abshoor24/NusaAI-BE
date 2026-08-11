package logger

import (
	"log/slog"
	"os"
)

// New returns a *slog.Logger configured for the given environment.
// Production logs at INFO level (JSON), development/test logs at DEBUG level
// (text) for readability.
func New(env string) *slog.Logger {
	var level slog.Level
	var handler slog.Handler
	switch env {
	case "production":
		level = slog.LevelInfo
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		level = slog.LevelDebug
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}
