// Package httpclient builds the outbound http.Client every gox service uses:
// timeouts and pool limits from config, OpenTelemetry trace propagation,
// request-id propagation, a service User-Agent, and opt-in retries for
// idempotent requests.
//
// A web layer that calls its own API as the signed-in user derives a client
// per request with WithBearer; the shared client is never mutated.
package httpclient

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/guilhermebr/gox/pkg/log"
)

// Config is the client's timeouts and pool limits.
type Config struct {
	Timeout         time.Duration
	MaxIdleConns    int
	IdleConnTimeout time.Duration
}

type options struct {
	userAgent string
	tp        trace.TracerProvider
	prop      propagation.TextMapPropagator
	retries   int
	backoff   time.Duration
	base      http.RoundTripper
}

// Option configures New.
type Option func(*options)

// WithUserAgent sets the User-Agent sent when the request has none.
func WithUserAgent(ua string) Option {
	return func(o *options) { o.userAgent = ua }
}

// WithTracerProvider sets the provider for client spans (default: global).
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(o *options) { o.tp = tp }
}

// WithPropagator sets the propagator that injects trace context (default:
// global).
func WithPropagator(p propagation.TextMapPropagator) Option {
	return func(o *options) { o.prop = p }
}

// WithRetry retries idempotent requests (GET, HEAD, OPTIONS, PUT, DELETE)
// up to attempts times on connection errors, 429, 502, 503 and 504, with
// exponential backoff starting at base. Off unless asked for: some upstreams
// already retry and double retries hurt.
func WithRetry(attempts int, base time.Duration) Option {
	return func(o *options) {
		o.retries = attempts
		o.backoff = base
	}
}

// WithTransport replaces the base transport (for tests or custom TLS).
func WithTransport(rt http.RoundTripper) Option {
	return func(o *options) { o.base = rt }
}

// New builds a client.
func New(cfg Config, opts ...Option) *http.Client {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	rt := o.base
	if rt == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.MaxIdleConns > 0 {
			t.MaxIdleConns = cfg.MaxIdleConns
			t.MaxIdleConnsPerHost = cfg.MaxIdleConns
		}
		if cfg.IdleConnTimeout > 0 {
			t.IdleConnTimeout = cfg.IdleConnTimeout
		}
		rt = t
	}
	if o.retries > 0 {
		rt = &retryTransport{next: rt, attempts: o.retries, backoff: o.backoff}
	}
	otelOpts := []otelhttp.Option{}
	if o.tp != nil {
		otelOpts = append(otelOpts, otelhttp.WithTracerProvider(o.tp))
	}
	if o.prop != nil {
		otelOpts = append(otelOpts, otelhttp.WithPropagators(o.prop))
	}
	rt = otelhttp.NewTransport(rt, otelOpts...)
	rt = &headerTransport{next: rt, userAgent: o.userAgent}
	return &http.Client{Transport: rt, Timeout: cfg.Timeout}
}

// WithBearer returns a copy of c that sends "Authorization: Bearer token".
// c itself is untouched, so a shared client stays shared.
func WithBearer(c *http.Client, token string) *http.Client {
	next := c.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	derived := *c
	derived.Transport = &bearerTransport{next: next, token: token}
	return &derived
}

// headerTransport adds User-Agent and X-Request-ID without mutating the
// caller's request.
type headerTransport struct {
	next      http.RoundTripper
	userAgent string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if t.userAgent != "" && r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", t.userAgent)
	}
	if id := log.RequestID(r.Context()); id != "" && r.Header.Get("X-Request-ID") == "" {
		r.Header.Set("X-Request-ID", id)
	}
	return t.next.RoundTrip(r)
}

type bearerTransport struct {
	next  http.RoundTripper
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(r)
}

type retryTransport struct {
	next     http.RoundTripper
	attempts int
	backoff  time.Duration
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !idempotent(req.Method) || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return t.next.RoundTrip(req)
	}
	backoff := t.backoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		r := req
		if attempt > 1 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r = req.Clone(req.Context())
			r.Body = body
		}
		resp, err := t.next.RoundTrip(r)
		if err == nil && !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		if attempt >= t.attempts {
			return resp, err
		}
		if err == nil {
			_ = resp.Body.Close()
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		lastErr = err
		select {
		case <-req.Context().Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, req.Context().Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}
