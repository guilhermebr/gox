package workos

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Config is the WORKOS config section: BILLING_WORKOS_* under a service
// prefixed BILLING.
type Config struct {
	APIKey         string        `conf:"required,mask,help:WorkOS API key (sk_...)"`
	ClientID       string        `conf:"required,help:WorkOS client id (client_...)"`
	RedirectURI    string        `conf:"help:absolute URL of the route serving workos.Callback; required by workos.Login"`
	CookiePassword string        `conf:"mask,help:key that seals the session cookie; at least 32 bytes; required by WithSessions"`
	CookieName     string        `conf:"default:wos_session"`
	CookieMaxAge   time.Duration `conf:"default:720h"`
	SecureCookies  string        `conf:"default:auto,help:auto | true | false; auto is on in production or behind X-Forwarded-Proto https"`
	BaseURL        string        `conf:"help:API base URL; leave empty for api.workos.com (set it for an emulator)"`
	Issuer         string        `conf:"default:https://api.workos.com/,help:trusted access-token issuer; change it for a custom auth domain or an emulator"`
	WebhookSecret  string        `conf:"mask,help:signing secret of the webhook endpoint; required by workos.VerifyWebhook"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	var errs []error
	if c.CookiePassword != "" && len(c.CookiePassword) < 32 {
		errs = append(errs, errors.New("WORKOS_COOKIE_PASSWORD must be at least 32 bytes"))
	}
	switch c.SecureCookies {
	case "auto", "true", "false":
	default:
		errs = append(errs, fmt.Errorf("WORKOS_SECURE_COOKIES must be auto, true or false, got %q", c.SecureCookies))
	}
	for name, v := range map[string]string{"WORKOS_REDIRECT_URI": c.RedirectURI, "WORKOS_BASE_URL": c.BaseURL} {
		if v == "" {
			continue
		}
		if u, err := url.Parse(v); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("%s must be an absolute http(s) URL, got %q", name, v))
		}
	}
	if c.CookieMaxAge <= 0 {
		errs = append(errs, fmt.Errorf("WORKOS_COOKIE_MAX_AGE must be > 0, got %v", c.CookieMaxAge))
	}
	return errors.Join(errs...)
}
