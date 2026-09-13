package supabase

import (
	"fmt"
	"net/url"

	"github.com/ardanlabs/conf/v3"
	"github.com/supabase-community/supabase-go"
)

// New creates a client from <PREFIX>_SUPABASE_URL and <PREFIX>_SUPABASE_KEY.
//
// Deprecated: pass supabase.Enable() to gox.New and read the client with
// supabase.From. New is removed one minor version after the root package
// reaches v1.
func New(prefix string) (*supabase.Client, error) {
	var lc struct {
		URL string `conf:"env:SUPABASE_URL,required"`
		Key string `conf:"env:SUPABASE_KEY,required,mask"`
	}
	if _, err := conf.Parse(prefix, &lc); err != nil {
		return nil, fmt.Errorf("parsing supabase config from prefix [%s]: %w", prefix, err)
	}
	return NewFromConfig(Config{URL: lc.URL, Key: lc.Key})
}

// NewFromConfig creates a new Supabase client from a pre-loaded Config.
func NewFromConfig(cfg Config) (*supabase.Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid supabase URL %q: must be an absolute http(s) URL", cfg.URL)
	}

	client, err := supabase.NewClient(cfg.URL, cfg.Key, &supabase.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create supabase client: %w", err)
	}
	return client, nil
}
