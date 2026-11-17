package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
)

// CacheStore persists validated HTML keyed by an exact brief hash.
type CacheStore interface {
	GetHTML(ctx context.Context, hash string) (string, bool, error)
	PutHTML(ctx context.Context, hash, html string) error
}

// briefHash returns a stable SHA-256 hash of the canonical brief JSON. Exactly
// equal briefs produce the same hash; any field change produces a new one.
func briefHash(b model.Brief) string {
	data, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// cacheable reports whether this request may use the exact-brief cache.
// Revise depends on the base HTML, and images change the output, so both are
// excluded to avoid returning a page that does not match the request.
func (p *PendingStream) cacheable() bool {
	return p != nil && p.Kind == model.KindGenerate && p.briefHash != "" && len(p.providerImages) == 0
}

// tryCachedStream attaches a synthetic stream carrying the stored HTML when
// the exact brief has a validated cached result. No provider request is made.
func (s *service) tryCachedStream(ctx context.Context, pending *PendingStream) bool {
	if s.cacheStore == nil || !pending.cacheable() {
		return false
	}
	html, ok, err := s.cacheStore.GetHTML(ctx, pending.briefHash)
	if err != nil {
		s.logger.Warn("failed to read HTML cache", "error", err)
		return false
	}
	if !ok || html == "" {
		return false
	}
	pending.Stream = cachedChunkStream(html)
	pending.CacheHit = true
	pending.Model = "cache"
	pending.ThinkingLevel = "none"
	pending.OpenDuration = 0
	return true
}

func cachedChunkStream(html string) <-chan aiprovider.StreamChunk {
	ch := make(chan aiprovider.StreamChunk, 2)
	ch <- aiprovider.StreamChunk{Token: html}
	ch <- aiprovider.StreamChunk{Done: true, FinishReason: "STOP"}
	close(ch)
	return ch
}

// storeValidatedHTML records a successful generate result for exact-brief
// reuse. Revise results and image-assisted results are never cached.
func (s *service) storeValidatedHTML(ctx context.Context, pending *PendingStream, doc string) {
	if s.cacheStore == nil || !pending.cacheable() {
		return
	}
	if err := s.cacheStore.PutHTML(ctx, pending.briefHash, doc); err != nil {
		s.logger.Warn("failed to write HTML cache", "error", err)
	}
}
