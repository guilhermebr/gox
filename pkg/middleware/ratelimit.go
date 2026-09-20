package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// RateLimitOption configures RateLimit.
type RateLimitOption func(*limiter)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) RateLimitOption {
	return func(l *limiter) { l.now = now }
}

// RateLimit allows limit requests per window for each key (a token bucket:
// a burst of limit, refilled evenly), and answers the rest with a 429
// envelope and a Retry-After header. State is in memory, per process: with
// N replicas the effective limit is up to N times higher, which is the right
// trade for protecting a login or a signup form without another datastore.
func RateLimit(limit int, window time.Duration, key func(*http.Request) string, opts ...RateLimitOption) Middleware {
	l := &limiter{limit: float64(limit), refill: float64(limit) / window.Seconds(), window: window, now: time.Now, buckets: map[string]*bucket{}}
	for _, opt := range opts {
		opt(l)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wait := l.take(key(r)); wait > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
				status, env := errors.ToEnvelope(errors.ResourceExhausted("too many requests; try again shortly"), log.RequestID(r.Context()))
				httpx.WriteError(w, r, status, env)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type bucket struct {
	tokens float64
	seen   time.Time
}

type limiter struct {
	limit, refill float64 // refill is tokens per second
	window        time.Duration
	now           func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

// take spends a token for key, or returns how long until one is available.
func (l *limiter) take(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.swept) > l.window { // full buckets carry no information: drop them
		for k, b := range l.buckets {
			if now.Sub(b.seen) > l.window {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.limit, seen: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.limit, b.tokens+now.Sub(b.seen).Seconds()*l.refill)
	b.seen = now
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	return time.Duration((1 - b.tokens) / l.refill * float64(time.Second))
}
