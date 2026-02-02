package supabase

import (
	"fmt"

	"github.com/ardanlabs/conf/v3"
	"github.com/supabase-community/supabase-go"
)

// New creates a new Supabase client from environment variables.
func New(prefix string) (*supabase.Client, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing supabase config from prefix [%s]: %w", prefix, err)
	}

	return NewFromConfig(cfg)
}

// NewFromConfig creates a new Supabase client from a pre-loaded Config.
func NewFromConfig(cfg Config) (*supabase.Client, error) {
	client, err := supabase.NewClient(cfg.URL, cfg.Key, &supabase.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create supabase client: %w", err)
	}
	return client, nil
}
