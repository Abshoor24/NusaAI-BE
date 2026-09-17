package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/ai/rag"
	"github.com/Abshoor24/backend-go/internal/modules/ai/repository"
)

const (
	defaultContextWindow = 20
	maxTitleLength       = 40
	ragTopK              = 4
	embeddingModel       = "text-embedding-004"

	// defaultSystemPrompt is the generic system instruction sent with every
	// request unless the caller overrides it via ChatRequest.System.
	// Keep this neutral — no domain bias, no company/business framing.
	// It should work equally well for coding, writing, analysis, research,
	// debugging, brainstorming, summarization, reasoning, and Q&A.
	defaultSystemPrompt = `You are a helpful, accurate, and thoughtful AI assistant.

Your goal is to understand what the user is asking and give the most useful response.

Guidelines:
- Be clear and concise. Avoid unnecessary filler.
- Match the level of detail to what the user actually needs.
- When writing code: produce correct, idiomatic code; explain key decisions; never fabricate APIs or libraries.
- When explaining concepts: adapt to the apparent knowledge level of the user.
- When analyzing text or data: be systematic and highlight what matters most.
- When brainstorming or writing: be creative but stay on topic.
- When you are uncertain about facts: say so clearly rather than guessing.
- Do not assume any particular industry, domain, or use case unless the user specifies one.`
)

// Service defines the core AI application business logic.
type Service interface {
	Chat(ctx context.Context, userID string, req model.ChatRequest) (model.ChatResponse, error)
	ChatStream(ctx context.Context, userID string, req model.ChatRequest) (<-chan provider.StreamChunk, string, string, error)
	ListConversations(ctx context.Context, userID string) ([]model.Conversation, error)
	GetConversation(ctx context.Context, conversationID, userID string) (*model.ConversationDetail, error)
	DeleteConversation(ctx context.Context, conversationID, userID string) error

	// Document and RAG operations
	IngestDocument(ctx context.Context, userID string, req model.IngestDocumentRequest) (*model.IngestDocumentResponse, error)
	ListDocuments(ctx context.Context, userID string) ([]model.DocumentDetail, error)
	GetDocument(ctx context.Context, id, userID string) (*model.Document, error)
	DeleteDocument(ctx context.Context, id, userID string) error
}

type service struct {
	provider provider.Provider
	cfg      config.AIConfig
	logger   *slog.Logger
	repo     repository.Repository
	chunker  *rag.Chunker
}

// New creates a new instance of the AI Service with injected dependencies.
func New(provider provider.Provider, cfg config.AIConfig, logger *slog.Logger, repo repository.Repository) Service {
	return &service{
		provider: provider,
		cfg:      cfg,
		logger:   logger,
		repo:     repo,
		chunker:  rag.NewChunker(800, 150),
	}
}

