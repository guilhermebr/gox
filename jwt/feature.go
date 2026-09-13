package jwt

import (
	"context"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/middleware"
)

type key struct{}

// Enable registers the JWT config section and builds the Service from it.
// There is no lifecycle component: a Service is a value.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("JWT", cfg, "jwt.Enable()")
		b.Setup(func(*gox.App) error {
			svc, err := NewFromConfig(*cfg)
			if err != nil {
				return err
			}
			b.Set(key{}, svc)
			return nil
		})
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
