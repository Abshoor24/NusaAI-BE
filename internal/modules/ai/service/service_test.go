package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/ai/repository"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type mockProvider struct {
	content string
	model   string
	err     error
	lastReq provider.Request
}

func (m *mockProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	m.lastReq = req
	if m.err != nil {
		return provider.Response{}, m.err
	}
	content := m.content
	if content == "" {
		content = "mock response"
	}
	modelName := m.model
	if modelName == "" {
		modelName = "mock-model"
	}
	return provider.Response{Content: content, Model: modelName}, nil
}

func (m *mockProvider) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

type inMemoryRepository struct {
	conversations map[string]*model.Conversation
	messages      map[string][]*model.Message
}

func newInMemoryRepo() *inMemoryRepository {
	return &inMemoryRepository{
		conversations: make(map[string]*model.Conversation),
		messages:      make(map[string][]*model.Message),
	}
}

func (r *inMemoryRepository) CreateConversation(_ context.Context, conv *model.Conversation) error {
	conv.CreatedAt = time.Now()
	conv.UpdatedAt = time.Now()
	r.conversations[conv.ID] = conv
	return nil
}

func (r *inMemoryRepository) GetConversation(_ context.Context, id, userID string) (*model.Conversation, error) {
	conv, ok := r.conversations[id]
	if !ok || conv.UserID != userID {
		return nil, nil
	}
	return conv, nil
}

func (r *inMemoryRepository) ListConversations(_ context.Context, userID string) ([]model.Conversation, error) {
	var list []model.Conversation
	for _, conv := range r.conversations {
		if conv.UserID == userID {
			list = append(list, *conv)
		}
	}
	return list, nil
}

func (r *inMemoryRepository) DeleteConversation(_ context.Context, id, userID string) error {
	conv, ok := r.conversations[id]
	if !ok || conv.UserID != userID {
		return apperror.NotFound("Conversation not found", nil)
	}
	delete(r.conversations, id)
	delete(r.messages, id)
	return nil
}

func (r *inMemoryRepository) TouchConversation(_ context.Context, id, userID string) error {
	if conv, ok := r.conversations[id]; ok && conv.UserID == userID {
		conv.UpdatedAt = time.Now()
	}
	return nil
}

func (r *inMemoryRepository) CreateMessage(_ context.Context, msg *model.Message) error {
	msg.CreatedAt = time.Now()
	r.messages[msg.ConversationID] = append(r.messages[msg.ConversationID], msg)
	return nil
}

func (r *inMemoryRepository) GetRecentMessages(_ context.Context, conversationID string, limit int) ([]model.Message, error) {
	msgs := r.messages[conversationID]
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	res := make([]model.Message, len(msgs))
	for i, m := range msgs {
		res[i] = *m
	}
	return res, nil
}

func (r *inMemoryRepository) GetMessages(_ context.Context, conversationID string) ([]model.Message, error) {
	msgs := r.messages[conversationID]
	res := make([]model.Message, len(msgs))
	for i, m := range msgs {
		res[i] = *m
	}
	return res, nil
}

var _ repository.Repository = (*inMemoryRepository)(nil)

func TestChatNewConversation(t *testing.T) {
	p := &mockProvider{content: "Recursion is a technique where a function calls itself."}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	resp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		Message: "Jelaskan apa itu recursion",
	})
	if err != nil {
		t.Fatalf("Chat() unexpected error: %v", err)
	}

	if resp.ConversationID == "" {
		t.Errorf("expected non-empty conversation_id")
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", resp.Message.Role)
	}
	if resp.Message.Content != "Recursion is a technique where a function calls itself." {
		t.Errorf("content = %q", resp.Message.Content)
	}

	// Verify conversation created in repository
	conv, _ := repo.GetConversation(ctx, resp.ConversationID, "user-123")
	if conv == nil {
		t.Fatal("conversation not found in repo")
	}
	if conv.Title != "Jelaskan apa itu recursion" {
		t.Errorf("title = %q, want 'Jelaskan apa itu recursion'", conv.Title)
	}

	// Verify both user and assistant messages saved
	msgs, _ := repo.GetMessages(ctx, resp.ConversationID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user, assistant), got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Jelaskan apa itu recursion" {
		t.Errorf("first message mismatch: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != resp.Message.Content {
		t.Errorf("second message mismatch: %+v", msgs[1])
	}
}

