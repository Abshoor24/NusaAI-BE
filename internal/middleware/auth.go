package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Abshoor24/backend-go/internal/jwt"
)

const claimsKey ctxKey = "claims"

// writeAuthError maps a token parse failure to a stable 401 response. An
// expired token and a forged/malformed token share HTTP 401 + code
// UNAUTHORIZED (the api.md contract) but carry distinct messages so the
// frontend can auto-logout on expiry while treating other failures as bugs.
func writeAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, jwt.ErrTokenExpired) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token has expired")
		return
	}
	writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authentication token")
}

// RequireAuth validates the Bearer token and stores its claims in the
// request context. It answers "who is this user?" (authentication).
func RequireAuth(tokens *jwt.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authentication token")
				return
			}

			claims, err := tokens.Parse(token)
			if err != nil {
				writeAuthError(w, err)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuthSSE is identical to RequireAuth but additionally accepts the JWT
// via a `?token=` query parameter. This exists solely because the browser
// EventSource API cannot set custom HTTP headers. It is applied ONLY to the
// SSE streaming endpoint — every other protected route keeps RequireAuth and
// requires the Authorization header as before.
func RequireAuthSSE(tokens *jwt.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			if token == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authentication token")
				return
			}

			claims, err := tokens.Parse(token)
			if err != nil {
				writeAuthError(w, err)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFrom returns the authenticated user's claims stored by RequireAuth.
func ClaimsFrom(ctx context.Context) (*jwt.Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*jwt.Claims)
	return claims, ok
}

// UserIDFrom returns the authenticated user's ID, if present.
func UserIDFrom(ctx context.Context) (string, bool) {
	claims, ok := ClaimsFrom(ctx)
	if !ok {
		return "", false
	}
	return claims.Subject, true
}

// WithUserID injects a user ID into the context (intended for tests).
func WithUserID(ctx context.Context, userID string) context.Context {
	c := &jwt.Claims{}
	c.Subject = userID
	return context.WithValue(ctx, claimsKey, c)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
