package routes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/model"
	aimodel "github.com/Abshoor24/backend-go/internal/modules/ai/model"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	aiservice "github.com/Abshoor24/backend-go/internal/modules/ai/service"
)

type dummyAuthService struct{}

func (d *dummyAuthService) Register(_ context.Context, _ model.AuthRegisterRequest) (*model.AuthResponse, error) {
	return nil, nil
}
func (d *dummyAuthService) Login(_ context.Context, _ model.AuthLoginRequest) (*model.AuthResponse, error) {
	return nil, nil
}
func (d *dummyAuthService) Me(_ context.Context, _ string) (*model.UserResponse, error) {
	return nil, nil
}

type dummyAIService struct{}

func (d *dummyAIService) Chat(_ context.Context, _ string, req aimodel.ChatRequest) (aimodel.ChatResponse, error) {
	return aimodel.ChatResponse{
		ConversationID: "conv-1",
		Message: aimodel.ChatMessageItem{
			ID:      "msg-1",
			Role:    "assistant",
			Content: "Answer to: " + req.Message,
		},
	}, nil
}

func (d *dummyAIService) ChatStream(_ context.Context, _ string, _ aimodel.ChatRequest) (<-chan aiprovider.StreamChunk, string, string, error) {
	ch := make(chan aiprovider.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- aiprovider.StreamChunk{Done: true}
	}()
	return ch, "conv-stream-1", "msg-stream-1", nil
}

func (d *dummyAIService) ListConversations(_ context.Context, _ string) ([]aimodel.Conversation, error) {
	return []aimodel.Conversation{}, nil
}

func (d *dummyAIService) GetConversation(_ context.Context, id, userID string) (*aimodel.ConversationDetail, error) {
	return &aimodel.ConversationDetail{
		Conversation: aimodel.Conversation{ID: id, UserID: userID},
		Messages:     []aimodel.Message{},
	}, nil
}

func (d *dummyAIService) DeleteConversation(_ context.Context, _, _ string) error {
	return nil
}

func (d *dummyAIService) IngestDocument(_ context.Context, _ string, req aimodel.IngestDocumentRequest) (*aimodel.IngestDocumentResponse, error) {
	return &aimodel.IngestDocumentResponse{
		Document:   aimodel.Document{ID: "doc-1", Title: req.Title},
		ChunkCount: 1,
	}, nil
}

func (d *dummyAIService) ListDocuments(_ context.Context, _ string) ([]aimodel.DocumentDetail, error) {
	return []aimodel.DocumentDetail{}, nil
}

func (d *dummyAIService) GetDocument(_ context.Context, id, userID string) (*aimodel.Document, error) {
	return &aimodel.Document{ID: id, UserID: userID, Title: "Doc Title"}, nil
}

func (d *dummyAIService) DeleteDocument(_ context.Context, _, _ string) error {
	return nil
}

var _ aiservice.Service = (*dummyAIService)(nil)

func setupTestRouter() (http.Handler, *jwt.Manager) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: "8080",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"*"},
		},
		JWT: config.JWTConfig{
			Secret:     "secret-must-be-long-enough-for-testing",
			Expiration: time.Hour,
			Issuer:     "test",
		},
		AI: config.AIConfig{
			ChatRateLimitEnabled: false,
			MaxBytes:             1 << 20,
		},
		Rate: config.RateLimitConfig{
			Enabled: false,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := jwt.NewManager(cfg.JWT)

	router := New(Dependencies{
		Config: cfg,
		Logger: logger,
		DB:     nil,
		Auth:   &dummyAuthService{},
		AI:     &dummyAIService{},
		Tokens: tokens,
	})
	return router, tokens
}

func TestAIChatRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":"Hello"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/v1/ai/chat without token should return 401, got %d", rec.Code)
	}
}

func TestAIChatAuthorized(t *testing.T) {
	router, tokens := setupTestRouter()

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-1", Email: "test@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(`{"message":"Hello"}`))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /api/v1/ai/chat with token should return 200 OK, got %d", rec.Code)
	}
}

func TestAIChatStreamRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"Hello"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/v1/ai/chat/stream without token should return 401, got %d", rec.Code)
	}
}

func TestAIChatStreamAuthorized(t *testing.T) {
	router, tokens := setupTestRouter()

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-1", Email: "test@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"Hello"}`))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /api/v1/ai/chat/stream with token should return 200 OK, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type should be text/event-stream, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestConversationEndpointsRequireAuth(t *testing.T) {
	router, _ := setupTestRouter()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/ai/conversations"},
		{http.MethodGet, "/api/v1/ai/conversations/conv-1"},
		{http.MethodDelete, "/api/v1/ai/conversations/conv-1"},
	}

	for _, ep := range endpoints {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(ep.method, ep.path, nil)
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s expected 401 Unauthorized, got %d", ep.method, ep.path, rec.Code)
		}
	}
}

func TestDocumentEndpointsRequireAuth(t *testing.T) {
	router, _ := setupTestRouter()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/ai/documents"},
		{http.MethodGet, "/api/v1/ai/documents"},
		{http.MethodGet, "/api/v1/ai/documents/doc-1"},
		{http.MethodDelete, "/api/v1/ai/documents/doc-1"},
	}

	for _, ep := range endpoints {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(`{"title":"Doc","content":"Content text"}`))
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s expected 401 Unauthorized, got %d", ep.method, ep.path, rec.Code)
		}
	}
}

func TestPublicHealthDoesNotRequireAuth(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/health", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1/ai/health expected 200 OK, got %d", rec.Code)
	}
}
