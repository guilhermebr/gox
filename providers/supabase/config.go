package supabase

import (
	"errors"
	"fmt"
	"net/url"
)

// Config is the SUPABASE config section: BILLING_SUPABASE_* under a service
// prefixed BILLING.
type Config struct {
	URL string `conf:"required,help:project URL such as https://xyz.supabase.co"`
	Key string `conf:"required,mask,help:anon or service role key"`
}

// Validate checks the URL is absolute.
func (c *Config) Validate() error {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("SUPABASE_URL must be an absolute http(s) URL, got %q", c.URL)
	}
	if c.Key == "" {
		return errors.New("SUPABASE_KEY is required")
	}
	return nil
}