func TestChatExistingConversation(t *testing.T) {
	p := &mockProvider{content: "Here is an example in Go."}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// Initial message
	firstResp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		Message: "Explain recursion",
	})
	if err != nil {
		t.Fatalf("first Chat() failed: %v", err)
	}

	// Follow-up message in same conversation
	secondResp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		ConversationID: firstResp.ConversationID,
		Message:        "Show me in Go",
	})
	if err != nil {
		t.Fatalf("second Chat() failed: %v", err)
	}

	if secondResp.ConversationID != firstResp.ConversationID {
		t.Errorf("conversation ID mismatch: %s != %s", secondResp.ConversationID, firstResp.ConversationID)
	}

	// Verify messages: 2 user + 2 assistant = 4
	msgs, _ := repo.GetMessages(ctx, firstResp.ConversationID)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != "user" || msgs[2].Content != "Show me in Go" {
		t.Errorf("third message = %+v", msgs[2])
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "Here is an example in Go." {
		t.Errorf("fourth message = %+v", msgs[3])
	}

	// Verify messages passed to provider on second call (1 system + 3 history + 1 current = 4 in total)
	if len(p.lastReq.Messages) != 4 {
		t.Fatalf("expected 4 messages to provider (1 system + 3 stored), got %d", len(p.lastReq.Messages))
	}
}

func TestChatOwnershipIsolation(t *testing.T) {
	repo := newInMemoryRepo()
	p := &mockProvider{}
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// User A creates conversation
	respA, err := svc.Chat(ctx, "user-A", model.ChatRequest{Message: "User A message"})
	if err != nil {
		t.Fatalf("User A chat failed: %v", err)
	}

	// User B tries to post to User A's conversation
	_, err = svc.Chat(ctx, "user-B", model.ChatRequest{
		ConversationID: respA.ConversationID,
		Message:        "User B attempting to hijack",
	})
	if err == nil {
		t.Fatal("expected error when User B accesses User A's conversation")
	}

	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func TestChatGeminiErrorDoesNotPersistAssistantMessage(t *testing.T) {
	p := &mockProvider{err: apperror.AIProvider("Provider timeout", errors.New("timeout"))}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	_, err := svc.Chat(ctx, "user-123", model.ChatRequest{Message: "Hello"})
	if err == nil {
		t.Fatal("expected Gemini error")
	}

	// User message is recorded, but no assistant message was saved
	for _, msgs := range repo.messages {
		for _, m := range msgs {
			if m.Role == "assistant" {
				t.Errorf("unexpected assistant message saved on provider failure: %+v", m)
			}
		}
	}
}

func TestConversationCRUD(t *testing.T) {
	repo := newInMemoryRepo()
	p := &mockProvider{content: "Answer"}
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// 1. Create chat
	resp, err := svc.Chat(ctx, "user-1", model.ChatRequest{Message: "Hello from User 1"})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	// 2. List conversations
	list, err := svc.ListConversations(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListConversations error: %v", err)
	}
	if len(list) != 1 || list[0].ID != resp.ConversationID {
		t.Fatalf("list mismatch: %+v", list)
	}

	// User 2 listing should be empty
	list2, err := svc.ListConversations(ctx, "user-2")
	if err != nil || len(list2) != 0 {
		t.Fatalf("user-2 list should be empty, got: %+v", list2)
	}

	// 3. Get conversation detail
	detail, err := svc.GetConversation(ctx, resp.ConversationID, "user-1")
	if err != nil {
		t.Fatalf("GetConversation error: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(detail.Messages))
	}

	// User 2 cannot get User 1's conversation
	_, err = svc.GetConversation(ctx, resp.ConversationID, "user-2")
	if err == nil {
		t.Fatal("expected NOT_FOUND for user-2")
	}

	// 4. Delete conversation
	if err := svc.DeleteConversation(ctx, resp.ConversationID, "user-1"); err != nil {
		t.Fatalf("DeleteConversation error: %v", err)
	}

	// Verify deleted
	_, err = svc.GetConversation(ctx, resp.ConversationID, "user-1")
	if err == nil {
		t.Fatal("expected NOT_FOUND after deletion")
	}
}
