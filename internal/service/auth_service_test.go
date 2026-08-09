package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/USERNAME/PROJECT_NAME/internal/apperror"
	"github.com/USERNAME/PROJECT_NAME/internal/config"
	"github.com/USERNAME/PROJECT_NAME/internal/jwt"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
	"github.com/USERNAME/PROJECT_NAME/internal/repository"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockUserRepository is an in-memory UserRepository for service tests.
type mockUserRepository struct {
	byEmail map[string]*model.User
	byID    map[string]*model.User
}

func newMockUserRepository() *mockUserRepository {
	return &mockUserRepository{
		byEmail: map[string]*model.User{},
		byID:    map[string]*model.User{},
	}
}

func (m *mockUserRepository) Create(_ context.Context, u *model.User) error {
	if _, exists := m.byEmail[u.Email]; exists {
		return apperror.Conflict("An account with this email already exists", nil)
	}
	m.byEmail[u.Email] = u
	m.byID[u.ID] = u
	return nil
}

func (m *mockUserRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	return m.byEmail[email], nil
}

func (m *mockUserRepository) FindByID(_ context.Context, id string) (*model.User, error) {
	return m.byID[id], nil
}

func testAuthService(t *testing.T, repo repository.UserRepository) AuthService {
	t.Helper()
	tokens := jwt.NewManager(config.JWTConfig{
		Secret:     "test-secret",
		Expiration: time.Hour,
		Issuer:     "test",
	})
	return NewAuthService(repo, tokens, testLogger())
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestRegisterSuccess(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	resp, err := svc.Register(context.Background(), model.AuthRegisterRequest{
		Email:    "  User@Test.com  ",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if resp.Token == "" {
		t.Error("Token is empty")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", resp.TokenType)
	}
	if resp.User.Email != "user@test.com" {
		t.Errorf("email = %q, want normalized lowercase", resp.User.Email)
	}
	if resp.User.Role != model.RoleAdmin {
		t.Errorf("role = %q, want admin", resp.User.Role)
	}
	if resp.User.ID == "" {
		t.Error("user id is empty")
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())
	req := model.AuthRegisterRequest{Email: "dup@test.com", Password: "secret123"}

	if _, err := svc.Register(context.Background(), req); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	_, err := svc.Register(context.Background(), req)
	wantCode(t, err, "CONFLICT")
}

func TestLoginSuccess(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	if _, err := svc.Register(context.Background(), model.AuthRegisterRequest{
		Email: "login@test.com", Password: "secret123",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resp, err := svc.Login(context.Background(), model.AuthLoginRequest{
		Email: "login@test.com", Password: "secret123",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp.Token == "" {
		t.Error("Token is empty")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	if _, err := svc.Register(context.Background(), model.AuthRegisterRequest{
		Email: "login@test.com", Password: "secret123",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := svc.Login(context.Background(), model.AuthLoginRequest{
		Email: "login@test.com", Password: "wrong-pass",
	})
	wantCode(t, err, "UNAUTHORIZED")
}

func TestLoginUnknownEmail(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	_, err := svc.Login(context.Background(), model.AuthLoginRequest{
		Email: "nobody@test.com", Password: "secret123",
	})
	wantCode(t, err, "UNAUTHORIZED")
}

func TestMe(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	reg, err := svc.Register(context.Background(), model.AuthRegisterRequest{
		Email: "me@test.com", Password: "secret123",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resp, err := svc.Me(context.Background(), reg.User.ID)
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if resp.Email != "me@test.com" {
		t.Errorf("email = %q", resp.Email)
	}
}

func TestMeNotFound(t *testing.T) {
	svc := testAuthService(t, newMockUserRepository())

	_, err := svc.Me(context.Background(), "does-not-exist")
	wantCode(t, err, "NOT_FOUND")
}