// Chat handles sending a prompt to the general-purpose AI assistant.
// It creates a new conversation or appends to an existing conversation,
// retrieves recent messages within the context window, sends them to Gemini,
// persists both user prompt and assistant response, and returns the response.
// If req.UseRAG is true, relevant document chunks are retrieved and injected into the prompt.
func (s *service) Chat(ctx context.Context, userID string, req model.ChatRequest) (model.ChatResponse, error) {
	if strings.TrimSpace(userID) == "" {
		return model.ChatResponse{}, apperror.Unauthorized("User authentication is required", nil)
	}

	messageText := strings.TrimSpace(req.Message)
	if messageText == "" {
		return model.ChatResponse{}, apperror.Validation("Message is required")
	}

	var convID string
	if strings.TrimSpace(req.ConversationID) == "" {
		// Create a new conversation thread
		title := generateTitle(messageText)
		conv := &model.Conversation{
			ID:     uuid.New().String(),
			UserID: userID,
			Title:  title,
		}
		if err := s.repo.CreateConversation(ctx, conv); err != nil {
			s.logger.Error("failed to create conversation", "user_id", userID, "error", err)
			return model.ChatResponse{}, err
		}
		convID = conv.ID
	} else {
		// Verify conversation ownership
		conv, err := s.repo.GetConversation(ctx, req.ConversationID, userID)
		if err != nil {
			s.logger.Error("failed to get conversation", "conversation_id", req.ConversationID, "user_id", userID, "error", err)
			return model.ChatResponse{}, err
		}
		if conv == nil {
			return model.ChatResponse{}, apperror.NotFound("Conversation not found", nil)
		}
		convID = conv.ID
	}

	// 1. Save user message to database
	userMsg := &model.Message{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Role:           "user",
		Content:        messageText,
	}
	if err := s.repo.CreateMessage(ctx, userMsg); err != nil {
		s.logger.Error("failed to save user message", "conversation_id", convID, "error", err)
		return model.ChatResponse{}, err
	}

	// 2. Fetch context window (recent messages in chronological order)
	recentMessages, err := s.repo.GetRecentMessages(ctx, convID, defaultContextWindow)
	if err != nil {
		s.logger.Error("failed to fetch recent messages", "conversation_id", convID, "error", err)
		return model.ChatResponse{}, err
	}

	// 3. Optional RAG retrieval
	var retrievedSources []model.ChunkSearchResult
	ragContext := ""
	if req.UseRAG {
		sources, rCtx, ragErr := s.retrieveContext(ctx, userID, messageText, req.DocumentID)
		if ragErr != nil {
			// Log error but do not fail chat completely — fall back to standard generation
			s.logger.Warn("rag retrieval failed, proceeding without document context", "error", ragErr)
		} else {
			retrievedSources = sources
			ragContext = rCtx
		}
	}

	// 4. Build system instruction and message payload.
	systemPrompt := defaultSystemPrompt
	if strings.TrimSpace(req.System) != "" {
		systemPrompt = req.System
	}
	if ragContext != "" {
		systemPrompt = systemPrompt + "\n\n" + ragContext
	}

	var msgs []provider.ChatMessage
	msgs = append(msgs, provider.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	for _, m := range recentMessages {
		msgs = append(msgs, provider.ChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// 5. Request completion from provider (single call — no classifier needed).
	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:    s.cfg.Model,
		Messages: msgs,
	})
	if err != nil {
		s.logger.Error("ai provider completion failed", "conversation_id", convID, "error", err)
		return model.ChatResponse{}, err
	}

	// 6. Save assistant response to database
	assistantMsg := &model.Message{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Role:           "assistant",
		Content:        resp.Content,
	}
	if err := s.repo.CreateMessage(ctx, assistantMsg); err != nil {
		s.logger.Error("failed to save assistant message", "conversation_id", convID, "error", err)
		return model.ChatResponse{}, err
	}

	// 7. Update conversation timestamp
	_ = s.repo.TouchConversation(ctx, convID, userID)

	return model.ChatResponse{
		ConversationID: convID,
		Message: model.ChatMessageItem{
			ID:      assistantMsg.ID,
			Role:    assistantMsg.Role,
			Content: assistantMsg.Content,
		},
		Sources: retrievedSources,
	}, nil
}

// ChatStream is the streaming counterpart of Chat.
// It orchestrates conversation/message creation, starts a Gemini stream, and
// returns:
//   - ch      — a channel of StreamChunk values (tokens, done, or error)
//   - convID  — the conversation ID (new or existing)
//   - msgID   — the UUID pre-allocated for the assistant message
//   - err     — non-nil if the stream could not be started at all
func (s *service) ChatStream(ctx context.Context, userID string, req model.ChatRequest) (<-chan provider.StreamChunk, string, string, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, "", "", apperror.Unauthorized("User authentication is required", nil)
	}

	messageText := strings.TrimSpace(req.Message)
	if messageText == "" {
		return nil, "", "", apperror.Validation("Message is required")
	}

	var convID string
	if strings.TrimSpace(req.ConversationID) == "" {
		title := generateTitle(messageText)
		conv := &model.Conversation{
			ID:     uuid.New().String(),
			UserID: userID,
			Title:  title,
		}
		if err := s.repo.CreateConversation(ctx, conv); err != nil {
			s.logger.Error("stream: failed to create conversation", "user_id", userID, "error", err)
			return nil, "", "", err
		}
		convID = conv.ID
	} else {
		conv, err := s.repo.GetConversation(ctx, req.ConversationID, userID)
		if err != nil {
			s.logger.Error("stream: failed to get conversation", "conversation_id", req.ConversationID, "user_id", userID, "error", err)
			return nil, "", "", err
		}
		if conv == nil {
			return nil, "", "", apperror.NotFound("Conversation not found", nil)
		}
		convID = conv.ID
	}

	// Save user message before starting the stream so it is always persisted
	// regardless of what happens during streaming.
	userMsg := &model.Message{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Role:           "user",
		Content:        messageText,
	}
	if err := s.repo.CreateMessage(ctx, userMsg); err != nil {
		s.logger.Error("stream: failed to save user message", "conversation_id", convID, "error", err)
		return nil, "", "", err
	}

	// Build message history for context window.
	recentMessages, err := s.repo.GetRecentMessages(ctx, convID, defaultContextWindow)
	if err != nil {
		s.logger.Error("stream: failed to fetch recent messages", "conversation_id", convID, "error", err)
		return nil, "", "", err
	}

	// Optional RAG retrieval
	ragContext := ""
	if req.UseRAG {
		_, rCtx, ragErr := s.retrieveContext(ctx, userID, messageText, req.DocumentID)
		if ragErr != nil {
			s.logger.Warn("stream: rag retrieval failed, proceeding without document context", "error", ragErr)
		} else {
			ragContext = rCtx
		}
	}

	// Build system instruction — same shared prompt as Chat().
	systemPrompt := defaultSystemPrompt
	if strings.TrimSpace(req.System) != "" {
		systemPrompt = req.System
	}
	if ragContext != "" {
		systemPrompt = systemPrompt + "\n\n" + ragContext
	}

	var msgs []provider.ChatMessage
	msgs = append(msgs, provider.ChatMessage{Role: "system", Content: systemPrompt})
	for _, m := range recentMessages {
		msgs = append(msgs, provider.ChatMessage{Role: m.Role, Content: m.Content})
	}

	// Start the provider stream. The HTTP request context drives cancellation.
	providerCh, err := s.provider.CompleteStream(ctx, provider.Request{
		Model:    s.cfg.Model,
		Messages: msgs,
	})
	if err != nil {
		s.logger.Error("stream: provider stream start failed", "conversation_id", convID, "error", err)
		return nil, "", "", err
	}

	// Pre-allocate assistant message ID so the handler can return it to the
	// client immediately, before streaming is complete.
	assistantMsgID := uuid.New().String()

	// dbCtx is intentionally detached from the HTTP request context.
	// This ensures DB writes (save assistant message, touch conversation)
	// complete even if the HTTP client disconnects mid-stream.
	dbCtx := context.WithoutCancel(ctx)

	outCh := make(chan provider.StreamChunk, 32)
	go func() {
		defer close(outCh)

		var collected strings.Builder
		clientGone := false

		for chunk := range providerCh {
			if chunk.Err != nil {
				s.logger.Error("stream: provider chunk error", "conversation_id", convID, "error", chunk.Err)
				if !clientGone {
					select {
					case outCh <- chunk:
					case <-ctx.Done():
					}
				}
				// Do not persist partial content on provider error.
				return
			}

			if chunk.Token != "" {
				collected.WriteString(chunk.Token)
			}

			// Forward token to handler only while client is still connected.
			if !clientGone {
				select {
				case outCh <- chunk:
				case <-ctx.Done():
					clientGone = true
				}
			}

			if chunk.Done {
				// Stream completed (success path). Persist full response using
				// dbCtx so this is not affected by client disconnect.
				content := collected.String()
				assistantMsg := &model.Message{
					ID:             assistantMsgID,
					ConversationID: convID,
					Role:           "assistant",
					Content:        content,
				}
				if saveErr := s.repo.CreateMessage(dbCtx, assistantMsg); saveErr != nil {
					s.logger.Error("stream: failed to save assistant message", "conversation_id", convID, "error", saveErr)
				}
				_ = s.repo.TouchConversation(dbCtx, convID, userID)
				return
			}
		}

		// If the stream ended without an explicit Done chunk (e.g. client disconnect / context cancellation
		// or network interruption), but we collected content, persist whatever was accumulated.
		if collected.Len() > 0 {
			content := collected.String()
			assistantMsg := &model.Message{
				ID:             assistantMsgID,
				ConversationID: convID,
				Role:           "assistant",
				Content:        content,
			}
			if saveErr := s.repo.CreateMessage(dbCtx, assistantMsg); saveErr != nil {
				s.logger.Error("stream: failed to save partial assistant message on interruption", "conversation_id", convID, "error", saveErr)
			}
			_ = s.repo.TouchConversation(dbCtx, convID, userID)
		}
	}()

	return outCh, convID, assistantMsgID, nil
}

