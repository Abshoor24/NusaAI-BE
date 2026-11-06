package provider

import "context"

// Provider is the transport abstraction for calling an LLM. Implementations
// can be swapped (OpenAI-compatible, Anthropic, Gemini, local model, ...)
// without changing the service layer.
type Provider interface {
	CompleteStream(ctx context.Context, req Request) (<-chan StreamChunk, error)
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

// Request is the provider-neutral request payload. Messages carries the
// system/user turns (and optional images) sent to the provider.
type Request struct {
	Model    string
	Messages []ChatMessage
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
