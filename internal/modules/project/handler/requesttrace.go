package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/middleware"
)

const duplicateGenerateWindow = 2 * time.Second

type activeGenerate struct {
	requestID string
	started   time.Time
}

type lastGenerate struct {
	requestID string
	ended     time.Time
}

// requestTracker counts in-flight generate requests per project and remembers
// when the previous one ended so likely client/proxy retries can be logged.
// It never rejects a request.
type requestTracker struct {
	mu     sync.Mutex
	active map[string][]activeGenerate
	last   map[string]lastGenerate
}

func newRequestTracker() *requestTracker {
	return &requestTracker{
		active: make(map[string][]activeGenerate),
		last:   make(map[string]lastGenerate),
	}
}

type requestTrace struct {
	requestID string
	kind      string
	projectID string
	started   time.Time
	tracked   bool
}

// beginRequestTrace logs the incoming generate/revise request and returns the
// handle used by finishRequestTrace. activeGenerate is the number of generate
// requests already running for the same project before this one.
func (h *Handler) beginRequestTrace(r *http.Request, kind, projectID string) *requestTrace {
	id := middleware.GetRequestID(r.Context())
	if id == "" {
		id = shortRequestID()
	}
	tr := &requestTrace{requestID: id, kind: kind, projectID: projectID, started: time.Now()}

	var activeGenerate int
	var duplicate bool
	var previousID string
	var gap time.Duration

	if h.trace != nil {
		activeGenerate, duplicate, previousID, gap = h.trace.begin(id, kind, projectID, tr.started)
		tr.tracked = kind == "generate"
	}

	attrs := []any{
		"request_id", id,
		"kind", kind,
		"method", r.Method,
		"path", r.URL.Path,
		"project_id", projectID,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
		"referer", r.Referer(),
		"x_forwarded_for", r.Header.Get("X-Forwarded-For"),
		"active_generate", activeGenerate,
	}
	h.logger.Info("ai request started", attrs...)

	if duplicate {
		h.logger.Warn("duplicate_generate_suspected",
			"request_id", id,
			"previous_request_id", previousID,
			"project_id", projectID,
			"gap_ms", gap.Milliseconds(),
			"active_generate", activeGenerate,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
			"x_forwarded_for", r.Header.Get("X-Forwarded-For"),
		)
	}
	return tr
}

func (h *Handler) finishRequestTrace(tr *requestTrace, outcome string) {
	if tr == nil {
		return
	}
	activeAfter := 0
	if h.trace != nil {
		activeAfter = h.trace.finish(tr.requestID, tr.kind, tr.projectID, time.Now())
	}
	h.logger.Info("ai request finished",
		"request_id", tr.requestID,
		"kind", tr.kind,
		"project_id", tr.projectID,
		"outcome", outcome,
		"duration_ms", time.Since(tr.started).Milliseconds(),
		"active_generate", activeAfter,
	)
}

func (t *requestTracker) begin(id, kind, projectID string, now time.Time) (int, bool, string, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	active := len(t.active[projectID])
	if kind != "generate" {
		return active, false, "", 0
	}

	var duplicate bool
	var previousID string
	var gap time.Duration
	switch {
	case active > 0:
		duplicate = true
		previousID = t.active[projectID][0].requestID
		gap = now.Sub(t.active[projectID][0].started)
	case !t.last[projectID].ended.IsZero() && now.Sub(t.last[projectID].ended) < duplicateGenerateWindow:
		duplicate = true
		previousID = t.last[projectID].requestID
		gap = now.Sub(t.last[projectID].ended)
	}

	t.active[projectID] = append(t.active[projectID], activeGenerate{requestID: id, started: now})
	return active, duplicate, previousID, gap
}

func (t *requestTracker) finish(id, kind, projectID string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if kind != "generate" {
		return len(t.active[projectID])
	}
	runs := t.active[projectID]
	for i, run := range runs {
		if run.requestID == id {
			runs = append(runs[:i], runs[i+1:]...)
			break
		}
	}
	if len(runs) == 0 {
		delete(t.active, projectID)
	} else {
		t.active[projectID] = runs
	}
	t.last[projectID] = lastGenerate{requestID: id, ended: now}
	return len(runs)
}

func shortRequestID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-" + time.Now().Format("150405.000")
	}
	return hex.EncodeToString(b[:])
}

func errorOutcome(err error) string {
	var ae *apperror.Error
	if errors.As(err, &ae) {
		if code := ae.Code(); code != "" {
			return "error:" + code
		}
	}
	return "error"
}
