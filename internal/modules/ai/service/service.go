package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

// Service is the AI abstraction used by HTTP handlers. It decouples the
// application from any concrete LLM API, so the AI can later be used for
// chatbot, recommendation, document Q&A, semantic search, content assistance,
// lead qualification, etc., without touching the handler layer.
type Service interface {
	GenerateText(ctx context.Context, req model.GenerateTextRequest) (model.GenerateTextResponse, error)
	Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error)
	GenerateSolution(ctx context.Context, req model.GenerateSolutionRequest) (model.GenerateSolutionResponse, error)
	IngestDocument(ctx context.Context, req model.DocumentUploadRequest) error
	GenerateCompanyProfile(ctx context.Context, req model.GenerateCompanyProfileRequest) (*model.GeneratedCompanyProfile, error)
}

type service struct {
	provider provider.Provider
	cfg      config.AIConfig
	logger   *slog.Logger
	pool     *pgxpool.Pool
}

func New(provider provider.Provider, cfg config.AIConfig, logger *slog.Logger, pool *pgxpool.Pool) Service {
	return &service{provider: provider, cfg: cfg, logger: logger, pool: pool}
}

// GenerateText passes a request to the configured provider and maps the
// result back to the model layer. No company-specific prompt is built here;
// timeout and transport concerns are handled by the provider.
//
// TODO: CLIENT REQUIREMENT - build system prompts / RAG context assembly for
// the actual client's AI use case once requirements are gathered.
func (s *service) GenerateText(ctx context.Context, req model.GenerateTextRequest) (model.GenerateTextResponse, error) {
	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:   s.cfg.Model,
		Prompt:  req.Prompt,
		Context: req.Context,
	})
	if err != nil {
		return model.GenerateTextResponse{}, err
	}
	return model.GenerateTextResponse{Content: resp.Content, Model: resp.Model}, nil
}

func (s *service) Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	var msgs []provider.ChatMessage

	// 1. Build system prompt with optional solution context
	systemPrompt := "You are a helpful customer service chatbot assistant."
	if req.System != "" {
		systemPrompt = req.System
	}

	// Append solution context if provided
	if req.SolutionContext != nil {
		solutionCtx := req.SolutionContext
		solutionPrompt := fmt.Sprintf(`

---
Generated Solution Context (for reference when user asks about this solution):
Business Type: %s
Business Problem: %s
Business Goal: %s

Solution Name: %s
Summary: %s
Recommended Features: %s
Recommended Technology: %s
Complexity: %s

Instructions:
- Use the above context as the primary reference when the user asks questions related to the generated solution.
- Do not invent company-specific information that is not present in the context.
- If the user asks something unrelated to the generated solution, answer normally.
- If information is unavailable from the context, clearly say that the information is not available rather than inventing it.
---
`,
			solutionCtx.BusinessType,
			solutionCtx.BusinessProblem,
			solutionCtx.BusinessGoal,
			solutionCtx.Solution.SolutionName,
			solutionCtx.Solution.Summary,
			formatStringSlice(solutionCtx.Solution.RecommendedFeatures),
			formatStringSlice(solutionCtx.Solution.RecommendedTechnology),
			solutionCtx.Solution.Complexity,
		)
		systemPrompt += solutionPrompt
	}

	msgs = append(msgs, provider.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// 2. Add history
	for _, msg := range req.History {
		msgs = append(msgs, provider.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// 3. Add latest user message
	msgs = append(msgs, provider.ChatMessage{
		Role:    "user",
		Content: req.Message,
	})

	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:    s.cfg.Model,
		Messages: msgs,
	})
	if err != nil {
		return model.ChatResponse{}, err
	}

	return model.ChatResponse{
		Response: resp.Content,
		Model:    resp.Model,
	}, nil
}

