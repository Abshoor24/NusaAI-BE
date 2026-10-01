package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/model"
)

func testTokens(expiration time.Duration) *jwt.Manager {
	return jwt.NewManager(config.JWTConfig{
		Secret:     "auth-test-secret-that-is-long-enough",
		Expiration: expiration,
		Issuer:     "test",
	})
}

func authResponse(t *testing.T, tokens *jwt.Manager, token string) (int, map[string]any) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	RequireAuth(tokens)(next).ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body must be JSON: %v", err)
	}
	return rec.Code, body
}

func errorMessage(t *testing.T, body map[string]any) string {
	t.Helper()
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope error = %v, want object", body["error"])
	}
	msg, _ := errObj["message"].(string)
	return msg
}

func TestAuthMissingToken(t *testing.T) {
	code, body := authResponse(t, testTokens(time.Hour), "")
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	if got := errorMessage(t, body); got != "Missing authentication token" {
		t.Errorf("message = %q", got)
	}
}

func TestAuthInvalidToken(t *testing.T) {
	tokens := testTokens(time.Hour)
	valid, _, err := tokens.Generate(&model.User{ID: "u", Email: "e@x.com"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	code, body := authResponse(t, tokens, valid+"tampered")
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	if got := errorMessage(t, body); got != "Invalid authentication token" {
		t.Errorf("message = %q, want invalid-token message", got)
	}
}

func TestAuthExpiredToken(t *testing.T) {
	expired, _, err := testTokens(-time.Hour).Generate(&model.User{ID: "u", Email: "e@x.com"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Parse with a normal-expiry manager: signature valid, exp in the past.
	code, body := authResponse(t, testTokens(time.Hour), expired)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	if got := errorMessage(t, body); got != "Authentication token has expired" {
		t.Errorf("message = %q, want expiry message so frontend can auto-logout", got)
	}
}
