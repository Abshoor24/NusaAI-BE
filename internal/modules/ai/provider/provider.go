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

// ImageAttachment represents an image passed to the AI provider.
type ImageAttachment struct {
	MimeType string
	Data     []byte
}

// ChatMessage represents a single chat message for multi-turn conversation.
type ChatMessage struct {
	Role    string
	Content string
	Images  []ImageAttachment
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
// FinishReason carries the provider's terminal reason on the Done chunk
// (e.g. "STOP", "MAX_TOKENS" for Gemini); empty when unknown.
// BlockReason carries a safety block reason when the provider refused
// content (Gemini promptFeedback.blockReason); empty when not blocked.
// Usage carries token accounting when the provider reports it.
type StreamChunk struct {
	Token        string
	Done         bool
	Err          error
	FinishReason string
	BlockReason  string
	Usage        *UsageMetadata
}

// UsageMetadata is provider-neutral token accounting for one streamed
// response. Fields stay zero when the provider does not report them.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}
