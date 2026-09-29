package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// sseWriter wraps an http.ResponseWriter with automatic flushing for SSE.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// newSSEWriter commits HTTP 200 with the SSE headers from api.md and returns
// a writer for token/done/error events.
func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	sw := &sseWriter{w: w, flusher: f}
	sw.flush()
	return sw
}

func (s *sseWriter) writeEvent(event string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(s.w, "event: error\ndata: {\"error\":\"internal encoding error\"}\n\n")
		s.flush()
		return
	}
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload)
	s.flush()
}

func (s *sseWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// b64 encodes raw multipart bytes for the JSON-shaped image pipeline.
func b64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
