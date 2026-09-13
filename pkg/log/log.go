// Package log builds the slog.Logger every gox service uses: JSON in
// production, text in development, and request-scoped fields stamped on every
// record from the context. The standard field keys defined here are a
// contract with dashboards and must not be renamed casually.
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Standard field keys.
const (
	KeyRequestID = "request_id"
	KeyTraceID   = "trace_id"
	KeySpanID    = "span_id"
	KeyError     = "error"
	KeyComponent = "component"
	KeyDuration  = "duration"
)

// Config selects level and format. Format "auto" means JSON when Environment
// is "production" and text otherwise.
type Config struct {
	Level       string
	Format      string
	Environment string
}

// ContextAttrs extracts attributes from a context. pkg/otel registers one to
// stamp trace and span ids.
type ContextAttrs func(ctx context.Context) []slog.Attr

type options struct {
	w     io.Writer
	hooks []ContextAttrs
}

// Option configures New.
type Option func(*options)

// WithWriter sets the output (default os.Stderr).
func WithWriter(w io.Writer) Option {
	return func(o *options) { o.w = w }
}

// WithContextAttrs adds a hook that contributes attributes from the context
// of every record.
func WithContextAttrs(fn ContextAttrs) Option {
	return func(o *options) { o.hooks = append(o.hooks, fn) }
}

// New builds a logger from cfg. It does not install it as the slog default;
// the root package does that once.
func New(cfg Config, opts ...Option) (*slog.Logger, error) {
	o := options{w: os.Stderr}
	for _, opt := range opts {
		opt(&o)
	}
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	format, err := resolveFormat(cfg.Format, cfg.Environment)
	if err != nil {
		return nil, err
	}
	hopts := &slog.HandlerOptions{Level: level}
	var base slog.Handler
	if format == "json" {
		base = slog.NewJSONHandler(o.w, hopts)
	} else {
		base = slog.NewTextHandler(o.w, hopts)
	}
	return slog.New(&contextHandler{next: base, hooks: o.hooks}), nil
}

// ParseLevel converts "debug", "info", "warn" or "error" to a slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("log: invalid level %q (use debug, info, warn or error)", s)
}

func resolveFormat(format, environment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "auto":
		if environment == "production" {
			return "json", nil
		}
		return "text", nil
	case "json":
		return "json", nil
	case "text":
		return "text", nil
	}
	return "", fmt.Errorf("log: invalid format %q (use auto, json or text)", format)
}

type ctxKey int

const (
	loggerKey ctxKey = iota
	requestIDKey
)

// WithContext stores a logger in ctx.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the logger stored by WithContext, or slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID stores a request id in ctx. Every record logged with that
// context carries it under KeyRequestID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request id stored by WithRequestID, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// contextHandler stamps request-scoped attributes from the context onto every
// record before delegating.
type contextHandler struct {
	next  slog.Handler
	hooks []ContextAttrs
}

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx == nil {
		return h.next.Handle(ctx, r)
	}
	var attrs []slog.Attr
	if id := RequestID(ctx); id != "" {
		attrs = append(attrs, slog.String(KeyRequestID, id))
	}
	for _, hook := range h.hooks {
		attrs = append(attrs, hook(ctx)...)
	}
	if len(attrs) == 0 {
		return h.next.Handle(ctx, r)
	}
	r = r.Clone()
	r.AddAttrs(attrs...)
	return h.next.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs), hooks: h.hooks}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name), hooks: h.hooks}
}
