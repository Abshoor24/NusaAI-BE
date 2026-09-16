package model

import "time"

// Conversation represents a chat session/thread owned by a user.
type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message represents an individual turn (user, assistant, system) in a conversation.
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConversationDetail represents a conversation with its full message history.
type ConversationDetail struct {
	Conversation
	Messages []Message `json:"messages"`
}

// ChatRequest is the input payload for POST /api/v1/ai/chat.
// If ConversationID is omitted or empty, a new conversation is automatically created.
type ChatRequest struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Message        string `json:"message" validate:"required,max=10000"`
	System         string `json:"system,omitempty"` // Optional custom system prompt override
}

// ChatMessageItem represents the generated message returned in ChatResponse.
type ChatMessageItem struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse is the response payload for POST /api/v1/ai/chat.
type ChatResponse struct {
	ConversationID string          `json:"conversation_id"`
	Message        ChatMessageItem `json:"message"`
}
