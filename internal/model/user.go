package model

import "time"

// Role is a user role used for authorization. Only "admin" exists today.
// TODO: CLIENT REQUIREMENT - expand the role system only when a real client
// need exists (e.g. editor, contributor). Do not pre-build a complex RBAC.
type Role string

const (
	RoleAdmin Role = "admin"
)

// User is the database model for an authenticated user. It intentionally has
// no business-specific fields (no profile, address, phone, etc.).
// TODO: CLIENT REQUIREMENT - add business user fields only after the client's
// authentication requirements are known.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AuthRegisterRequest is the request DTO for POST /api/v1/auth/register.
type AuthRegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// AuthLoginRequest is the request DTO for POST /api/v1/auth/login.
type AuthLoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// AuthResponse is the response DTO containing a signed JWT access token.
type AuthResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresIn int64        `json:"expires_in"` // seconds
	User      UserResponse `json:"user"`
}

// UserResponse is the public representation of a user returned to clients.
type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      Role   `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ToUserResponse converts a User database model to its public representation.
func ToUserResponse(u *User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Email:     u.Email,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}
