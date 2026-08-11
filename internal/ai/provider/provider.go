package ai

import "context"

// Provider is the transport abstraction for calling an LLM. Implementations
// can be swapped (OpenAI-compatible, Anthropic, Gemini, local model, ...)
// without changing the service layer.
type Provider interface {
	Complete(ctx context.Context, req providerRequest) (providerResponse, error)
}

// providerRequest is the provider-neutral request payload.
// Context is optional supplementary material (documents, FAQ, search results)
// the model can use to answer. Its assembly is a CLIENT REQUIREMENT.
type providerRequest struct {
	Model   string
	Message string
	Context []string
}

// providerResponse is the provider-neutral response payload.
type providerResponse struct {
	Message string
	Model   string
}
