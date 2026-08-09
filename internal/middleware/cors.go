package middleware

import (
	"net/http"
)

// CORS restricts cross-origin requests to the configured allowlist. Requests
// without an Origin header (same-origin, curl) are unaffected. Wildcard "*"
// is never applied: the production default is an explicit list from
// CORS_ALLOWED_ORIGINS.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			if r.Method == http.MethodOptions {
				if _, ok := allowed[origin]; ok {
					w.WriteHeader(http.StatusNoContent)
				} else {
					writeError(w, http.StatusForbidden, "FORBIDDEN", "Origin is not allowed")
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
