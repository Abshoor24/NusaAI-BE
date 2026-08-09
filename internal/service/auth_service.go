package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/USERNAME/PROJECT_NAME/internal/apperror"
	"github.com/USERNAME/PROJECT_NAME/internal/jwt"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
	"github.com/USERNAME/PROJECT_NAME/internal/repository"
)

// AuthService contains the authentication business logic. It depends on the
// UserRepository interface so tests can substitute a mock.
type AuthService interface {
	Register(ctx context.Context, req model.AuthRegisterRequest) (*model.AuthResponse, error)
	Login(ctx context.Context, req model.AuthLoginRequest) (*model.AuthResponse, error)
	Me(ctx context.Context, userID string) (*model.UserResponse, error)
}

type authService struct {
	users  repository.UserRepository
	tokens *jwt.Manager
	logger *slog.Logger
}

func NewAuthService(users repository.UserRepository, tokens *jwt.Manager, logger *slog.Logger) AuthService {
	return &authService{users: users, tokens: tokens, logger: logger}
}

// Register creates a new user with a bcrypt-hashed password and returns a
// signed token. Password is never stored or logged in plaintext.
//
// TODO: CLIENT REQUIREMENT - decide how new accounts are created (invite-only,
// email verification, admin approval) once the client's auth needs are known.
func (s *authService) Register(ctx context.Context, req model.AuthRegisterRequest) (*model.AuthResponse, error) {
	email := normalizeEmail(req.Email)

	existing, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.Conflict("An account with this email already exists", nil)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.Internal("Failed to secure password", err)
	}

	user := &model.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin, // only role that exists today
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	s.logger.Info("user registered", "user_id", user.ID, "role", user.Role)
	return s.issueToken(user)
}

// Login verifies credentials and returns a signed token. The same generic
// error is returned for unknown email and wrong password to avoid user
// enumeration.
func (s *authService) Login(ctx context.Context, req model.AuthLoginRequest) (*model.AuthResponse, error) {
	user, err := s.users.FindByEmail(ctx, normalizeEmail(req.Email))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperror.Unauthorized("Invalid email or password", nil)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, apperror.Unauthorized("Invalid email or password", nil)
	}

	s.logger.Info("user logged in", "user_id", user.ID)
	return s.issueToken(user)
}

// Me returns the authenticated user's public profile.
func (s *authService) Me(ctx context.Context, userID string) (*model.UserResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperror.NotFound("User not found", nil)
	}
	resp := model.ToUserResponse(user)
	return &resp, nil
}

func (s *authService) issueToken(user *model.User) (*model.AuthResponse, error) {
	token, expiresAt, err := s.tokens.Generate(user)
	if err != nil {
		return nil, apperror.Internal("Failed to generate token", err)
	}
	respUser := model.ToUserResponse(user)
	return &model.AuthResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int64(time.Until(expiresAt).Seconds()),
		User:      respUser,
	}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
