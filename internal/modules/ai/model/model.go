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

// MessageAttachment represents an image or media file attached to a chat message.
type MessageAttachment struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id"`
	FileName    string    `json:"file_name"`
	FileType    string    `json:"file_type"`
	FileSize    int64     `json:"file_size"`
	StoragePath string    `json:"-"` // internal storage path, omitted from JSON
	MimeType    string    `json:"mime_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// Message represents an individual turn (user, assistant, system) in a conversation.
type Message struct {
	ID             string              `json:"id"`
	ConversationID string              `json:"conversation_id"`
	Role           string              `json:"role"`
	Content        string              `json:"content"`
	Attachments    []MessageAttachment `json:"attachments,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}

// ConversationDetail represents a conversation with its full message history.
type ConversationDetail struct {
	Conversation
	Messages []Message `json:"messages"`
}

// Document represents a knowledge base document ingested by a user for RAG.
type Document struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Title        string    `json:"title"`
	Source       string    `json:"source"`
	FileName     string    `json:"file_name,omitempty"`
	FileType     string    `json:"file_type,omitempty"`
	FileSize     int64     `json:"file_size,omitempty"`
	FilePath     string    `json:"-"` // internal storage path, omitted from JSON
	Status       string    `json:"status"` // "processing", "ready", "failed"
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// DocumentChunk represents a segmented portion of a document with its vector embedding.
type DocumentChunk struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	ChunkIndex int       `json:"chunk_index"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}

// DocumentDetail represents a document along with metadata such as total chunks.
type DocumentDetail struct {
	Document
	ChunkCount int `json:"chunk_count"`
}

// DocumentStatusResponse represents the status of document background processing.
type DocumentStatusResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"` // "processing", "ready", "failed"
	ChunkCount   int    `json:"chunk_count"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// IngestDocumentRequest is the input payload for POST /api/v1/ai/documents.
type IngestDocumentRequest struct {
	Title   string `json:"title" validate:"required,min=1,max=255"`
	Content string `json:"content" validate:"required,min=5,max=500000"`
	Source  string `json:"source,omitempty"`
}

// IngestDocumentResponse is the response payload for POST /api/v1/ai/documents.
type IngestDocumentResponse struct {
	Document   Document `json:"document"`
	ChunkCount int      `json:"chunk_count"`
}


// ChunkSearchResult represents a chunk returned by vector similarity search.
type ChunkSearchResult struct {
	ID         string  `json:"id"`
	DocumentID string  `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
	Similarity float64 `json:"similarity"`
}

// ImageUploadPayload represents an image attached to a chat request.
type ImageUploadPayload struct {
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type"`
	Data     []byte `json:"data"` // binary data
}

// ChatRequest is the input payload for POST /api/v1/ai/chat and POST /api/v1/ai/chat/stream.
// If ConversationID is omitted or empty, a new conversation is automatically created.
// If UseRAG is true, relevant document chunks will be retrieved and injected into the context.
type ChatRequest struct {
	ConversationID string               `json:"conversation_id,omitempty"`
	Message        string               `json:"message" validate:"required,max=10000"`
	System         string               `json:"system,omitempty"`          // Optional custom system prompt override
	UseRAG         bool                 `json:"use_rag,omitempty"`         // Optional: enable RAG knowledge retrieval
	DocumentID     string               `json:"document_id,omitempty"`     // Optional: restrict retrieval to a specific document
	Images         []ImageUploadPayload `json:"images,omitempty"`          // Optional: multimodal image attachments
}

// ChatMessageItem represents the generated message returned in ChatResponse.
type ChatMessageItem struct {
	ID          string              `json:"id"`
	Role        string              `json:"role"`
	Content     string              `json:"content"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

// ChatResponse is the response payload for POST /api/v1/ai/chat.
type ChatResponse struct {
	ConversationID string              `json:"conversation_id"`
	Message        ChatMessageItem     `json:"message"`
	Sources        []ChunkSearchResult `json:"sources,omitempty"` // Included if RAG was used
}
