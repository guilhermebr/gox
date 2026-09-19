package workos

import (
	"fmt"
	"net/http"

	sdk "github.com/workos/workos-go/v10"
	"golang.org/x/sync/singleflight"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/config"
)

type (
	clientKey  struct{}
	featureKey struct{}
)

// Option configures Enable.
type Option func(*options)

type options struct {
	sessions bool
	codec    SessionCodec
}

// WithSessions authenticates every request: the sealed session cookie, or a
// bearer access token when the Authorization header carries one. Expired
// access tokens are refreshed once and the cookie rotates. Handlers read
// the result with SessionFrom; RequireSession rejects anonymous requests.
func WithSessions() Option {
	return func(o *options) { o.sessions = true }
}

// WithSessionCodec replaces how the session cookie is sealed, for services
// that share the cookie with an application using another WorkOS SDK.
func WithSessionCodec(c SessionCodec) Option {
	return func(o *options) { o.codec = c }
}

// SessionCodec seals and unseals the session cookie value.
type SessionCodec interface {
	Seal(data *sdk.SessionData, password string) (string, error)
	Unseal(sealed, password string) (*sdk.SessionData, error)
}

// sdkCodec is the layout the WorkOS Go SDK writes.
type sdkCodec struct{}

func (sdkCodec) Seal(d *sdk.SessionData, password string) (string, error) {
	return sdk.SealSession(d, password)
}

func (sdkCodec) Unseal(sealed, password string) (*sdk.SessionData, error) {
	d, err := sdk.Unseal[sdk.SessionData](sealed, password)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

type feature struct {
	cfg        *Config
	client     *sdk.Client
	codec      SessionCodec
	production bool
	refreshes  singleflight.Group
}

// Enable registers the WORKOS config section and builds the API client.
// There is no lifecycle component: the client is a value.
func Enable(opts ...Option) gox.Option {
	return func(b *gox.Builder) error {
		o := options{codec: sdkCodec{}}
		for _, opt := range opts {
			opt(&o)
		}
		cfg := &Config{}
		b.ConfigSection("WORKOS", cfg, "workos.Enable()")
		f := &feature{cfg: cfg, codec: o.codec}
		b.Setup(func(a *gox.App) error {
			if o.sessions && cfg.CookiePassword == "" {
				return fmt.Errorf("%s is required by workos.WithSessions() (at least 32 bytes)", config.EnvName(a.ConfigPrefix(), "WORKOS_COOKIE_PASSWORD"))
			}
			clientOpts := []sdk.ClientOption{sdk.WithClientID(cfg.ClientID)}
			if cfg.BaseURL != "" {
				clientOpts = append(clientOpts, sdk.WithBaseURL(cfg.BaseURL))
			}
			if a.HasHTTPClient() {
				clientOpts = append(clientOpts, sdk.WithHTTPClient(a.HTTPClient()))
			}
			f.client = sdk.NewClient(cfg.APIKey, clientOpts...)
			f.production = a.Config().Environment == "production"
			b.Set(clientKey{}, f.client)
			b.Set(featureKey{}, f)
			return nil
		})
		if o.sessions {
			b.Middleware(func(next http.Handler) http.Handler { return f.sessions(next) })
		}
		return nil
	}
}

// From returns the WorkOS API client. It panics if Enable was not passed to
// gox.New.
func From(a *gox.App) *sdk.Client {
	return gox.MustValue[*sdk.Client](a, clientKey{}, "workos.From", "workos.Enable()")
}

func featureOf(a *gox.App, accessor string) *feature {
	return gox.MustValue[*feature](a, featureKey{}, accessor, "workos.Enable()")
}
