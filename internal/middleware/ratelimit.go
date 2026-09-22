package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Abshoor24/backend-go/internal/config"
)

// ipRateLimiter tracks one token-bucket limiter per client IP. Stale entries
// are cleaned up opportunistically to avoid unbounded memory growth.
//
// This is a foundation-grade limiter. TODO: CLIENT REQUIREMENT - decide
// whether per-user / stricter limits or a distributed store (Redis) is needed
// for the final product.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	lastSeen map[string]time.Time
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

func newIPRateLimiter(perMinute, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
		rate:     rate.Limit(float64(perMinute) / 60.0),
		burst:    burst,
		ttl:      5 * time.Minute,
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	lim, ok := l.limiters[ip]
	if !ok {
		lim = rate.NewLimiter(l.rate, l.burst)
		l.limiters[ip] = lim
	}
	l.lastSeen[ip] = time.Now()

	// Opportunistic cleanup when the map grows large.
	if len(l.limiters) > 1024 {
		now := time.Now()
		for k, t := range l.lastSeen {
			if now.Sub(t) > l.ttl {
				delete(l.limiters, k)
				delete(l.lastSeen, k)
			}
		}
	}

	return lim.Allow()
}

// RateLimit rejects requests that exceed the configured per-IP limit with
// HTTP 429. It is applied to the API route group, not to /health, so liveness
// checks are never throttled.
func RateLimit(cfg config.RateLimitConfig) func(http.Handler) http.Handler {
	limiter := newIPRateLimiter(cfg.PerMinute, cfg.Burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !limiter.allow(ip) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	// TODO: only trust X-Forwarded-For when behind a known reverse proxy,
	// otherwise it can be spoofed.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := firstIP(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func firstIP(csv string) string {
	for _, part := range strings.Split(csv, ",") {
		if p := strings.TrimSpace(part); p != "" {
			return p
		}
	}
	return ""
}
