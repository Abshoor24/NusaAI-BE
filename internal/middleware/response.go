package middleware

import (
	"encoding/json"
	"net/http"
)

// writeError writes a standard error envelope directly from middleware. It
// must only contain safe, generic content (no stack traces, no internals).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
