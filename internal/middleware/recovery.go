package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
)

// Recovery converts panics into a generic 500 response and logs the panic
// server-side. Expected runtime errors are handled via apperror, never panic.
func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"request_id", GetRequestID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"panic", fmt.Sprintf("%v", rec),
					)
					writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An unexpected error occurred")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
