package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
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
	content       string
	model         string
	err           error
	lastReq       provider.Request
	streamContent string // tokens to emit, space-separated
	streamErr     error  // error to emit on stream
	omitDone      bool   // if true, close stream without emitting Done chunk
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

func (m *mockProvider) CompleteStream(_ context.Context, req provider.Request) (<-chan provider.StreamChunk, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamChunk, 16)
	go func() {
		defer close(ch)
		if m.streamErr != nil {
			ch <- provider.StreamChunk{Err: m.streamErr}
			return
		}
		content := m.streamContent
		if content == "" {
			content = "mock stream response"
		}
		for _, word := range strings.Fields(content) {
			ch <- provider.StreamChunk{Token: word + " "}
		}
		if !m.omitDone {
			ch <- provider.StreamChunk{Done: true}
		}
	}()
	return ch, nil
}

func (m *mockProvider) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

type inMemoryRepository struct {
	conversations  map[string]*model.Conversation
	messages       map[string][]*model.Message
	documents      map[string]*model.Document
	documentChunks map[string][]model.DocumentChunk
}

func newInMemoryRepo() *inMemoryRepository {
	return &inMemoryRepository{
		conversations:  make(map[string]*model.Conversation),
		messages:       make(map[string][]*model.Message),
		documents:      make(map[string]*model.Document),
		documentChunks: make(map[string][]model.DocumentChunk),
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

func (r *inMemoryRepository) CreateDocument(_ context.Context, doc *model.Document) error {
	doc.CreatedAt = time.Now()
	r.documents[doc.ID] = doc
	return nil
}

func (r *inMemoryRepository) GetDocument(_ context.Context, id, userID string) (*model.Document, error) {
	doc, ok := r.documents[id]
	if !ok || doc.UserID != userID {
		return nil, nil
	}
	return doc, nil
}

func (r *inMemoryRepository) ListDocuments(_ context.Context, userID string) ([]model.DocumentDetail, error) {
	var list []model.DocumentDetail
	for _, doc := range r.documents {
		if doc.UserID == userID {
			chunks := r.documentChunks[doc.ID]
			list = append(list, model.DocumentDetail{
				Document:   *doc,
				ChunkCount: len(chunks),
			})
		}
	}
	return list, nil
}

func (r *inMemoryRepository) DeleteDocument(_ context.Context, id, userID string) error {
	doc, ok := r.documents[id]
	if !ok || doc.UserID != userID {
		return apperror.NotFound("Document not found", nil)
	}
	delete(r.documents, id)
	delete(r.documentChunks, id)
	return nil
}

func (r *inMemoryRepository) CreateDocumentChunks(_ context.Context, chunks []model.DocumentChunk) error {
	for _, c := range chunks {
		c.CreatedAt = time.Now()
		r.documentChunks[c.DocumentID] = append(r.documentChunks[c.DocumentID], c)
	}
	return nil
}

func (r *inMemoryRepository) SearchSimilarChunks(_ context.Context, _ []float32, userID string, documentID string, limit int) ([]model.ChunkSearchResult, error) {
	var results []model.ChunkSearchResult
	for docID, chunks := range r.documentChunks {
		doc, ok := r.documents[docID]
		if !ok || doc.UserID != userID {
			continue // strict ownership isolation
		}
		if documentID != "" && docID != documentID {
			continue
		}
		for _, c := range chunks {
			results = append(results, model.ChunkSearchResult{
				ID:         c.ID,
				DocumentID: c.DocumentID,
				ChunkIndex: c.ChunkIndex,
				Content:    c.Content,
				Similarity: 0.95,
			})
			if len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
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

	conv, _ := repo.GetConversation(ctx, resp.ConversationID, "user-123")
	if conv == nil {
		t.Fatal("conversation not found in repo")
	}
	if conv.Title != "Jelaskan apa itu recursion" {
		t.Errorf("title = %q, want 'Jelaskan apa itu recursion'", conv.Title)
	}

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

	firstResp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		Message: "Explain recursion",
	})
	if err != nil {
		t.Fatalf("first Chat() failed: %v", err)
	}

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

	if len(p.lastReq.Messages) != 4 {
		t.Fatalf("expected 4 messages to provider, got %d", len(p.lastReq.Messages))
	}
}

func TestChatOwnershipIsolation(t *testing.T) {
	repo := newInMemoryRepo()
	p := &mockProvider{}
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	respA, err := svc.Chat(ctx, "user-A", model.ChatRequest{Message: "User A message"})
	if err != nil {
		t.Fatalf("User A chat failed: %v", err)
	}

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

	resp, err := svc.Chat(ctx, "user-1", model.ChatRequest{Message: "Hello from User 1"})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	list, err := svc.ListConversations(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListConversations error: %v", err)
	}
	if len(list) != 1 || list[0].ID != resp.ConversationID {
		t.Fatalf("list mismatch: %+v", list)
	}

	list2, err := svc.ListConversations(ctx, "user-2")
	if err != nil || len(list2) != 0 {
		t.Fatalf("user-2 list should be empty, got: %+v", list2)
	}

	detail, err := svc.GetConversation(ctx, resp.ConversationID, "user-1")
	if err != nil {
		t.Fatalf("GetConversation error: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(detail.Messages))
	}

	_, err = svc.GetConversation(ctx, resp.ConversationID, "user-2")
	if err == nil {
		t.Fatal("expected NOT_FOUND for user-2")
	}

	if err := svc.DeleteConversation(ctx, resp.ConversationID, "user-1"); err != nil {
		t.Fatalf("DeleteConversation error: %v", err)
	}

	_, err = svc.GetConversation(ctx, resp.ConversationID, "user-1")
	if err == nil {
		t.Fatal("expected NOT_FOUND after deletion")
	}
}

// --- ChatStream tests ---

func TestChatStreamNewConversation(t *testing.T) {
	p := &mockProvider{streamContent: "Hello world from stream"}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	ch, convID, msgID, err := svc.ChatStream(ctx, "user-123", model.ChatRequest{
		Message: "Say hello",
	})
	if err != nil {
		t.Fatalf("ChatStream() unexpected error: %v", err)
	}
	if convID == "" {
		t.Error("expected non-empty convID")
	}
	if msgID == "" {
		t.Error("expected non-empty msgID")
	}

	var tokens []string
	gotDone := false
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Done {
			gotDone = true
			break
		}
		if chunk.Token != "" {
			tokens = append(tokens, chunk.Token)
		}
	}
	if !gotDone {
		t.Error("stream ended without done chunk")
	}
	if len(tokens) == 0 {
		t.Error("expected at least one token")
	}
	combined := strings.Join(tokens, "")
	if !strings.Contains(combined, "Hello") {
		t.Errorf("tokens don't contain expected content: %q", combined)
	}

	msgs, _ := repo.GetMessages(ctx, convID)
	var assistantFound bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.ID == msgID {
			assistantFound = true
		}
	}
	if !assistantFound {
		t.Error("assistant message not persisted after stream completed")
	}
}

func TestChatStreamExistingConversation(t *testing.T) {
	p := &mockProvider{streamContent: "continuation reply"}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	p.content = "first reply"
	firstResp, err := svc.Chat(ctx, "user-stream", model.ChatRequest{Message: "First message"})
	if err != nil {
		t.Fatalf("initial Chat failed: %v", err)
	}

	ch, convID, _, err := svc.ChatStream(ctx, "user-stream", model.ChatRequest{
		ConversationID: firstResp.ConversationID,
		Message:        "Follow up via stream",
	})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	if convID != firstResp.ConversationID {
		t.Errorf("convID mismatch: got %s, want %s", convID, firstResp.ConversationID)
	}

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
	}

	msgs, _ := repo.GetMessages(ctx, convID)
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages, got %d", len(msgs))
	}
}

