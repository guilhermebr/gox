package supabase

import (
	"fmt"

	"github.com/ardanlabs/conf/v3"
	"github.com/supabase-community/supabase-go"
)

// New creates a new Supabase client.
func New(prefix string) (*supabase.Client, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing supabase config from prefix [%s]: %w", prefix, err)
	}
	client, err := supabase.NewClient(cfg.URL, cfg.Key, &supabase.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create supabase client: %w", err)
	}
	return client, nil
}
