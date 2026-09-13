package supabase

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/supabase-community/supabase-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

type key struct{}

// Enable registers the SUPABASE config section and a client component at
// StageClient whose readiness probes the project's auth health endpoint.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("SUPABASE", cfg, "supabase.Enable()")
		b.Component(gox.StageClient, func(a *gox.App) (lifecycle.Component, error) {
			client, err := NewFromConfig(*cfg)
			if err != nil {
				return nil, fmt.Errorf("supabase: %w", err)
			}
			b.Set(key{}, client)
			return &component{cfg: cfg, log: a.Log(), http: &http.Client{Timeout: 5 * time.Second}}, nil
		})
		return nil
	}
}

// From returns the client. It panics if Enable was not passed to gox.New.
func From(a *gox.App) *supabase.Client {
	return gox.MustValue[*supabase.Client](a, key{}, "supabase.From", "supabase.Enable()")
}

type component struct {
	cfg  *Config
	log  *slog.Logger
	http *http.Client
}

func (c *component) Name() string { return "supabase" }

func (c *component) Start(context.Context) error {
	c.log.Info("supabase client configured", slog.String("url", c.cfg.URL))
	return nil
}

func (c *component) Stop(context.Context) error { return nil }

// Ready probes GET /auth/v1/health, which every Supabase project serves.
func (c *component) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.URL, "/")+"/auth/v1/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.cfg.Key)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("supabase: health: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("supabase: health returned %d", resp.StatusCode)
	}
	return nil
}
