package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsTestHandler() http.Handler {
	return CORS([]string{"http://localhost:3000"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestCORSAllowedOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	corsTestHandler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") || !strings.Contains(got, "Accept") {
		t.Errorf("Allow-Headers = %q, must include Authorization and Accept", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Content-Disposition") {
		t.Errorf("Expose-Headers = %q, must include Content-Disposition", got)
	}
	// Never a wildcard together with credentialed requests.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Error("Allow-Origin must never be *")
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Origin", "https://evil.test")
	corsTestHandler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for disallowed origin", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, non-preflight request must still reach handler", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	corsTestHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Error("preflight must set Access-Control-Max-Age")
	}
	if methods := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, "POST") || !strings.Contains(methods, "DELETE") {
		t.Errorf("Allow-Methods = %q", methods)
	}
}

func TestCORSPreflightDisallowed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	req.Header.Set("Origin", "https://evil.test")
	corsTestHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("preflight status = %d, want 403", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("preflight error must be JSON: %v", err)
	}
	if body["success"] != false {
		t.Errorf("envelope success = %v, want false", body["success"])
	}
}
