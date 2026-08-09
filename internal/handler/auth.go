package handler

import (
	"log/slog"
	"net/http"

	"github.com/USERNAME/PROJECT_NAME/internal/apperror"
	"github.com/USERNAME/PROJECT_NAME/internal/middleware"
	"github.com/USERNAME/PROJECT_NAME/internal/model"
	"github.com/USERNAME/PROJECT_NAME/internal/service"
)

const defaultMaxBytes = 1 << 20 // 1 MB

// AuthHandler exposes register/login/me endpoints.
type AuthHandler struct {
	auth   service.AuthService
	logger *slog.Logger
}

func NewAuthHandler(auth service.AuthService, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{auth: auth, logger: logger}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req model.AuthRegisterRequest
	if err := decodeAndValidate(w, r, &req, defaultMaxBytes); err != nil {
		RespondError(w, h.logger, err)
		return
	}

	resp, err := h.auth.Register(r.Context(), req)
	if err != nil {
		RespondError(w, h.logger, err)
		return
	}

	RespondSuccess(w, http.StatusCreated, "User registered successfully", resp)
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.AuthLoginRequest
	if err := decodeAndValidate(w, r, &req, defaultMaxBytes); err != nil {
		RespondError(w, h.logger, err)
		return
	}

	resp, err := h.auth.Login(r.Context(), req)
	if err != nil {
		RespondError(w, h.logger, err)
		return
	}

	RespondSuccess(w, http.StatusOK, "Login successful", resp)
}

// Me handles GET /api/v1/auth/me. It requires a valid token (RequireAuth).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		RespondError(w, h.logger, apperror.Unauthorized("Missing authentication token", nil))
		return
	}

	resp, err := h.auth.Me(r.Context(), userID)
	if err != nil {
		RespondError(w, h.logger, err)
		return
	}

	RespondSuccess(w, http.StatusOK, "Request successful", resp)
}
