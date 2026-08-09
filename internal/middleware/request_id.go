package middleware

import (
	"context"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

type ctxKey string

const requestIDKey ctxKey = "requestID"

// safeRequestID guards against header injection: only alphanumerics, dashes,
// underscores, dots, max 64 chars are accepted.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID ensures every request has an ID. A client-supplied X-Request-ID is
// reused only when it is safe; otherwise a UUID is generated. The ID is
// returned to the client and stored in context for logging.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || !safeRequestID.MatchString(id) {
			id = uuid.NewString()
		}

		w.Header().Set("X-Request-ID", id)
		r.Header.Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the request ID stored in the context, if any.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
