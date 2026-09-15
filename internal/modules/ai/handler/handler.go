package handler

import (
	"log/slog"
	"net/http"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/service"
	"github.com/Abshoor24/backend-go/pkg/response"
)

// Handler exposes the AI foundation endpoints. It depends only on the AI
// Service interface, never on a concrete provider SDK.
type Handler struct {
	ai     service.Service
	logger *slog.Logger
	cfg    config.AIConfig
}

func New(svc service.Service, logger *slog.Logger, cfg config.AIConfig) *Handler {
	return &Handler{ai: svc, logger: logger, cfg: cfg}
}

// Generate handles POST /ai/generate. This is a FOUNDATION endpoint to prove
// the AI abstraction end-to-end. It is intentionally generic (no company
// prompt, no business context) until the client's AI use case is known.
//
// TODO: CLIENT REQUIREMENT - decide whether the real AI endpoint needs
// authentication, streaming, usage tracking and richer context once the
// client's use case is defined.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req model.GenerateTextRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.GenerateText(r.Context(), req)
	if err != nil {
		response.RespondError(w, h.logger, err)
		return
	}

	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
}

// Health handles GET /ai/health. It only inspects the local AI configuration
// and never calls the external provider, so it does not consume API quota.
// The API key itself is never returned.
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

// Chat handles POST /ai/chat. It receives conversation history and a message,
// and returns the assistant's reply. No authentication is required, which
// is ideal for public website chatbots.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("chatbot handler entered", "request_id", r.Header.Get("X-Request-ID"))

	var req model.ChatRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("chatbot request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.Chat(r.Context(), req)
	if err != nil {
		h.logger.Error("chatbot ai service execution failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	h.logger.Info("chatbot gemini response received successfully")

	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
}

// GenerateSolution handles POST /ai/generate-solution. It receives business
// information and returns a structured digital solution recommendation.
func (h *Handler) GenerateSolution(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("generate-solution handler entered", "request_id", r.Header.Get("X-Request-ID"))

	var req model.GenerateSolutionRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("generate-solution request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	resp, err := h.ai.GenerateSolution(r.Context(), req)
	if err != nil {
		h.logger.Error("generate-solution ai service execution failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	h.logger.Info("generate-solution response generated successfully")

	response.RespondSuccess(w, http.StatusOK, "Request successful", resp)
}

// HandleUploadDocument handles POST /documents/upload. It converts the uploaded
// text content into vector embeddings and stores it into the company_documents table.
func (h *Handler) HandleUploadDocument(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("upload-document handler entered", "request_id", r.Header.Get("X-Request-ID"))

	var req model.DocumentUploadRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("upload-document request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	if err := h.ai.IngestDocument(r.Context(), req); err != nil {
		h.logger.Error("upload-document ai service execution failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	h.logger.Info("document uploaded and embedded successfully", "company_id", req.CompanyID)

	response.RespondSuccess(w, http.StatusCreated, "Document uploaded and ingested successfully", map[string]interface{}{
		"company_id": req.CompanyID,
	})
}

// HandleGenerateCompanyProfile handles POST /ai/generate-company-profile. It retrieves
// stored company documents and generates a structured company profile using RAG.
func (h *Handler) HandleGenerateCompanyProfile(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("generate-company-profile handler entered", "request_id", r.Header.Get("X-Request-ID"))

	var req model.GenerateCompanyProfileRequest
	if err := response.DecodeAndValidate(w, r, &req, h.cfg.MaxBytes); err != nil {
		h.logger.Error("generate-company-profile request validation failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	profile, err := h.ai.GenerateCompanyProfile(r.Context(), req)
	if err != nil {
		h.logger.Error("generate-company-profile ai service execution failed", "error", err)
		response.RespondError(w, h.logger, err)
		return
	}

	h.logger.Info("company profile generated successfully", "company_id", req.CompanyID)

	response.RespondSuccess(w, http.StatusOK, "Request successful", profile)
}
