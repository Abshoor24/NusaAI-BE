package jwt

import (
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/model"
)

func testManager(expiration time.Duration) *Manager {
	return NewManager(config.JWTConfig{
		Secret:     "test-secret-that-is-long-enough-for-tests",
		Expiration: expiration,
		Issuer:     "test",
	})
}

func testUser() *model.User {
	return &model.User{ID: "user-1", Email: "test@example.com"}
}

func TestParseValidToken(t *testing.T) {
	m := testManager(time.Hour)
	tokenStr, _, err := m.Generate(testUser())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	claims, err := m.Parse(tokenStr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("sub = %q, want user-1", claims.Subject)
	}
}

func TestParseExpiredTokenIsDistinguishable(t *testing.T) {
	m := testManager(-time.Hour) // already expired at issue time
	tokenStr, _, err := m.Generate(testUser())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, err = m.Parse(tokenStr)
	if err == nil {
		t.Fatal("Parse expired token: want error, got nil")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("Parse expired token error = %v, want errors.Is ErrTokenExpired", err)
	}
}

func TestParseForgedTokenIsNotExpiry(t *testing.T) {

	m := testManager(time.Hour)
	tokenStr, _, err := m.Generate(testUser())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Corrupt the signature part.
	forged := tokenStr[:len(tokenStr)-2] + "xx"
	_, err = m.Parse(forged)
	if err == nil {
		t.Fatal("Parse forged token: want error, got nil")
	}
	if errors.Is(err, ErrTokenExpired) {
		t.Errorf("forged token must not classify as expired: %v", err)
	}
}

func TestParseLegacyTokenWithRoleClaim(t *testing.T) {
	m := testManager(time.Hour)
	// Hand-craft a token the way pre-removal code issued it: with a "role"
	// claim. The parser must ignore the unknown claim, not fail.
	legacy := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, jwtlib.MapClaims{
		"sub":   "user-1",
		"email": "test@example.com",
		"role":  "admin",
		"iss":   "test",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	signed, err := legacy.SignedString([]byte("test-secret-that-is-long-enough-for-tests"))
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}
	claims, err := m.Parse(signed)
	if err != nil {
		t.Fatalf("Parse legacy token with role claim: %v", err)
	}
	if claims.Subject != "user-1" || claims.Email != "test@example.com" {
		t.Errorf("claims = %+v, want sub user-1 and test email", claims)
	}
}
