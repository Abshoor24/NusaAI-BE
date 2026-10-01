package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeError writes a standard error envelope directly from middleware. It
// must only contain safe, generic content (no stack traces, no internals).
//
// This is the single centralised error writer used by every middleware
// (auth, CORS, rate-limit, recovery). It produces the exact same JSON shape
// as pkg/response.RespondError so the frontend needs only one error handler.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errBody := map[string]interface{}{
		"code":    code,
		"message": message,
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   errBody,
	})

	slog.Default().Warn("middleware error response",
		"status", status,
		"code", code,
		"message", message,
	)
}
