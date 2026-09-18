package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type mockService struct {
	chatResp     model.ChatResponse
	convList     []model.Conversation
	convDetail   *model.ConversationDetail
	err          error
	streamTokens []string // tokens to emit via ChatStream
	streamErr    error    // error chunk to emit via ChatStream
	docResp      *model.IngestDocumentResponse
	docList      []model.DocumentDetail
	docDetail    *model.Document
}

func (m *mockService) Chat(_ context.Context, userID string, req model.ChatRequest) (model.ChatResponse, error) {
	if m.err != nil {
		return model.ChatResponse{}, m.err
	}
	if m.chatResp.ConversationID != "" {
		return m.chatResp, nil
	}
	return model.ChatResponse{
		ConversationID: "conv-123",
		Message: model.ChatMessageItem{
			ID:      "msg-1",
			Role:    "assistant",
			Content: "Echo: " + req.Message,
		},
	}, nil
}

func (m *mockService) ChatStream(_ context.Context, _ string, _ model.ChatRequest) (<-chan provider.StreamChunk, string, string, error) {
	if m.err != nil {
		return nil, "", "", m.err
	}
	ch := make(chan provider.StreamChunk, 16)
	go func() {
		defer close(ch)
		if m.streamErr != nil {
			ch <- provider.StreamChunk{Err: m.streamErr}
			return
		}
		tokens := m.streamTokens
		if len(tokens) == 0 {
			tokens = []string{"Hello", " ", "world"}
		}
		for _, t := range tokens {
			ch <- provider.StreamChunk{Token: t}
		}
		ch <- provider.StreamChunk{Done: true}
	}()
	return ch, "conv-stream-123", "msg-stream-1", nil
}

func (m *mockService) ListConversations(_ context.Context, userID string) ([]model.Conversation, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.convList, nil
}

