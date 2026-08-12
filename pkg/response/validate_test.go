package response

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Abshoor24/backend-go/internal/apperror"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type validatable struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

func TestDecodeAndValidateValid(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"a@b.com","password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")

	var dst validatable
	if err := DecodeAndValidate(rec, req, &dst, 1<<20); err != nil {
		t.Fatalf("DecodeAndValidate() error = %v", err)
	}
	if dst.Email != "a@b.com" {
		t.Errorf("Email = %q", dst.Email)
	}
}

func TestDecodeAndValidateInvalidEmail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"not-an-email","password":"secret123"}`))

	var dst validatable
	err := DecodeAndValidate(rec, req, &dst, 1<<20)
	if err == nil {
		t.Fatal("DecodeAndValidate() expected error")
	}

	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error = %T, want *apperror.Error", err)
	}
	if ae.Code() != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", ae.Code())
	}
	msg, ok := ae.Fields["email"]
	if !ok {
		t.Errorf("fields = %v, want an email entry", ae.Fields)
	}
	if msg != "must be a valid email address" {
		t.Errorf("email message = %q", msg)
	}
}

func TestDecodeAndValidateRejectsUnknownField(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"a@b.com","password":"secret123","role":"admin"}`))

	var dst validatable
	err := DecodeAndValidate(rec, req, &dst, 1<<20)
	if err == nil {
		t.Fatal("DecodeAndValidate() expected error for unknown field")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "VALIDATION_ERROR" {
		t.Fatalf("error = %v, want VALIDATION_ERROR", err)
	}
}

func TestDecodeAndValidateRejectsOversizedBody(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"a@b.com","password":"secret123"}`))

	var dst validatable
	err := DecodeAndValidate(rec, req, &dst, 16)
	if err == nil {
		t.Fatal("DecodeAndValidate() expected error for oversized body")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "BAD_REQUEST" {
		t.Fatalf("error = %v, want BAD_REQUEST", err)
	}
}

func TestValidationFieldsSerialization(t *testing.T) {
	// Ensure the fields map is serialized in the standard error envelope.
	rec := httptest.NewRecorder()
	err := apperror.ValidationFields(map[string]string{"email": "must be a valid email address"})

	// Sanity: the apperror carries fields.
	if err.Fields["email"] == "" {
		t.Fatal("Fields not set on apperror")
	}

	// Round-trip through the response helper.
	RespondError(rec, testLogger(), err)
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if derr := json.Unmarshal(rec.Body.Bytes(), &body); derr != nil {
		t.Fatalf("decode body: %v", derr)
	}
	if body.Success {
		t.Error("success = true, want false")
	}
	if body.Error.Fields["email"] != "must be a valid email address" {
		t.Errorf("fields = %v", body.Error.Fields)
	}
}
