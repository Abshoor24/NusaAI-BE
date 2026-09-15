package model

// GenerateTextRequest is the input contract for the AI service abstraction.
// Prompt is the user's instruction/text. Context is optional supplementary
// material (documents, FAQ, search results) the model can use to answer. Its
// source is deliberately open so any business use case can feed it later. No
// company-specific prompt is assumed.
type GenerateTextRequest struct {
	Prompt  string   `json:"prompt" validate:"required"`
	Context []string `json:"context,omitempty"`
}

// GenerateTextResponse is the output contract of the AI service abstraction.
type GenerateTextResponse struct {
	Content string `json:"content"`
	Model   string `json:"model,omitempty"`
}

// ChatMessage represents a single message in a chatbot conversation history.
type ChatMessage struct {
	Role    string `json:"role" validate:"required,oneof=system user assistant"`
	Content string `json:"content" validate:"required"`
}

// ChatRequest is the input contract for the chatbot endpoint.
type ChatRequest struct {
	Message         string           `json:"message" validate:"required"`
	History         []ChatMessage    `json:"history,omitempty"`
	System          string           `json:"system,omitempty"`           // Optional custom system prompt override
	SolutionContext *SolutionContext `json:"solution_context,omitempty"` // Optional generated solution context
}

// ChatResponse is the output contract of the chatbot endpoint.
type ChatResponse struct {
	Response string `json:"response"`
	Model    string `json:"model,omitempty"`
}

// GenerateSolutionRequest is the input for AI Business Solution Generator.
type GenerateSolutionRequest struct {
	BusinessType    string `json:"business_type" validate:"required"`
	BusinessProblem string `json:"business_problem" validate:"required"`
	BusinessGoal    string `json:"business_goal" validate:"required"`
}

// GeneratedSolution is the structured output from AI Business Solution Generator.
type GeneratedSolution struct {
	SolutionName          string   `json:"solution_name"`
	Summary               string   `json:"summary"`
	RecommendedFeatures   []string `json:"recommended_features"`
	RecommendedTechnology []string `json:"recommended_technology"`
	Complexity            string   `json:"complexity"`
}

// GenerateSolutionResponse wraps the generated solution for API response.
type GenerateSolutionResponse struct {
	Solution GeneratedSolution `json:"solution"`
	Model    string            `json:"model,omitempty"`
}

// SolutionContext carries the generated solution context for chatbot discussion.
type SolutionContext struct {
	BusinessType    string            `json:"business_type"`
	BusinessProblem string            `json:"business_problem"`
	BusinessGoal    string            `json:"business_goal"`
	Solution        GeneratedSolution `json:"solution"`
}

// DocumentUploadRequest represents the payload for uploading raw company documents for RAG.
type DocumentUploadRequest struct {
	CompanyID string `json:"company_id" validate:"required"`
	Content   string `json:"content" validate:"required"`
}

// GenerateCompanyProfileRequest is the input for generating a company profile via RAG.
type GenerateCompanyProfileRequest struct {
	CompanyID string `json:"company_id" validate:"required"`
}

// GeneratedCompanyProfile is the structured output from AI Company Profile Generator.
type GeneratedCompanyProfile struct {
	Name           string   `json:"name"`
	Tagline        string   `json:"tagline"`
	AboutUs        string   `json:"about_us"`
	Vision         string   `json:"vision"`
	Mission        []string `json:"mission"`
	CoreValues     []string `json:"core_values"`
	Services       []string `json:"services"`
	TargetAudience string   `json:"target_audience"`
}
