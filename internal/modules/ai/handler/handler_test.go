package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	projectservice "github.com/Abshoor24/backend-go/internal/modules/project/service"
)

type stubQuotaReporter struct {
	status projectservice.QuotaStatus
}

func (s stubQuotaReporter) QuotaStatus(context.Context) projectservice.QuotaStatus {
	return s.status
}

func TestQuotaStatusEndpointReturnsPerModelUsage(t *testing.T) {
	until := time.Date(2026, 11, 17, 15, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	reporter := stubQuotaReporter{status: projectservice.QuotaStatus{
		WindowStart: until.Add(-24 * time.Hour),
		ResetAt:     until,
		DailyLimit:  20,
		Models: []projectservice.QuotaModelStatus{
			{Model: "gemini-primary", Used: 20, Remaining: 0, Exhausted: true, ExhaustedUntil: &until, Status: "exhausted"},
			{Model: "gemini-fallback", Used: 4, Remaining: 16, Status: "available"},
		},
	}}
	h := New(config.AIConfig{}, reporter)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/quota-status", nil)
	rec := httptest.NewRecorder()

	h.QuotaStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			DailyLimit int                               `json:"daily_limit"`
			Models     []projectservice.QuotaModelStatus `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success || body.Data.DailyLimit != 20 {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Data.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(body.Data.Models))
	}
	primary := body.Data.Models[0]
	if primary.Model != "gemini-primary" || !primary.Exhausted || primary.Remaining != 0 {
		t.Errorf("primary = %+v, want exhausted", primary)
	}
	if primary.ExhaustedUntil == nil || !primary.ExhaustedUntil.Equal(until) {
		t.Errorf("exhausted_until = %v, want %v", primary.ExhaustedUntil, until)
	}
	if body.Data.Models[1].Remaining != 16 {
		t.Errorf("fallback remaining = %d, want 16", body.Data.Models[1].Remaining)
	}
}

func TestQuotaStatusEndpointWithoutReporterFallsBackToConfig(t *testing.T) {
	h := New(config.AIConfig{
		Model:           "gemini-primary",
		FallbackModels:  []string{"gemini-fallback"},
		DailyQuotaLimit: 20,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/quota-status", nil)
	rec := httptest.NewRecorder()

	h.QuotaStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Data struct {
			Models []projectservice.QuotaModelStatus `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(body.Data.Models))
	}
	for _, m := range body.Data.Models {
		if m.Status != "unknown" || m.Remaining != 20 {
			t.Errorf("model %s = %+v, want unknown with remaining 20", m.Model, m)
		}
	}
}
