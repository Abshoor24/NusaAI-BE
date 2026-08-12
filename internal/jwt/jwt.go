package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/model"
)

// Manager signs and validates JWT access tokens. Tokens only carry the
// minimal claims needed: subject (user id), email, role, issuer and expiry.
type Manager struct {
	secret     []byte
	expiration time.Duration
	issuer     string
}

func NewManager(cfg config.JWTConfig) *Manager {
	return &Manager{
		secret:     []byte(cfg.Secret),
		expiration: cfg.Expiration,
		issuer:     cfg.Issuer,
	}
}

// Claims are the standard payload carried inside a signed token.
type Claims struct {
	Email string     `json:"email"`
	Role  model.Role `json:"role"`
	jwtlib.RegisteredClaims
}

// Generate signs a new token for the user and returns the token string plus
// its expiry time.
func (m *Manager) Generate(user *model.User) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.expiration)

	claims := Claims{
		Email: user.Email,
		Role:  user.Role,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    m.issuer,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse validates a token (signature, expiry, issuer, algorithm) and returns
// its claims. Any failure results in an invalid-token error.
func (m *Manager) Parse(tokenString string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(
		tokenString,
		&Claims{},
		func(t *jwtlib.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method %q", t.Header["alg"])
			}
			return m.secret, nil
		},
		jwtlib.WithIssuer(m.issuer),
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("jwt: invalid token claims")
	}
	return claims, nil
}
