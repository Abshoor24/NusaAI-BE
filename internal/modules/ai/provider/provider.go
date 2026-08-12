package provider

import "context"

// Provider is the transport abstraction for calling an LLM. Implementations
// can be swapped (OpenAI-compatible, Anthropic, Gemini, local model, ...)
// without changing the service layer.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Request is the provider-neutral request payload.
// Context is optional supplementary material (documents, FAQ, search results)
// the model can use to answer. Its assembly is a CLIENT REQUIREMENT.
type Request struct {
	Model   string
	Prompt  string
	Context []string
}

// Response is the provider-neutral response payload.
type Response struct {
	Content string
	Model   string
}
