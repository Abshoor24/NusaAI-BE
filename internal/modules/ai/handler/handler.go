package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	projectservice "github.com/Abshoor24/backend-go/internal/modules/project/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// QuotaReporter is implemented by the project service. It is optional so the
// diagnostic router still works in tests without a database.
type QuotaReporter interface {
	QuotaStatus(ctx context.Context) projectservice.QuotaStatus
}

// Handler exposes the AI diagnostic endpoints.
type Handler struct {
	cfg   config.AIConfig
	quota QuotaReporter
}

// New creates a new AI Handler.
func New(cfg config.AIConfig, quota ...QuotaReporter) *Handler {
	h := &Handler{cfg: cfg}
	if len(quota) > 0 {
		h.quota = quota[0]
	}
	return h
}

// Health handles GET /api/v1/ai/health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	providerConfigured := h.cfg.Provider != ""
	apiKeyConfigured := h.cfg.APIKey != ""
	modelConfigured := h.cfg.Model != ""

	response.RespondSuccess(w, http.StatusOK, "Request successful", map[string]interface{}{
		"provider":   h.cfg.Provider,
		"configured": providerConfigured && apiKeyConfigured && modelConfigured,
		"checks": map[string]bool{
			"provider": providerConfigured,
			"api_key":  apiKeyConfigured,
			"model":    modelConfigured,
		},
	})
}

// QuotaStatus handles GET /api/v1/ai/quota-status. The count is an estimate of
// provider generate calls in the current 15:00 WIB window, persisted across
// restarts; it is not a provider-side billing statement.
func (h *Handler) QuotaStatus(w http.ResponseWriter, r *http.Request) {
	if h.quota != nil {
		response.RespondSuccess(w, http.StatusOK, "Request successful", h.quota.QuotaStatus(r.Context()))
		return
	}
	response.RespondSuccess(w, http.StatusOK, "Request successful", h.fallbackQuotaStatus())
}

func (h *Handler) fallbackQuotaStatus() projectservice.QuotaStatus {
	limit := h.cfg.DailyQuotaLimit
	if limit <= 0 {
		limit = 20
	}
	models := make([]projectservice.QuotaModelStatus, 0, 1+len(h.cfg.FallbackModels))
	seen := make(map[string]bool, 1+len(h.cfg.FallbackModels))
	for _, model := range append([]string{h.cfg.Model}, h.cfg.FallbackModels...) {
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, projectservice.QuotaModelStatus{
			Model: model, Remaining: limit, Status: "unknown",
		})
	}
	now := time.Now()
	return projectservice.QuotaStatus{
		WindowStart: now,
		ResetAt:     now,
		DailyLimit:  limit,
		Models:      models,
	}
}
