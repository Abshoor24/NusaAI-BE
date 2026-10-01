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

	"github.com/Abshoor24/backend-go/internal/middleware"
	"github.com/Abshoor24/backend-go/internal/model"
)

type stubAuthService struct{}

func (stubAuthService) Register(_ context.Context, req model.AuthRegisterRequest) (*model.AuthResponse, error) {
	return &model.AuthResponse{
		Token:     "test-token",
		TokenType: "Bearer",
		ExpiresIn: 86400,
		User:      model.UserResponse{ID: "user-1", Email: req.Email, CreatedAt: "2026-09-26T10:00:00Z"},
	}, nil
}

func (stubAuthService) Login(_ context.Context, req model.AuthLoginRequest) (*model.AuthResponse, error) {
	return &model.AuthResponse{
		Token:     "test-token",
		TokenType: "Bearer",
		ExpiresIn: 86400,
		User:      model.UserResponse{ID: "user-1", Email: req.Email, CreatedAt: "2026-09-26T10:00:00Z"},
	}, nil
}

func (stubAuthService) Me(_ context.Context, userID string) (*model.UserResponse, error) {
	return &model.UserResponse{ID: userID, Email: "user@test.com", CreatedAt: "2026-09-26T10:00:00Z"}, nil
}

func testAuthHandler() *AuthHandler {
	return NewAuthHandler(stubAuthService{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// userPayload extracts data.user from a success envelope as a raw map so
// tests can assert on key presence, not just decoded values.
func userPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v, want object", body["data"])
	}
	var user map[string]interface{}
	if u, ok := data["user"].(map[string]interface{}); ok {
		user = u // register/login wrap the user object
	} else {
		user = data // /me returns the user object directly
	}
	return user
}

func assertNoRoleKey(t *testing.T, rec *httptest.ResponseRecorder, endpoint string) {
	t.Helper()
	user := userPayload(t, rec)
	if _, present := user["role"]; present {
		t.Errorf("%s: data.user must not contain a role key, got %v", endpoint, user)
	}
	if _, present := user["id"]; !present {
		t.Errorf("%s: data.user must contain id, got %v", endpoint, user)
	}
	if _, present := user["email"]; !present {
		t.Errorf("%s: data.user must contain email, got %v", endpoint, user)
	}
}

func TestRegisterOmitsRoleKey(t *testing.T) {
	h := testAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"User@Test.com","password":"Password123"}`))
	rec := httptest.NewRecorder()
	h.Register(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	assertNoRoleKey(t, rec, "register")
}

func TestLoginOmitsRoleKey(t *testing.T) {
	h := testAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"user@test.com","password":"Password123"}`))
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertNoRoleKey(t, rec, "login")
}

func TestMeOmitsRoleKey(t *testing.T) {
	h := testAuthHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()
	h.Me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertNoRoleKey(t, rec, "me")
}
