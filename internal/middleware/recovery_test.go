package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecoveryConvertsPanicToJSONEnvelope(t *testing.T) {
	panicking := Recovery(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("secret stack trace with db password=hunter2")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	panicking.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body must be JSON: %v", err)
	}
	if out["success"] != false {
		t.Errorf("envelope success = %v, want false", out["success"])
	}
	for _, leaked := range []string{"hunter2", "stack", "panic("} {
		if strings.Contains(body, leaked) {
			t.Errorf("body leaks internals (%q): %s", leaked, body)
		}
	}
}
