package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind classifies an application error. It maps to both an HTTP status code
// and a stable machine-readable error code for the API response.
type Kind string

const (
	KindValidation        Kind = "VALIDATION_ERROR"
	KindUnauthorized      Kind = "UNAUTHORIZED"
	KindForbidden         Kind = "FORBIDDEN"
	KindNotFound          Kind = "NOT_FOUND"
	KindMethodNotAllowed  Kind = "METHOD_NOT_ALLOWED"
	KindConflict          Kind = "CONFLICT"
	KindBadRequest        Kind = "BAD_REQUEST"
	KindDatabase          Kind = "DATABASE_ERROR"
	KindAIProvider        Kind = "AI_PROVIDER_ERROR"
	KindExecutionProvider Kind = "EXECUTION_PROVIDER_ERROR"
	KindUnavailable       Kind = "SERVICE_UNAVAILABLE"
	KindRateLimited       Kind = "RATE_LIMITED"
	KindInternal          Kind = "INTERNAL_SERVER_ERROR"
)

// Error is the canonical application error used across all layers.
// It wraps the underlying cause so details can be logged server-side while
// only safe information is exposed to the client.
type Error struct {
	Kind    Kind
	Message string // safe, human-readable message exposed to the client
	Fields  map[string]string
	Err     error // underlying cause, never exposed to the client
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func (k Kind) httpStatus() int {
	switch k {
	case KindValidation, KindBadRequest:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case KindConflict:
		return http.StatusConflict
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindAIProvider, KindExecutionProvider:
		return http.StatusBadGateway
	case KindUnavailable:
		return http.StatusServiceUnavailable
	case KindDatabase:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// Status returns the HTTP status code for the error.
func (e *Error) Status() int { return e.Kind.httpStatus() }

// Code returns the machine-readable error code for the API response.
func (e *Error) Code() string { return string(e.Kind) }

// IsAppError reports whether err is an application error.
func IsAppError(err error) bool {
	var ae *Error
	return errors.As(err, &ae)
}

func new(kind Kind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

func newFields(kind Kind, message string, fields map[string]string, err error) *Error {
	return &Error{Kind: kind, Message: message, Fields: fields, Err: err}
}

func Validation(message string) *Error { return new(KindValidation, message, nil) }
func ValidationFields(fields map[string]string) *Error {
	return newFields(KindValidation, "Invalid request", fields, nil)
}
func BadRequest(message string, err error) *Error   { return new(KindBadRequest, message, err) }
func Unauthorized(message string, err error) *Error { return new(KindUnauthorized, message, err) }
func Forbidden(message string, err error) *Error    { return new(KindForbidden, message, err) }
func NotFound(message string, err error) *Error     { return new(KindNotFound, message, err) }
func MethodNotAllowed(message string) *Error        { return new(KindMethodNotAllowed, message, nil) }
func Conflict(message string, err error) *Error     { return new(KindConflict, message, err) }
func Database(message string, err error) *Error     { return new(KindDatabase, message, err) }
func AIProvider(message string, err error) *Error   { return new(KindAIProvider, message, err) }
func ExecutionProvider(message string, err error) *Error {
	return new(KindExecutionProvider, message, err)
}
func Unavailable(message string, err error) *Error { return new(KindUnavailable, message, err) }
func Internal(message string, err error) *Error    { return new(KindInternal, message, err) }
func RateLimited(message string) *Error            { return new(KindRateLimited, message, nil) }