// formatStringSlice formats a string slice as a comma-separated string.
func formatStringSlice(slice []string) string {
	if len(slice) == 0 {
		return "None"
	}
	result := ""
	for i, s := range slice {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

func (s *service) GenerateSolution(ctx context.Context, req model.GenerateSolutionRequest) (model.GenerateSolutionResponse, error) {
	systemPrompt := `You are an expert digital solutions architect. Analyze the user's business type, problem, and goal. Return ONLY a valid JSON object with this exact structure:

{
  "solution_name": "string",
  "summary": "string", 
  "recommended_features": ["string", ...],
  "recommended_technology": ["string", ...],
  "complexity": "Low" | "Medium" | "High"
}

Rules:
- NO markdown, NO extra text, NO explanations
- Output ONLY the JSON object
- solution_name: concise product/platform name
- summary: 1-2 sentence description
- recommended_features: 3-7 specific features
- recommended_technology: 3-6 technologies (React, Go, PostgreSQL, etc.)
- complexity: "Low", "Medium", or "High" based on scope`

	userPrompt := fmt.Sprintf(`Business Type: %s
Business Problem: %s
Business Goal: %s`, req.BusinessType, req.BusinessProblem, req.BusinessGoal)

	resp, err := s.provider.Complete(ctx, provider.Request{
		Model: s.cfg.Model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return model.GenerateSolutionResponse{}, err
	}

	var solution model.GeneratedSolution
	if err := json.Unmarshal([]byte(resp.Content), &solution); err != nil {
		s.logger.Error("failed to parse AI solution response", "error", err, "raw", resp.Content)
		return model.GenerateSolutionResponse{}, apperror.AIProvider("The AI provider returned an invalid response format", err)
	}

	// Validate required fields
	if solution.SolutionName == "" || solution.Summary == "" || len(solution.RecommendedFeatures) == 0 || len(solution.RecommendedTechnology) == 0 || solution.Complexity == "" {
		s.logger.Error("AI solution response missing required fields", "solution", solution)
		return model.GenerateSolutionResponse{}, apperror.AIProvider("The AI provider returned an incomplete response", nil)
	}

	return model.GenerateSolutionResponse{
		Solution: solution,
		Model:    resp.Model,
	}, nil
}

// IngestDocument converts raw document content into text embeddings using Gemini
// (model text-embedding-004) and stores the company_id, content, and embedding vector
// in the company_documents table.
func (s *service) IngestDocument(ctx context.Context, req model.DocumentUploadRequest) error {
	if s.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	// 1. Convert req.Content into float32 array (vector) using Gemini text-embedding-004
	embedding, err := s.provider.Embed(ctx, "text-embedding-004", req.Content)
	if err != nil {
		s.logger.Error("failed to generate embedding for document", "company_id", req.CompanyID, "error", err)
		return err
	}

	// 2. Format vector as PostgreSQL vector string representation: [val1,val2,...]
	vecStr := formatVector(embedding)

	// 3. Save company_id, content, and vector to company_documents table using pgxpool
	docID := uuid.New().String()
	const q = `
		INSERT INTO company_documents (id, company_id, content, embedding, created_at)
		VALUES ($1, $2, $3, $4::vector, NOW())`

	if _, err := s.pool.Exec(ctx, q, docID, req.CompanyID, req.Content, vecStr); err != nil {
		s.logger.Error("failed to insert company document into database", "company_id", req.CompanyID, "error", err)
		return apperror.Database("Failed to save document to database", err)
	}

	s.logger.Info("document ingested and embedded successfully", "company_id", req.CompanyID, "doc_id", docID)
	return nil
}

// GenerateCompanyProfile queries raw documents for the given CompanyID from Supabase,
// combines them as [CONTEXT] into the system prompt, and calls the LLM to generate
// a structured company profile JSON.
func (s *service) GenerateCompanyProfile(ctx context.Context, req model.GenerateCompanyProfileRequest) (*model.GeneratedCompanyProfile, error) {
	if s.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	// 1. Query raw documents for CompanyID from Supabase
	const q = `
		SELECT content
		FROM company_documents
		WHERE company_id = $1
		ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, q, req.CompanyID)
	if err != nil {
		s.logger.Error("failed to query company documents", "company_id", req.CompanyID, "error", err)
		return nil, apperror.Database("Failed to retrieve company documents", err)
	}
	defer rows.Close()

	var contents []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			s.logger.Error("failed to scan company document content", "error", err)
			return nil, apperror.Database("Failed to read company document", err)
		}
		contents = append(contents, content)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("row iteration error for company documents", "error", err)
		return nil, apperror.Database("Failed to retrieve company documents", err)
	}

	if len(contents) == 0 {
		return nil, apperror.NotFound("No documents found for the specified company ID", nil)
	}

	// 2. Combine retrieved document content as [CONTEXT]
	combinedContext := strings.Join(contents, "\n\n---\n\n")

	// 3. Construct System Prompt with [CONTEXT]
	systemPrompt := fmt.Sprintf(`Anda adalah AI copywriter profesional. Ekstrak informasi dari [CONTEXT] berikut ke dalam format JSON yang telah ditentukan. Anda HANYA boleh menggunakan fakta dari [CONTEXT]. Jika informasi tidak ada di dalam konteks, berikan nilai string kosong atau array kosong []. Jangan pernah berhalusinasi.

[CONTEXT]
%s

Format JSON yang telah ditentukan:
{
  "name": "string",
  "tagline": "string",
  "about_us": "string",
  "vision": "string",
  "mission": ["string"],
  "core_values": ["string"],
  "services": ["string"],
  "target_audience": "string"
}

Aturan:
- Output HANYA objek JSON valid sesuai format di atas.
- JANGAN sertakan markdown block (tanpa `+"```json"+` atau `+"```"+`).
- JANGAN sertakan penjelasan, teks pembuka, atau teks penutup apa pun.`, combinedContext)

	userPrompt := fmt.Sprintf("Ekstrak dan hasilkan profil perusahaan untuk company_id %q berdasarkan fakta dari [CONTEXT].", req.CompanyID)

	// 4. Use existing LLM execution method to return JSON data
	resp, err := s.provider.Complete(ctx, provider.Request{
		Model: s.cfg.Model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		s.logger.Error("failed to complete company profile generation", "error", err)
		return nil, err
	}

	// 5. Clean potential markdown formatting
	cleaned := strings.TrimSpace(resp.Content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var profile model.GeneratedCompanyProfile
	if err := json.Unmarshal([]byte(cleaned), &profile); err != nil {
		s.logger.Error("failed to parse AI company profile response", "error", err, "raw", resp.Content)
		return nil, apperror.AIProvider("The AI provider returned an invalid response format", err)
	}

	// Ensure slice fields are non-nil so JSON serialization outputs [] instead of null
	if profile.Mission == nil {
		profile.Mission = []string{}
	}
	if profile.CoreValues == nil {
		profile.CoreValues = []string{}
	}
	if profile.Services == nil {
		profile.Services = []string{}
	}

	return &profile, nil
}

// formatVector formats a float32 slice into PostgreSQL vector syntax [v1,v2,v3,...].
func formatVector(values []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range values {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
