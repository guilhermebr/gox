package web

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config is the WEB config section: <PREFIX>_WEB_*.
type Config struct {
	SessionSecret string        `conf:"mask,help:key for the session cookie; at least 32 bytes; required by WithSessions"`
	SessionName   string        `conf:"default:session"`
	SessionMaxAge time.Duration `conf:"default:720h"`
	SecureCookies string        `conf:"default:auto,help:auto | true | false; auto is on in production or behind X-Forwarded-Proto https"`
	StaticPrefix  string        `conf:"default:/static/"`
	BackendURL    string        `conf:"help:base URL of the API this UI calls as the signed-in user; required by WithBackend"`
}

// Validate checks the section. Session and backend requirements are
// checked by Enable, which knows which options were passed.
func (c *Config) Validate() error {
	var errs []error
	switch strings.ToLower(c.SecureCookies) {
	case "auto", "true", "false":
	default:
		errs = append(errs, fmt.Errorf("WEB_SECURE_COOKIES must be auto, true or false, got %q", c.SecureCookies))
	}
	if !strings.HasPrefix(c.StaticPrefix, "/") || !strings.HasSuffix(c.StaticPrefix, "/") {
		errs = append(errs, fmt.Errorf("WEB_STATIC_PREFIX must start and end with /, got %q", c.StaticPrefix))
	}
	if c.SessionName == "" {
		errs = append(errs, errors.New("WEB_SESSION_NAME must not be empty"))
	}
	if c.SessionMaxAge <= 0 {
		errs = append(errs, fmt.Errorf("WEB_SESSION_MAX_AGE must be > 0, got %v", c.SessionMaxAge))
	}
	if c.BackendURL != "" {
		if u, err := url.Parse(c.BackendURL); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("WEB_BACKEND_URL must be an absolute URL, got %q", c.BackendURL))
		}
	}
	return errors.Join(errs...)
}
