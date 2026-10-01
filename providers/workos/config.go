package workos

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
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
	Issuer         string        `conf:"help:trusted access-token issuer; empty is <BASE_URL or https://api.workos.com>/user_management/<CLIENT_ID>; set it to the tokens' iss for a custom auth domain or a non-default application or https://api.workos.com/ for the old default"`
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
	for name, v := range map[string]string{"WORKOS_REDIRECT_URI": c.RedirectURI, "WORKOS_BASE_URL": c.BaseURL, "WORKOS_ISSUER": c.Issuer} {
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

// issuer is the issuer access tokens are verified against: the configured
// one, or the one AuthKit signs with, <API base URL>/user_management/<client
// id>. A token from any other issuer is rejected. The derived value is wrong
// for a non-default application of a multi-application environment (its
// tokens name the default application's client id), behind a custom auth
// domain, and when BASE_URL is a proxy rather than an emulator: set
// WORKOS_ISSUER to the tokens' iss there, which a rejected session logs as
// token_issuer.
func (c *Config) issuer() string {
	if c.Issuer != "" {
		return c.Issuer
	}
	base := "https://api.workos.com"
	if c.BaseURL != "" {
		base = strings.TrimRight(c.BaseURL, "/")
	}
	return base + "/user_management/" + c.ClientID
}