func (m *mockService) GetConversation(_ context.Context, conversationID, userID string) (*model.ConversationDetail, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.convDetail != nil {
		return m.convDetail, nil
	}
	return &model.ConversationDetail{
		Conversation: model.Conversation{
			ID:        conversationID,
			UserID:    userID,
			Title:     "Test Conversation",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Messages: []model.Message{},
	}, nil
}

func (m *mockService) DeleteConversation(_ context.Context, conversationID, userID string) error {
	return m.err
}

func (m *mockService) IngestDocument(_ context.Context, userID string, req model.IngestDocumentRequest) (*model.IngestDocumentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.docResp != nil {
		return m.docResp, nil
	}
	return &model.IngestDocumentResponse{
		Document: model.Document{
			ID:        "doc-123",
			UserID:    userID,
			Title:     req.Title,
			Source:    req.Source,
			CreatedAt: time.Now(),
		},
		ChunkCount: 3,
	}, nil
}

func (m *mockService) ListDocuments(_ context.Context, userID string) ([]model.DocumentDetail, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.docList, nil
}

func (m *mockService) GetDocument(_ context.Context, id, userID string) (*model.Document, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.docDetail != nil {
		return m.docDetail, nil
	}
	return &model.Document{
		ID:        id,
		UserID:    userID,
		Title:     "Sample Document",
		Source:    "text",
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockService) UploadDocument(_ context.Context, userID, title, fileName string, fileSize int64, _ io.Reader) (*model.Document, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.docDetail != nil {
		return m.docDetail, nil
	}
	return &model.Document{
		ID:        "doc-upload-123",
		UserID:    userID,
		Title:     title,
		Source:    "upload",
		FileName:  fileName,
		FileType:  "txt",
		FileSize:  fileSize,
		Status:    "processing",
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockService) GetDocumentStatus(_ context.Context, id, _ string) (*model.DocumentStatusResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &model.DocumentStatusResponse{
		ID:         id,
		Title:      "Sample Doc",
		Status:     "ready",
		ChunkCount: 3,
	}, nil
}

func (m *mockService) DeleteDocument(_ context.Context, id, userID string) error {
	return m.err
}

func TestChatSuccess(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":"Jelaskan recursion"}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.Chat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ConversationID string `json:"conversation_id"`
			Message        struct {
				ID      string `json:"id"`
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success {
		t.Error("success = false, want true")
	}
	if body.Data.ConversationID != "conv-123" {
		t.Errorf("conv id = %q, want conv-123", body.Data.ConversationID)
	}
	if body.Data.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", body.Data.Message.Role)
	}
}

func TestChatUnauthorized(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":"Hello"}`))

	h.Chat(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestChatValidationError(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":""}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.Chat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListConversations(t *testing.T) {
	svc := &mockService{
		convList: []model.Conversation{
			{ID: "c-1", UserID: "user-123", Title: "Conversation 1"},
		},
	}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.ListConversations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Success bool                 `json:"success"`
		Data    []model.Conversation `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "c-1" {
		t.Errorf("list mismatch: %+v", body.Data)
	}
}

func TestGetConversationDetail(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/conversations/{id}", h.GetConversation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/conv-123", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetConversationNotFound(t *testing.T) {
	svc := &mockService{err: apperror.NotFound("Conversation not found", nil)}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/conversations/{id}", h.GetConversation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/missing", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteConversation(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Delete("/api/v1/ai/conversations/{id}", h.DeleteConversation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ai/conversations/conv-123", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealthConfigured(t *testing.T) {
	cfg := config.AIConfig{Provider: "gemini", APIKey: "secret-key", Model: "gemini-2.5-flash"}
	h := New(&mockService{}, testLogger(), cfg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/health", nil)

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestChatStreamSuccess(t *testing.T) {
	svc := &mockService{streamTokens: []string{"Halo", " ", "dunia"}}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"test stream"}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.ChatStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: token") {
		t.Errorf("body does not contain 'event: token': %s", body)
	}
	if !strings.Contains(body, "Halo") {
		t.Errorf("body does not contain token 'Halo': %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("body does not contain 'event: done': %s", body)
	}
	if !strings.Contains(body, "conv-stream-123") {
		t.Errorf("body does not contain conversation_id: %s", body)
	}
}

func TestChatStreamUnauthorized(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"Hello"}`))

	h.ChatStream(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestChatStreamValidationError(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":""}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.ChatStream(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChatStreamProviderError(t *testing.T) {
	svc := &mockService{streamErr: apperror.AIProvider("stream broke", nil)}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"trigger error"}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.ChatStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected 'event: error' in SSE body, got: %s", body)
	}
}

// --- Document & RAG Handler Tests ---

func TestIngestDocumentSuccess(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/documents", strings.NewReader(`{"title":"Product Handbook","content":"Full text content of product handbook..."}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.IngestDocument(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var body struct {
		Success bool                          `json:"success"`
		Data    model.IngestDocumentResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success {
		t.Error("expected success = true")
	}
	if body.Data.Document.ID != "doc-123" {
		t.Errorf("doc ID = %q, want doc-123", body.Data.Document.ID)
	}
	if body.Data.ChunkCount != 3 {
		t.Errorf("chunk count = %d, want 3", body.Data.ChunkCount)
	}
}

func TestIngestDocumentUnauthorized(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/documents", strings.NewReader(`{"title":"T","content":"Content text"}`))

	h.IngestDocument(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIngestDocumentValidationError(t *testing.T) {
	h := New(&mockService{}, testLogger(), config.AIConfig{MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/documents", strings.NewReader(`{"title":"","content":""}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.IngestDocument(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListDocuments(t *testing.T) {
	svc := &mockService{
		docList: []model.DocumentDetail{
			{Document: model.Document{ID: "doc-1", Title: "Handbook"}, ChunkCount: 5},
		},
	}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/documents", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.ListDocuments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Success bool                   `json:"success"`
		Data    []model.DocumentDetail `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "doc-1" {
		t.Errorf("list mismatch: %+v", body.Data)
	}
}

func TestGetDocumentSuccess(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/documents/{id}", h.GetDocument)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/documents/doc-123", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	svc := &mockService{err: apperror.NotFound("Document not found", nil)}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/documents/{id}", h.GetDocument)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/documents/missing", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteDocument(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Delete("/api/v1/ai/documents/{id}", h.DeleteDocument)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ai/documents/doc-123", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestUploadDocumentSuccess(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 10 << 20})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "My Uploaded Book")
	part, err := mw.CreateFormFile("file", "handbook.txt")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	_, _ = part.Write([]byte("Handbook text content inside file."))
	_ = mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.UploadDocument(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var body struct {
		Success bool           `json:"success"`
		Data    model.Document `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if body.Data.Title != "My Uploaded Book" {
		t.Errorf("title = %q, want 'My Uploaded Book'", body.Data.Title)
	}
	if body.Data.Status != "processing" {
		t.Errorf("status = %q, want 'processing'", body.Data.Status)
	}
}

func TestUploadDocumentMissingFile(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 10 << 20})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "Title Without File")
	_ = mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	h.UploadDocument(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetDocumentStatusSuccess(t *testing.T) {
	svc := &mockService{}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/documents/{id}/status", h.GetDocumentStatus)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/documents/doc-123/status", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body struct {
		Success bool                         `json:"success"`
		Data    model.DocumentStatusResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if body.Data.Status != "ready" {
		t.Errorf("status = %q, want 'ready'", body.Data.Status)
	}
	if body.Data.ChunkCount != 3 {
		t.Errorf("chunk_count = %d, want 3", body.Data.ChunkCount)
	}
}

func TestGetDocumentStatusNotFound(t *testing.T) {
	svc := &mockService{err: apperror.NotFound("Document not found", nil)}
	h := New(svc, testLogger(), config.AIConfig{MaxBytes: 1 << 20})

	r := chi.NewRouter()
	r.Get("/api/v1/ai/documents/{id}/status", h.GetDocumentStatus)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/documents/non-existent/status", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-123"))

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
