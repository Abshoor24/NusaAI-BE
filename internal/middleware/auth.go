package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/model"
)

const claimsKey ctxKey = "claims"

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
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired authentication token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole restricts a route to users with the given role. It answers
// "what is this user allowed to do?" (authorization).
func RequireRole(role model.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authentication token")
				return
			}
			if claims.Role != role {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to access this resource")
				return
			}
			next.ServeHTTP(w, r)
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
