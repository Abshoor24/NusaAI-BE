package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SSE tuning. Heartbeats keep idle streams alive through proxies/browsers
// while Gemini is still producing; the write deadline is refreshed on every
// write so a single stalled client write fails fast instead of hanging.
// Tests override these (same package) to avoid waiting real time.
var (
	sseHeartbeatInterval = 15 * time.Second
	sseWriteTimeout      = 60 * time.Second
)

// sseWriter wraps an http.ResponseWriter with automatic flushing for SSE.
type sseWriter struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	heartbeat    time.Duration
	writeTimeout time.Duration
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
	sw := &sseWriter{w: w, flusher: f, heartbeat: sseHeartbeatInterval, writeTimeout: sseWriteTimeout}
	sw.touchDeadline()
	sw.flush()
	return sw
}

func (s *sseWriter) writeEvent(event string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(s.w, "event: error\ndata: {\"success\":false,\"error\":{\"code\":\"INTERNAL_SERVER_ERROR\",\"message\":\"internal encoding error\"}}\n\n")
		s.touchDeadline()
		s.flush()
		return
	}
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload)
	s.touchDeadline()
	s.flush()
}

// writeHeartbeat sends an SSE comment so intermediaries do not treat an
// idle-but-live stream as dead. Comments carry no data and are ignored by
// event parsers.
func (s *sseWriter) writeHeartbeat() {
	fmt.Fprintf(s.w, ": ping\n\n")
	s.touchDeadline()
	s.flush()
}

// touchDeadline extends the write deadline for the next write. Without this,
// http.Server.WriteTimeout alone bounds slow client writes; refreshing per
// write keeps a healthy-but-slow stream alive while still failing fast on a
// truly stalled connection. Writers without deadline support (e.g. test
// recorders) report an error here that is safe to ignore.
func (s *sseWriter) touchDeadline() {
	rc := http.NewResponseController(s.w)
	_ = rc.SetWriteDeadline(time.Now().Add(s.writeTimeout))
}

func (s *sseWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// sseErrorPayload builds the mid-stream error payload in the exact same
// envelope shape as every other JSON error in api.md. The message stays
// generic; technical details go to server logs only.
func sseErrorPayload(message string) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":    "AI_PROVIDER_ERROR",
			"message": message,
		},
	}
}

// b64 encodes raw multipart bytes for the JSON-shaped image pipeline.
func b64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
