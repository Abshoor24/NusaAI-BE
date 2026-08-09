package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error { return f.err }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthOK(t *testing.T) {
	h := NewHealthHandler(fakePinger{}, testLogger())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success {
		t.Error("success = false, want true")
	}
	if body.Data["status"] != "ok" {
		t.Errorf(`data.status = %q, want "ok"`, body.Data["status"])
	}
}

func TestHealthDBConnected(t *testing.T) {
	h := NewHealthHandler(fakePinger{}, testLogger())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/db", nil)

	h.DBHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data["database"] != "connected" {
		t.Errorf(`data.database = %q, want "connected"`, body.Data["database"])
	}
}

func TestHealthDBUnavailable(t *testing.T) {
	h := NewHealthHandler(fakePinger{err: errors.New("connection refused")}, testLogger())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/db", nil)

	h.DBHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// The internal error must never leak to the client.
	raw := rec.Body.String()
	if strings.Contains(raw, "connection refused") {
		t.Errorf("response leaks internal error: %s", raw)
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Success {
		t.Error("success = true, want false")
	}
	if body.Error.Code != "SERVICE_UNAVAILABLE" {
		t.Errorf("error.code = %q, want SERVICE_UNAVAILABLE", body.Error.Code)
	}
}
