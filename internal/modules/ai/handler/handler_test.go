package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type mockService struct {
	chatResp   model.ChatResponse
	convList   []model.Conversation
	convDetail *model.ConversationDetail
	err        error
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