func TestChatStreamOwnershipIsolation(t *testing.T) {
	p := &mockProvider{}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	p.content = "user a reply"
	respA, err := svc.Chat(ctx, "user-A", model.ChatRequest{Message: "Hello from A"})
	if err != nil {
		t.Fatalf("User A chat failed: %v", err)
	}

	_, _, _, err = svc.ChatStream(ctx, "user-B", model.ChatRequest{
		ConversationID: respA.ConversationID,
		Message:        "Hijack attempt",
	})
	if err == nil {
		t.Fatal("expected error when user-B accesses user-A conversation")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func TestChatStreamProviderError(t *testing.T) {
	p := &mockProvider{streamErr: apperror.AIProvider("stream failed", errors.New("network error"))}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	ch, convID, _, err := svc.ChatStream(ctx, "user-123", model.ChatRequest{Message: "trigger error"})
	if err != nil {
		t.Fatalf("unexpected early error: %v (should arrive via channel)", err)
	}

	var gotErr bool
	for chunk := range ch {
		if chunk.Err != nil {
			gotErr = true
			break
		}
	}
	if !gotErr {
		t.Error("expected error chunk on stream")
	}

	msgs, _ := repo.GetMessages(ctx, convID)
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Errorf("unexpected assistant message persisted on stream error: %+v", m)
		}
	}
}

