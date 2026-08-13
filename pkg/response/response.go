// Package response provides the standard success and error envelopes used by
// every HTTP handler. It is reusable infrastructure, independent of any
// business module.
package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Abshoor24/backend-go/internal/apperror"
)

// successResponse is the standard success envelope. data must be JSON-serializable.
type successResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message"`
}

// errorBody is the standard error body returned inside the error envelope.
type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// errorResponse is the standard error envelope. It never contains stack
// traces, SQL, credentials, or internal paths.
type errorResponse struct {
	Success bool      `json:"success"`
	Error   errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	slog.Default().Info("writeJSON: starting encoding")
	data, err := json.Marshal(v)
	if err != nil {
		slog.Default().Error("writeJSON: failed to encode JSON response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	slog.Default().Info("writeJSON: encoding successful", "bytes", len(data))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)

	n, err := w.Write(data)
	if err != nil {
		slog.Default().Error("writeJSON: failed to write HTTP response bytes", "error", err)
	} else {
		slog.Default().Info("writeJSON: successfully wrote response bytes", "bytes_written", n)
	}
}

// RespondSuccess writes a standard success response.
func RespondSuccess(w http.ResponseWriter, status int, message string, data interface{}) {
	writeJSON(w, status, successResponse{
		Success: true,
		Data:    data,
		Message: message,
	})
}

// RespondError writes a standard error response. Internal details (wrapped
// errors) are logged server-side but never exposed to the client.
func RespondError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var ae *apperror.Error
	if !apperror.IsAppError(err) {
		// Unexpected error: log the real cause, respond generically.
		logger.Error("unexpected error", "error", err)
		ae = apperror.Internal("An unexpected error occurred", err)
	} else {
		ae = err.(*apperror.Error)
		logger.Error("request failed",
			"code", ae.Code(),
			"message", ae.Message,
			"cause", ae.Err,
		)
	}

	writeJSON(w, ae.Status(), errorResponse{
		Success: false,
		Error: errorBody{
			Code:    ae.Code(),
			Message: ae.Message,
			Fields:  ae.Fields,
		},
	})
}
