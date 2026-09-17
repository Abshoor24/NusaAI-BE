// Package handler — SSE (Server-Sent Events) helpers.
// This file provides low-level helpers for writing SSE events to an
// http.ResponseWriter and a convenience method for flushing after every write.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// sseWriter wraps an http.ResponseWriter and an http.Flusher so that callers
// can write SSE events with automatic flushing.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// newSSEWriter prepares the ResponseWriter for SSE and returns an sseWriter.
// It sets the required headers and flushes the header immediately so the client
// sees a 200 OK before the first event.
func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable nginx / proxy buffering where possible.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	f, _ := w.(http.Flusher)
	sw := &sseWriter{w: w, flusher: f}
	sw.flush()
	return sw
}

// writeEvent writes a single SSE event with the given event name and JSON data.
func (s *sseWriter) writeEvent(event string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		// Fallback: write a raw error event so the client is informed.
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