func TestChatStreamInterruptedPersistsPartialMessage(t *testing.T) {
	p := &mockProvider{
		streamContent: "partial response text",
		omitDone:      true,
	}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	ch, convID, msgID, err := svc.ChatStream(ctx, "user-123", model.ChatRequest{Message: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}

	msgs, _ := repo.GetMessages(ctx, convID)
	var found bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.ID == msgID {
			found = true
			if !strings.Contains(m.Content, "partial") {
				t.Errorf("content = %q, want it to contain 'partial'", m.Content)
			}
		}
	}
	if !found {
		t.Error("expected partial assistant message to be persisted on stream interruption")
	}
}

// --- RAG Document Tests ---

func TestIngestDocument(t *testing.T) {
	p := &mockProvider{}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	resp, err := svc.IngestDocument(ctx, "user-123", model.IngestDocumentRequest{
		Title:   "Go Concurrency Guide",
		Content: "Goroutines are lightweight threads managed by the Go runtime. Channels provide synchronization.",
		Source:  "manual",
	})
	if err != nil {
		t.Fatalf("IngestDocument failed: %v", err)
	}

	if resp.Document.ID == "" {
		t.Error("expected non-empty document ID")
	}
	if resp.ChunkCount == 0 {
		t.Error("expected at least 1 chunk")
	}

	// Verify document saved in repo
	doc, err := repo.GetDocument(ctx, resp.Document.ID, "user-123")
	if err != nil || doc == nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.Title != "Go Concurrency Guide" {
		t.Errorf("doc title = %q, want 'Go Concurrency Guide'", doc.Title)
	}
}

func TestDocumentCRUD(t *testing.T) {
	p := &mockProvider{}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	resp, err := svc.IngestDocument(ctx, "user-1", model.IngestDocumentRequest{
		Title:   "Doc 1",
		Content: "Sample content text for testing document CRUD.",
	})
	if err != nil {
		t.Fatalf("IngestDocument failed: %v", err)
	}

	// List
	list, err := svc.ListDocuments(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != resp.Document.ID {
		t.Errorf("list mismatch: %+v", list)
	}

	// Get
	doc, err := svc.GetDocument(ctx, resp.Document.ID, "user-1")
	if err != nil || doc == nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	// Delete
	if err := svc.DeleteDocument(ctx, resp.Document.ID, "user-1"); err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}

	// Verify deleted
	_, err = svc.GetDocument(ctx, resp.Document.ID, "user-1")
	if err == nil {
		t.Error("expected error when getting deleted document")
	}
}

