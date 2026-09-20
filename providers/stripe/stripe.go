// Package stripe plugs the Stripe API client into a gox service and
// verifies the webhooks Stripe sends.
package stripe

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"

	"github.com/guilhermebr/gox"
)

// Config is the STRIPE config section: BILLING_STRIPE_* under a service
// prefixed BILLING.
type Config struct {
	APIKey        string `conf:"required,mask,help:secret or restricted API key (sk_... or rk_...)"`
	WebhookSecret string `conf:"mask,help:signing secret of the webhook endpoint (whsec_...); required by VerifyWebhook"`
	BaseURL       string `conf:"help:API base URL; leave empty for api.stripe.com (set it for stripe-mock)"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return nil
	}
	if u, err := url.Parse(c.BaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("STRIPE_BASE_URL must be an absolute http(s) URL, got %q", c.BaseURL)
	}
	return nil
}

type (
	clientKey struct{}
	configKey struct{}
)

// Enable registers the STRIPE config section and builds the API client on
// the app's outbound HTTP client when gox.HTTPClient() is declared. There
// is no lifecycle component: the client is a value.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("STRIPE", cfg, "stripe.Enable()")
		b.Setup(func(a *gox.App) error {
			var opts []sdk.ClientOption
			if cfg.BaseURL != "" || a.HasHTTPClient() {
				bc := &sdk.BackendConfig{}
				if cfg.BaseURL != "" {
					bc.URL = sdk.String(strings.TrimRight(cfg.BaseURL, "/"))
				}
				if a.HasHTTPClient() {
					bc.HTTPClient = a.HTTPClient()
				}
				opts = append(opts, sdk.WithBackends(sdk.NewBackendsWithConfig(bc)))
			}
			b.Set(clientKey{}, sdk.NewClient(cfg.APIKey, opts...))
			b.Set(configKey{}, cfg)
			return nil
		})
		return nil
	}
}

// From returns the Stripe API client. It panics if Enable was not passed to
// gox.New.
func From(a *gox.App) *sdk.Client {
	return gox.MustValue[*sdk.Client](a, clientKey{}, "stripe.From", "stripe.Enable()")
}

// VerifyWebhook reads the request body, checks the Stripe-Signature header
// against STRIPE_WEBHOOK_SECRET (five-minute tolerance) and returns the
// event. Events from another API version than the SDK's are accepted,
// because an endpoint keeps the version it was created with: read
// event.Data.Raw when the shapes differ. A bad signature is an
// unauthenticated error.
func VerifyWebhook(a *gox.App, r *http.Request) (sdk.Event, error) {
	cfg := gox.MustValue[*Config](a, configKey{}, "stripe.VerifyWebhook", "stripe.Enable()")
	if cfg.WebhookSecret == "" {
		return sdk.Event{}, gox.Internal("STRIPE_WEBHOOK_SECRET is not configured")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return sdk.Event{}, gox.WrapError(err, gox.CodeInvalidArgument, "could not read the webhook body")
	}
	ev, err := webhook.ConstructEventWithOptions(body, r.Header.Get("Stripe-Signature"), cfg.WebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return sdk.Event{}, gox.WrapError(err, gox.CodeUnauthenticated, "the Stripe signature is not valid")
	}
	return ev, nil
}
