package gox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/guilhermebr/gox/pkg/lifecycle"
	"github.com/guilhermebr/gox/pkg/sse"
)

type sseKey struct{}

// SSEHub fans server-sent events out to subscribers; get it with a.SSE().
type SSEHub = sse.Hub

// SSEUpdate is one event published to the subscribers of its topics.
type SSEUpdate = sse.Update

// SSEResyncEvent is the event type a subscriber receives when updates meant
// for it are gone (its Last-Event-ID left the history, or the transport
// lost updates while reconnecting): the client should refetch what it
// shows.
const SSEResyncEvent = sse.ResyncEvent

// SSETransport carries published updates to every replica of the service;
// postgres.SSETransport is one.
type SSETransport = sse.Transport

// SSEConfig configures WithSSE. Zero values take the defaults noted per
// field.
type SSEConfig struct {
	// Path subscribers connect to with GET ?topic=. Default: /events.
	Path string
	// Authorize decides which private updates a subscriber receives: an
	// update with Private set reaches the subscription made by r only if
	// Authorize returns true for one of its topics. It sees the request with
	// everything the chain put there (jwt.ClaimsFromContext, web.SessionFrom);
	// answers are cached per topic for a minute. Without it, private updates
	// reach no one. Who may subscribe at all is the chain's business
	// (WithAuth), checked again on every reconnect (see MaxAge).
	Authorize func(r *http.Request, topic string) bool
	// Transport fans updates out across replicas, built after the
	// datastores so it can use them: pass postgres.SSETransport. Default:
	// none, updates reach only this process's subscribers.
	Transport func(a *App) (SSETransport, error)
	// Heartbeat is the interval of the keep-alive comment that stops
	// proxies closing idle streams. Default: 15s.
	Heartbeat time.Duration
	// WriteTimeout bounds each write to a subscriber; a client that stalls
	// longer is disconnected. Default: 10s.
	WriteTimeout time.Duration
	// MaxAge ends a stream after this long (less up to 10% jitter); the
	// client reconnects with Last-Event-ID through the middleware chain
	// again, so an expired token or a closed session stops the stream.
	// Default: 15m; negative keeps streams open indefinitely.
	MaxAge time.Duration
	// Buffer is the number of updates queued per subscriber; one that falls
	// further behind is disconnected and resumes with Last-Event-ID.
	// Default: 64.
	Buffer int
	// History is the number of recent updates kept in memory for
	// Last-Event-ID replay. Default: 1000; negative disables replay.
	History int
	// MaxTopics caps the topic selectors of one subscription. Default: 32.
	MaxTopics int
	// MaxSubscribers caps the open streams of this process; more are
	// refused with 503 and Retry-After. Default: 10000; negative means no
	// cap.
	MaxSubscribers int
}

// WithSSE serves topic-based server-sent events, in the spirit of Mercure:
// clients subscribe with GET <Path>?topic=… (an exact topic, a template
// such as /orders/{id} or /files/{path...}, or *), the service publishes
// with a.SSE().Publish, and a reconnecting client resumes from
// Last-Event-ID, or gets an SSEResyncEvent when what it missed is gone.
// The subscription route alone is exempt from
// HTTP_REQUEST_TIMEOUT, HTTP_READ_TIMEOUT and HTTP_WRITE_TIMEOUT; on
// shutdown every stream ends so the server can drain. It enables a.SSE.
func WithSSE(cfg SSEConfig) HTTPOption {
	return func(o *httpOptions) { o.sse = &cfg }
}

// SSE returns the server-sent events hub. It panics if WithSSE was not
// passed to HTTP().
func (a *App) SSE() *SSEHub {
	return MustValue[*SSEHub](a, sseKey{}, "a.SSE", "gox.WithSSE()")
}

func newSSE(a *App, b *Builder, cfg SSEConfig) (lifecycle.Component, error) {
	var t SSETransport
	if cfg.Transport != nil {
		var err error
		if t, err = cfg.Transport(a); err != nil {
			return nil, err
		}
		if t == nil {
			return nil, errors.New("WithSSE: SSEConfig.Transport returned a nil transport")
		}
	}
	hub, err := sse.New(sse.Config{
		Path:           cfg.Path,
		Authorize:      cfg.Authorize,
		Heartbeat:      cfg.Heartbeat,
		WriteTimeout:   cfg.WriteTimeout,
		Buffer:         cfg.Buffer,
		MaxAge:         cfg.MaxAge,
		History:        cfg.History,
		MaxTopics:      cfg.MaxTopics,
		MaxSubscribers: cfg.MaxSubscribers,
	}, t, a.log.With(slog.String("component", "sse")))
	if err != nil {
		return nil, fmt.Errorf("WithSSE: %w", err)
	}
	b.Set(sseKey{}, hub)
	return &sseComponent{hub: hub}, nil
}

// sseComponent runs the hub. Runners are canceled before any component
// stops, so streams end before the HTTP server starts draining.
type sseComponent struct{ hub *SSEHub }

func (c *sseComponent) Name() string                { return "sse" }
func (c *sseComponent) Start(context.Context) error { return nil }
func (c *sseComponent) Stop(context.Context) error  { return nil }
func (c *sseComponent) Run(ctx context.Context) error {
	return c.hub.Run(ctx)
}