// retrieveContext generates an embedding for query and searches the user's documents.
func (s *service) retrieveContext(ctx context.Context, userID, query, documentID string) ([]model.ChunkSearchResult, string, error) {
	emb, err := s.provider.Embed(ctx, embeddingModel, query)
	if err != nil {
		return nil, "", fmt.Errorf("generate query embedding: %w", err)
	}

	chunks, err := s.repo.SearchSimilarChunks(ctx, emb, userID, documentID, ragTopK)
	if err != nil {
		return nil, "", fmt.Errorf("search similar chunks: %w", err)
	}

	if len(chunks) == 0 {
		return nil, "", nil
	}

	var sb strings.Builder
	sb.WriteString("=== REFERENCE KNOWLEDGE FROM USER DOCUMENTS ===\n")
	sb.WriteString("Use the following reference knowledge to inform your answer when relevant:\n\n")
	for i, c := range chunks {
		fmt.Fprintf(&sb, "[Excerpt %d]\n%s\n\n", i+1, c.Content)
	}
	sb.WriteString("=== END REFERENCE KNOWLEDGE ===\n")
	sb.WriteString("Instruction: If the answer is present in the reference knowledge, prioritize it. If not found, use your general knowledge and clearly clarify that.")

	return chunks, sb.String(), nil
}

