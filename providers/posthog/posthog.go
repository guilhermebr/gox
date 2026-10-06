// Package posthog plugs the PostHog client into a gox service: product
// analytics and feature flags, with queued events flushed on shutdown.
//
// Without a project key the client is off: From returns the SDK's no-op
// client, whose Enqueue drops the event and returns sdk.ErrSDKDisabled and
// whose flag lookups answer false. A service wires analytics the same way in
// every environment and turns it on with one variable.
package posthog

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	sdk "github.com/posthog/posthog-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

// Config is the POSTHOG config section: BILLING_POSTHOG_* under a service
// prefixed BILLING.
type Config struct {
	ProjectKey string `conf:"help:project API key (phc_...); it is public and also used by the browser SDK; empty turns the client off and events are dropped"`
	Host       string `conf:"default:https://us.i.posthog.com,help:ingestion host; https://eu.i.posthog.com for the EU or your own"`
	SecretKey  string `conf:"mask,help:secret key for evaluating feature flags locally; optional"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	if u, err := url.Parse(c.Host); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("POSTHOG_HOST must be an absolute http(s) URL, got %q", c.Host)
	}
	if c.SecretKey != "" && strings.TrimSpace(c.ProjectKey) == "" {
		return fmt.Errorf("POSTHOG_SECRET_KEY is set but POSTHOG_PROJECT_KEY is empty: set the project key or drop the secret")
	}
	return nil
}

type key struct{}

// Enable registers the POSTHOG config section and a component that owns
// the client: events are queued and sent in batches, and Stop flushes what
// is still queued so a deploy does not lose them. With no
// POSTHOG_PROJECT_KEY the client is the SDK's no-op one (logged once at
// start).
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("POSTHOG", cfg, "posthog.Enable()")
		b.Component(gox.StageClient, func(a *gox.App) (lifecycle.Component, error) {
			log := a.Log().With("component", "posthog")
			off := strings.TrimSpace(cfg.ProjectKey) == ""
			var lg sdk.Logger = logger{log}
			if off {
				lg = logger{slog.New(slog.DiscardHandler)} // the SDK would report the empty key as an error
			}
			client, err := sdk.NewWithConfig(cfg.ProjectKey, sdk.Config{Endpoint: cfg.Host, SecretKey: cfg.SecretKey, Logger: lg})
			if err != nil {
				return nil, fmt.Errorf("posthog: %w", err)
			}
			b.Set(key{}, client)
			return &component{client: client, off: off, log: log}, nil
		})
		return nil
	}
}

// From returns the PostHog client: Enqueue for events, and the feature flag
// methods. It panics if Enable was not passed to gox.New.
func From(a *gox.App) sdk.Client {
	return gox.MustValue[sdk.Client](a, key{}, "posthog.From", "posthog.Enable()")
}

type component struct {
	client sdk.Client
	off    bool
	log    *slog.Logger
}

func (c *component) Name() string { return "posthog" }

func (c *component) Start(ctx context.Context) error {
	if c.off {
		c.log.InfoContext(ctx, "posthog off: POSTHOG_PROJECT_KEY is empty; events are dropped")
	}
	return nil
}

// Stop flushes the queue. Close blocks until the batch is sent, so it runs
// beside the stop deadline. An off client has nothing to flush.
func (c *component) Stop(ctx context.Context) error {
	if c.off {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.client.Close() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("posthog: flush: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("posthog: flush: %w", ctx.Err())
	}
}

// logger routes the SDK's messages to the service's logger, so they carry
// the component and follow LOG_LEVEL and LOG_FORMAT.
type logger struct{ l *slog.Logger }

func (g logger) Debugf(format string, args ...any) { g.l.Debug(fmt.Sprintf(format, args...)) }
func (g logger) Logf(format string, args ...any)   { g.l.Info(fmt.Sprintf(format, args...)) }
func (g logger) Warnf(format string, args ...any)  { g.l.Warn(fmt.Sprintf(format, args...)) }
func (g logger) Errorf(format string, args ...any) { g.l.Error(fmt.Sprintf(format, args...)) }
