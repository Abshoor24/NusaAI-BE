package routes

import (
	"context"
	"encoding/json"
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
	executionmodel "github.com/Abshoor24/backend-go/internal/modules/execution/model"
	executionservice "github.com/Abshoor24/backend-go/internal/modules/execution/service"
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

func (d *dummyAIService) UploadDocument(_ context.Context, userID, title, fileName string, fileSize int64, _ io.Reader) (*aimodel.Document, error) {
	return &aimodel.Document{
		ID:        "doc-upload-1",
		UserID:    userID,
		Title:     title,
		FileName:  fileName,
		FileSize:  fileSize,
		Status:    "processing",
		CreatedAt: time.Now(),
	}, nil
}

func (d *dummyAIService) GetDocumentStatus(_ context.Context, id, _ string) (*aimodel.DocumentStatusResponse, error) {
	return &aimodel.DocumentStatusResponse{
		ID:         id,
		Title:      "Doc Title",
		Status:     "ready",
		ChunkCount: 1,
	}, nil
}

func (d *dummyAIService) DeleteDocument(_ context.Context, _, _ string) error {
	return nil
}

func (d *dummyAIService) GetAttachment(_ context.Context, id, _ string) (*aimodel.MessageAttachment, io.ReadCloser, error) {
	return &aimodel.MessageAttachment{
		ID:        id,
		MessageID: "msg-1",
		FileName:  "img.png",
		FileType:  "png",
		MimeType:  "image/png",
		FileSize:  4,
	}, io.NopCloser(strings.NewReader("fake")), nil
}

var _ aiservice.Service = (*dummyAIService)(nil)

type dummyExecutionService struct{}

func (d *dummyExecutionService) Execute(_ context.Context, _ string, req executionmodel.ExecuteRequest) (*executionmodel.ExecuteResponse, error) {
	return &executionmodel.ExecuteResponse{
		ID:              "exec-dummy-1",
		Language:        req.Language,
		Stdout:          "Hello from " + req.Language,
		ExitCode:        0,
		ExecutionTimeMs: 15,
		MemoryKB:        1024,
		Status:          "Accepted",
	}, nil
}

func (d *dummyExecutionService) GetExecution(_ context.Context, id, userID string) (*executionmodel.CodeExecution, error) {
	return &executionmodel.CodeExecution{ID: id, UserID: userID}, nil
}

func (d *dummyExecutionService) ListExecutions(_ context.Context, _ string, _, _ int) ([]executionmodel.CodeExecution, error) {
	return []executionmodel.CodeExecution{}, nil
}

var _ executionservice.Service = (*dummyExecutionService)(nil)

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
		Execution: config.ExecutionConfig{
			RateLimitEnabled: false,
			MaxCodeBytes:     65536,
		},
		Rate: config.RateLimitConfig{
			Enabled: false,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := jwt.NewManager(cfg.JWT)

	router := New(Dependencies{
		Config:    cfg,
		Logger:    logger,
		DB:        nil,
		Auth:      &dummyAuthService{},
		AI:        &dummyAIService{},
		Execution: &dummyExecutionService{},
		Tokens:    tokens,
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

	// Test with Authorization header (fetch approach)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", strings.NewReader(`{"message":"Hello"}`))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /api/v1/ai/chat/stream with header token should return 200 OK, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type should be text/event-stream, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestAIChatStreamAuthorizedViaQueryParam(t *testing.T) {
	router, tokens := setupTestRouter()

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-1", Email: "test@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	// Test with ?token= query parameter (EventSource approach)
	rec := httptest.NewRecorder()
	url := "/api/v1/ai/chat/stream?token=" + tokenStr
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"message":"Hello"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /api/v1/ai/chat/stream with query token should return 200 OK, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type should be text/event-stream, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestAIChatStreamInvalidTokenQueryParam(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	url := "/api/v1/ai/chat/stream?token=invalid-token"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"message":"Hello"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/v1/ai/chat/stream with invalid query token should return 401, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["success"] != false {
		t.Errorf("expected success=false, got %v", body["success"])
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}
	if errObj["code"] != "UNAUTHORIZED" {
		t.Errorf("expected error code UNAUTHORIZED, got %v", errObj["code"])
	}
}

func TestRateLimitResponseFormat(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "8080"},
		CORS:   config.CORSConfig{AllowedOrigins: []string{"*"}},
		JWT: config.JWTConfig{
			Secret:     "secret-must-be-long-enough-for-testing",
			Expiration: time.Hour,
			Issuer:     "test",
		},
		AI: config.AIConfig{ChatRateLimitEnabled: false},
		Execution: config.ExecutionConfig{
			RateLimitEnabled: true,
			RateLimitPerMin:  1,
			RateLimitBurst:   1,
			MaxCodeBytes:     65536,
		},
		Rate: config.RateLimitConfig{Enabled: false},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := jwt.NewManager(cfg.JWT)

	router := New(Dependencies{
		Config:    cfg,
		Logger:    logger,
		DB:        nil,
		Auth:      &dummyAuthService{},
		AI:        &dummyAIService{},
		Execution: &dummyExecutionService{},
		Tokens:    tokens,
	})

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-rate", Email: "rate@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	// First request - allowed
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(1)"}`))
	req1.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec1, req1)

	// Second request - rate limited (429)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(2)"}`))
	req2.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("request 2 should be rate limited with 429, got %d", rec2.Code)
	}

	// Verify Retry-After header
	retryAfter := rec2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header should be present on 429 response")
	}

	// Verify error body format
	var body map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["success"] != false {
		t.Errorf("expected success=false, got %v", body["success"])
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}
	if errObj["code"] != "RATE_LIMITED" {
		t.Errorf("expected error code RATE_LIMITED, got %v", errObj["code"])
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
		{http.MethodPost, "/api/v1/ai/documents/upload"},
		{http.MethodGet, "/api/v1/ai/documents"},
		{http.MethodGet, "/api/v1/ai/documents/doc-1"},
		{http.MethodGet, "/api/v1/ai/documents/doc-1/status"},
		{http.MethodDelete, "/api/v1/ai/documents/doc-1"},
		{http.MethodPost, "/api/v1/documents/upload"},
		{http.MethodGet, "/api/v1/documents/doc-1/status"},
		{http.MethodGet, "/api/v1/ai/attachments/att-1"},
		{http.MethodGet, "/api/v1/attachments/att-1"},
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

func TestExecuteRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(1)"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/v1/execute without token should return 401, got %d", rec.Code)
	}
}