// IngestDocument segments a text document into chunks, generates vector embeddings for each chunk,
// and saves the document along with its chunks to PostgreSQL.
func (s *service) IngestDocument(ctx context.Context, userID string, req model.IngestDocumentRequest) (*model.IngestDocumentResponse, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, apperror.Validation("Document title is required")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, apperror.Validation("Document content is required")
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "text"
	}

	doc := model.Document{
		ID:     uuid.New().String(),
		UserID: userID,
		Title:  title,
		Source: source,
	}

	if err := s.repo.CreateDocument(ctx, &doc); err != nil {
		s.logger.Error("failed to create document record", "user_id", userID, "error", err)
		return nil, err
	}

	// Segment text into overlapping chunks
	textChunks := s.chunker.Chunk(content)
	if len(textChunks) == 0 {
		textChunks = []string{content}
	}

	docChunks := make([]model.DocumentChunk, 0, len(textChunks))
	for i, chunkText := range textChunks {
		emb, err := s.provider.Embed(ctx, embeddingModel, chunkText)
		if err != nil {
			s.logger.Error("failed to generate chunk embedding", "chunk_index", i, "error", err)
			return nil, err
		}

		docChunks = append(docChunks, model.DocumentChunk{
			ID:         uuid.New().String(),
			DocumentID: doc.ID,
			ChunkIndex: i,
			Content:    chunkText,
			Embedding:  emb,
		})
	}

	if err := s.repo.CreateDocumentChunks(ctx, docChunks); err != nil {
		s.logger.Error("failed to save document chunks", "document_id", doc.ID, "error", err)
		return nil, err
	}

	return &model.IngestDocumentResponse{
		Document:   doc,
		ChunkCount: len(docChunks),
	}, nil
}

func (s *service) ListDocuments(ctx context.Context, userID string) ([]model.DocumentDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	return s.repo.ListDocuments(ctx, userID)
}

func (s *service) GetDocument(ctx context.Context, id, userID string) (*model.Document, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	doc, err := s.repo.GetDocument(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, apperror.NotFound("Document not found", nil)
	}
	return doc, nil
}

func (s *service) DeleteDocument(ctx context.Context, id, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperror.Unauthorized("User authentication is required", nil)
	}
	return s.repo.DeleteDocument(ctx, id, userID)
}

// ListConversations returns all conversation threads owned by the given user,
// ordered by most recently updated first.
func (s *service) ListConversations(ctx context.Context, userID string) ([]model.Conversation, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	return s.repo.ListConversations(ctx, userID)
}

// GetConversation returns details of a single conversation with its messages.
func (s *service) GetConversation(ctx context.Context, conversationID, userID string) (*model.ConversationDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperror.Unauthorized("User authentication is required", nil)
	}
	conv, err := s.repo.GetConversation(ctx, conversationID, userID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperror.NotFound("Conversation not found", nil)
	}

	messages, err := s.repo.GetMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	return &model.ConversationDetail{
		Conversation: *conv,
		Messages:     messages,
	}, nil
}

// DeleteConversation deletes a conversation belonging to the user.
func (s *service) DeleteConversation(ctx context.Context, conversationID, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperror.Unauthorized("User authentication is required", nil)
	}
	return s.repo.DeleteConversation(ctx, conversationID, userID)
}

func generateTitle(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxTitleLength {
		return msg
	}
	return msg[:maxTitleLength] + "..."
}
