package provider

import "context"

// Provider is the transport abstraction for calling an LLM. Implementations
// can be swapped (OpenAI-compatible, Anthropic, Gemini, local model, ...)
// without changing the service layer.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
	CompleteStream(ctx context.Context, req Request) (<-chan StreamChunk, error)
	Embed(ctx context.Context, model string, text string) ([]float32, error)
}

// ChatMessage represents a single chat message for multi-turn conversation.
type ChatMessage struct {
	Role    string
	Content string
}

// Request is the provider-neutral request payload.
// Context is optional supplementary material (documents, FAQ, search results)
// the model can use to answer. Its assembly is a CLIENT REQUIREMENT.
type Request struct {
	Model    string
	Prompt   string
	Context  []string
	Messages []ChatMessage // Optional structured messages for multi-turn chat
}

// Response is the provider-neutral response payload.
type Response struct {
	Content string
	Model   string
}

// StreamChunk is one incremental piece delivered by CompleteStream.
// If Err is non-nil the stream has failed and no further chunks will arrive.
// Done == true signals the end of the stream (Err will be nil in that case).
type StreamChunk struct {
	Token string
	Done  bool
	Err   error
}