func TestExecuteAuthorized(t *testing.T) {
	router, tokens := setupTestRouter()

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-1", Email: "test@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print('Hello')"}`))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /api/v1/execute with token should return 200 OK, got %d", rec.Code)
	}
}

func TestExecuteRateLimiting(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "8080"},
		CORS:   config.CORSConfig{AllowedOrigins: []string{"*"}},
		JWT: config.JWTConfig{
			Secret:     "secret-must-be-long-enough-for-testing",
			Expiration: time.Hour,
			Issuer:     "test",
		},
		AI: config.AIConfig{ChatRateLimitEnabled: false},
		Execution: config.ExecutionConfig{
			RateLimitEnabled: true,
			RateLimitPerMin:  2,
			RateLimitBurst:   2,
			MaxCodeBytes:     65536,
		},
		Rate: config.RateLimitConfig{Enabled: false},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := jwt.NewManager(cfg.JWT)

	router := New(Dependencies{
		Config:    cfg,
		Logger:    logger,
		DB:        nil,
		Auth:      &dummyAuthService{},
		AI:        &dummyAIService{},
		Execution: &dummyExecutionService{},
		Tokens:    tokens,
	})

	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-rate", Email: "rate@example.com", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}

	// First request - allowed
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(1)"}`))
	req1.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("request 1 should be 200, got %d", rec1.Code)
	}

	// Second request - allowed (burst = 2)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(2)"}`))
	req2.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("request 2 should be 200, got %d", rec2.Code)
	}

	// Third request - rate limited (429)
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"language":"python","code":"print(3)"}`))
	req3.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("request 3 should be rate limited with 429, got %d", rec3.Code)
	}
}