func TestChatWithRAG(t *testing.T) {
	p := &mockProvider{content: "According to the document, penguins live in Antarctica."}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// Ingest document
	_, err := svc.IngestDocument(ctx, "user-123", model.IngestDocumentRequest{
		Title:   "Penguin Research",
		Content: "Penguins live primarily in Antarctica and feed on krill and squid.",
	})
	if err != nil {
		t.Fatalf("IngestDocument failed: %v", err)
	}

	// Chat with UseRAG: true
	resp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		Message: "Where do penguins live?",
		UseRAG:  true,
	})
	if err != nil {
		t.Fatalf("Chat with RAG failed: %v", err)
	}

	// Verify provider was sent the reference knowledge in the system prompt
	var hasReferenceKnowledge bool
	for _, m := range p.lastReq.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "REFERENCE KNOWLEDGE FROM USER DOCUMENTS") {
			hasReferenceKnowledge = true
			if !strings.Contains(m.Content, "Penguins live primarily in Antarctica") {
				t.Errorf("system prompt did not contain document excerpt: %s", m.Content)
			}
			break
		}
	}
	if !hasReferenceKnowledge {
		t.Error("expected provider messages to include reference knowledge context")
	}

	// Verify sources are returned
	if len(resp.Sources) == 0 {
		t.Error("expected non-empty sources in ChatResponse when UseRAG is true")
	}
}

func TestChatWithoutRAG(t *testing.T) {
	p := &mockProvider{content: "Generic answer without RAG."}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// Ingest document
	_, _ = svc.IngestDocument(ctx, "user-123", model.IngestDocumentRequest{
		Title:   "Some Doc",
		Content: "Some content text that should NOT be in the prompt.",
	})

	// Chat with UseRAG: false
	resp, err := svc.Chat(ctx, "user-123", model.ChatRequest{
		Message: "A general question",
		UseRAG:  false,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify provider was NOT sent reference knowledge
	for _, m := range p.lastReq.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "REFERENCE KNOWLEDGE") {
			t.Errorf("unexpected reference knowledge found in non-RAG chat: %s", m.Content)
		}
	}

	// Verify sources are empty
	if len(resp.Sources) != 0 {
		t.Errorf("expected 0 sources when UseRAG is false, got %d", len(resp.Sources))
	}
}

func TestChatStreamWithRAG(t *testing.T) {
	p := &mockProvider{streamContent: "RAG streaming reply"}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	_, _ = svc.IngestDocument(ctx, "user-123", model.IngestDocumentRequest{
		Title:   "Doc Stream",
		Content: "Important documentation chunk content for streaming test.",
	})

	ch, _, _, err := svc.ChatStream(ctx, "user-123", model.ChatRequest{
		Message: "Explain doc stream",
		UseRAG:  true,
	})
	if err != nil {
		t.Fatalf("ChatStream with RAG failed: %v", err)
	}

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
	}

	var hasRefKnowledge bool
	for _, m := range p.lastReq.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "REFERENCE KNOWLEDGE FROM USER DOCUMENTS") {
			hasRefKnowledge = true
			break
		}
	}
	if !hasRefKnowledge {
		t.Error("expected provider messages to include reference knowledge in ChatStream with UseRAG")
	}
}

func TestDocumentOwnershipIsolation(t *testing.T) {
	p := &mockProvider{content: "Answer"}
	repo := newInMemoryRepo()
	svc := New(p, config.AIConfig{Model: "test-model"}, testLogger(), repo)
	ctx := context.Background()

	// User A ingests confidential document
	docA, err := svc.IngestDocument(ctx, "user-A", model.IngestDocumentRequest{
		Title:   "Secret Project Alpha",
		Content: "Project Alpha secret code is 99887766.",
	})
	if err != nil {
		t.Fatalf("User A IngestDocument failed: %v", err)
	}

	// User B tries to retrieve User A's document directly
	_, err = svc.GetDocument(ctx, docA.Document.ID, "user-B")
	if err == nil {
		t.Error("expected NOT_FOUND when User B accesses User A's document")
	}

	// User B tries to delete User A's document
	err = svc.DeleteDocument(ctx, docA.Document.ID, "user-B")
	if err == nil {
		t.Error("expected error when User B tries to delete User A's document")
	}

	// User B chats with UseRAG: true
	_, err = svc.Chat(ctx, "user-B", model.ChatRequest{
		Message: "What is Project Alpha secret code?",
		UseRAG:  true,
	})
	if err != nil {
		t.Fatalf("User B chat failed: %v", err)
	}

	// Verify User A's secret is NOT in User B's prompt
	for _, m := range p.lastReq.Messages {
		if strings.Contains(m.Content, "99887766") {
			t.Fatal("SECURITY VIOLATION: User A's secret document was leaked to User B's prompt!")
		}
	}
}
