package jwt

import (
	"context"
	"net/http"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/middleware"
)

type key struct{}

// Option configures Enable.
type Option func(*options)

type options struct {
	auth bool
}

// WithAuth protects every route with Auth, except /healthz and /readyz so
// platform probes keep working. For finer control leave it off and wrap
// the handlers that need it with Auth(From(a)).
func WithAuth() Option {
	return func(o *options) { o.auth = true }
}

// Enable registers the JWT config section and builds the Service from it.
// There is no lifecycle component: a Service is a value.
func Enable(opts ...Option) gox.Option {
	return func(b *gox.Builder) error {
		o := options{}
		for _, opt := range opts {
			opt(&o)
		}
		cfg := &Config{}
		b.ConfigSection("JWT", cfg, "jwt.Enable()")
		var svc *Service
		b.Setup(func(*gox.App) error {
			s, err := NewFromConfig(*cfg)
			if err != nil {
				return err
			}
			svc = s
			b.Set(key{}, s)
			return nil
		})
		if o.auth {
			b.Middleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
						next.ServeHTTP(w, r)
						return
					}
					Auth(svc)(next).ServeHTTP(w, r)
				})
			})
		}
		return nil
	}
}

// From returns the Service. It panics if Enable was not passed to gox.New.
func From(a *gox.App) *Service {
	return gox.MustValue[*Service](a, key{}, "jwt.From", "jwt.Enable()")
}

// Auth is the middleware for gox.WithAuth: it validates the bearer token
// with svc and stores the claims for ClaimsFromContext. Failures are 401
// envelopes.
func Auth(svc *Service) gox.Middleware {
	return middleware.Bearer(func(_ context.Context, token string) (any, error) {
		return svc.ValidateToken(token)
	})
}

// ClaimsFromContext returns the claims Auth stored for this request.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := middleware.Principal(ctx).(*Claims)
	return c, ok && c != nil
}
