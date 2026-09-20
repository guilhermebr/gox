// Package posthog plugs the PostHog client into a gox service: product
// analytics and feature flags, with queued events flushed on shutdown.
package posthog

import (
	"context"
	"fmt"
	"net/url"

	sdk "github.com/posthog/posthog-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

// Config is the POSTHOG config section: BILLING_POSTHOG_* under a service
// prefixed BILLING.
type Config struct {
	ProjectKey string `conf:"required,help:project API key (phc_...); it is public and also used by the browser SDK"`
	Host       string `conf:"default:https://us.i.posthog.com,help:ingestion host; https://eu.i.posthog.com for the EU or your own"`
	SecretKey  string `conf:"mask,help:secret key for evaluating feature flags locally; optional"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	if u, err := url.Parse(c.Host); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("POSTHOG_HOST must be an absolute http(s) URL, got %q", c.Host)
	}
	return nil
}

type key struct{}

// Enable registers the POSTHOG config section and a component that owns
// the client: events are queued and sent in batches, and Stop flushes what
// is still queued so a deploy does not lose them.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("POSTHOG", cfg, "posthog.Enable()")
		b.Component(gox.StageClient, func(*gox.App) (lifecycle.Component, error) {
			client, err := sdk.NewWithConfig(cfg.ProjectKey, sdk.Config{Endpoint: cfg.Host, SecretKey: cfg.SecretKey})
			if err != nil {
				return nil, fmt.Errorf("posthog: %w", err)
			}
			b.Set(key{}, client)
			return &component{client: client}, nil
		})
		return nil
	}
}

// From returns the PostHog client: Enqueue for events, and the feature flag
// methods. It panics if Enable was not passed to gox.New.
func From(a *gox.App) sdk.Client {
	return gox.MustValue[sdk.Client](a, key{}, "posthog.From", "posthog.Enable()")
}

type component struct{ client sdk.Client }

func (c *component) Name() string { return "posthog" }

func (c *component) Start(context.Context) error { return nil }

// Stop flushes the queue. Close blocks until the batch is sent, so it runs
// beside the stop deadline.
func (c *component) Stop(ctx context.Context) error {
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
