package response

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Abshoor24/backend-go/internal/apperror"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestRespondSuccessEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	RespondSuccess(rec, http.StatusOK, "Request successful", map[string]string{"response": "halo"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	// Content-Length must match the written body.
	body := rec.Body.Bytes()
	if cl := rec.Header().Get("Content-Length"); cl != "" && cl != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(body))
	}

	var out struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
		Message string            `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !out.Success {
		t.Error("success = false, want true")
	}
	if out.Message != "Request successful" {
		t.Errorf("message = %q, want \"Request successful\"", out.Message)
	}
	if out.Data["response"] != "halo" {
		t.Errorf("data = %v", out.Data)
	}
}

func TestRespondErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	RespondError(rec, testLogger(), apperror.ValidationFields(map[string]string{"email": "must be a valid email address"}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var out struct {
		Success bool `json:"success"`
		Error   struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if out.Success {
		t.Error("success = true, want false")
	}
	if out.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", out.Error.Code)
	}
}

func TestRespondErrorReason(t *testing.T) {
	// AI errors carry the stable reason sub-code; non-AI errors (and AI
	// errors without one) omit the field entirely.
	for _, tc := range []struct {
		name       string
		err        error
		wantReason string
	}{
		{"overloaded", apperror.AIProviderReason(apperror.ReasonProviderOverloaded, "busy", nil), "PROVIDER_OVERLOADED"},
		{"timeout", apperror.AIProviderReason(apperror.ReasonStreamTimeout, "timed out", nil), "STREAM_TIMEOUT"},
		{"ai without reason", apperror.AIProvider("generic", nil), ""},
		{"non-ai", apperror.NotFound("Project not found", nil), ""},
		{"validation", apperror.Validation("bad"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondError(rec, testLogger(), tc.err)

			var out map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			errObj, _ := out["error"].(map[string]any)
			got, present := errObj["reason"]
			if tc.wantReason == "" {
				if present {
					t.Errorf("reason = %v, want field omitted", got)
				}
				return
			}
			if got != tc.wantReason {
				t.Errorf("reason = %v, want %q", got, tc.wantReason)
			}
		})
	}
}

func TestRespondSuccessJSONEncodingError(t *testing.T) {
	// A channel cannot be marshaled by encoding/json; writeJSON must fall back
	// to a safe 500 and must not panic.
	rec := httptest.NewRecorder()
	RespondSuccess(rec, http.StatusOK, "Request successful", make(chan int))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var out struct {
		Success bool `json:"success"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("fallback body must be JSON: %v", err)
	}
	if out.Success || out.Error.Code != "INTERNAL_SERVER_ERROR" {
		t.Errorf("body = %q, want JSON envelope with INTERNAL_SERVER_ERROR", rec.Body.String())
	}
}

// failingWriter simulates an http.ResponseWriter whose Write always fails.
type failingWriter struct {
	status int
	header http.Header
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingWriter) WriteHeader(code int) {
	w.status = code
}

func (w *failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteJSONWriteFailureIsSafe(t *testing.T) {
	fw := &failingWriter{}
	// Must not panic and must not attempt a second WriteHeader after the first.
	RespondSuccess(fw, http.StatusOK, "Request successful", map[string]string{"response": "halo"})

	if fw.status != http.StatusOK {
		t.Errorf("status = %d, want %d", fw.status, http.StatusOK)
	}
}
