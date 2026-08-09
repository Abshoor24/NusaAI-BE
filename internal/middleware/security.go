package middleware

import "net/http"

// SecurityHeaders sets conservative security-relevant response headers.
// Content-Security-Policy is intentionally omitted: this is a JSON API, not a
// document-serving host. TODO: CLIENT REQUIREMENT - revisit headers once the
// frontend serving strategy (separate static host vs. Go-served assets) is
// known.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
