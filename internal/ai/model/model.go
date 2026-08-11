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
