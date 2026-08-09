package model

// AIRequest is the input contract for the AI service abstraction.
// Context holds additional information the AI may use to answer. Its source
// is deliberately open: database content, documents, FAQ or vector search
// results can all be passed later. No company-specific prompt is assumed.
type AIRequest struct {
	Message string   `json:"message" validate:"required"`
	Context []string `json:"context"`
}

// AIResponse is the output contract of the AI service abstraction.
type AIResponse struct {
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
}
