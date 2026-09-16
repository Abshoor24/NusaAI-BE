package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/ai/repository"
)

const (
	defaultContextWindow = 20
	maxTitleLength       = 40
)

// Service defines the core AI application business logic.
type Service interface {
	Chat(ctx context.Context, userID string, req model.ChatRequest) (model.ChatResponse, error)
	ListConversations(ctx context.Context, userID string) ([]model.Conversation, error)
	GetConversation(ctx context.Context, conversationID, userID string) (*model.ConversationDetail, error)
	DeleteConversation(ctx context.Context, conversationID, userID string) error
}

type service struct {
	provider provider.Provider
	cfg      config.AIConfig
	logger   *slog.Logger
	repo     repository.Repository
}

// New creates a new instance of the AI Service with injected dependencies.
func New(provider provider.Provider, cfg config.AIConfig, logger *slog.Logger, repo repository.Repository) Service {
	return &service{
		provider: provider,
		cfg:      cfg,
		logger:   logger,
		repo:     repo,
	}
}

// Chat handles sending a prompt to the general-purpose AI assistant.
// It creates a new conversation or appends to an existing conversation,
// retrieves recent messages within the context window, sends them to Gemini,
// persists both user prompt and assistant response, and returns the response.
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

	// 3. Build system instruction and message payload
	systemPrompt := `You are a helpful general-purpose AI assistant.

Understand the user's request and provide a useful, accurate response.

When the user asks for code:
- provide correct, idiomatic code
- explain important parts when useful
- do not fabricate APIs or libraries

When the user asks for explanations:
- explain clearly and concisely
- adapt the explanation to the user's question

When information is uncertain:
- do not fabricate facts
- clearly communicate uncertainty`

	if strings.TrimSpace(req.System) != "" {
		systemPrompt = req.System
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

	// 4. Request completion from Gemini provider (single call, no classifier)
	resp, err := s.provider.Complete(ctx, provider.Request{
		Model:    s.cfg.Model,
		Messages: msgs,
	})
	if err != nil {
		s.logger.Error("ai provider completion failed", "conversation_id", convID, "error", err)
		return model.ChatResponse{}, err
	}

	// 5. Save assistant response to database
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

	// 6. Update conversation timestamp
	_ = s.repo.TouchConversation(ctx, convID, userID)

	return model.ChatResponse{
		ConversationID: convID,
		Message: model.ChatMessageItem{
			ID:      assistantMsg.ID,
			Role:    assistantMsg.Role,
			Content: assistantMsg.Content,
		},
	}, nil
}

// ListConversations returns all conversations belonging to the authenticated user.
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
