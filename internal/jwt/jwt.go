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
// minimal claims needed: subject (user id), email, issuer and expiry.
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

// ErrTokenExpired marks a token that was correctly signed but whose exp
// claim is in the past. Use errors.Is to distinguish it from malformed or
// forged tokens so callers (and the frontend) can tell "log in again" apart
// from "something is wrong with this token".
var ErrTokenExpired = errors.New("jwt: token expired")

// Claims are the standard payload carried inside a signed token.
// Unknown claims (e.g. "role" from tokens issued before roles were
// removed) are ignored by the JSON decoder, so old tokens stay valid.
type Claims struct {
	Email string `json:"email"`
	jwtlib.RegisteredClaims
}

// Generate signs a new token for the user and returns the token string plus
// its expiry time.
func (m *Manager) Generate(user *model.User) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.expiration)

	claims := Claims{
		Email: user.Email,
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
		if errors.Is(err, jwtlib.ErrTokenExpired) {
			return nil, fmt.Errorf("jwt: parse: %w", ErrTokenExpired)
		}
		return nil, fmt.Errorf("jwt: parse: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("jwt: invalid token claims")
	}
	return claims, nil
}
