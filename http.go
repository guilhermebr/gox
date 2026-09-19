package gox

import (
	"fmt"
	"net/http"

	"github.com/guilhermebr/gox/pkg/httpclient"
	"github.com/guilhermebr/gox/pkg/httpserver"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/lifecycle"
	"github.com/guilhermebr/gox/pkg/middleware"
)

// Middleware wraps an http.Handler. Handlers are plain net/http.
type Middleware = middleware.Middleware

// CORSConfig configures WithCORS.
type CORSConfig = middleware.CORSConfig

// HTTPOption configures the public HTTP server declared with HTTP().
type HTTPOption func(*httpOptions)

type httpOptions struct {
	cors *CORSConfig
}

// WithCORS enables CORS for the given configuration. It is never on by
// default.
func WithCORS(cfg CORSConfig) HTTPOption {
	return func(o *httpOptions) { o.cors = &cfg }
}

type (
	muxKey        struct{}
	httpClientKey struct{}
)

// HTTP declares the public HTTP server: a net/http ServeMux behind the
// default middleware chain, /healthz and /readyz on the public port, and
// 404/405 rendered as the error envelope. It enables a.Mux and a.HandleFunc.
//
// Chain: error renderers → route capture → recovery → request id → tracing → metrics →
// logging → timeout → max bytes → security headers → CORS (WithCORS) →
// feature middleware (Builder.Middleware) → auth (WithAuth) →
// WithMiddleware → mux. Timeouts and the body limit come from HTTP_* config.
func HTTP(opts ...HTTPOption) Option {
	return func(b *Builder) error {
		if b.http != nil {
			return fmt.Errorf("HTTP() declared twice")
		}
		o := &httpOptions{}
		for _, opt := range opts {
			opt(o)
		}
		b.http = o
		b.Component(StageServer, func(a *App) (lifecycle.Component, error) {
			cfg := a.cfg.HTTP
			metrics, err := middleware.Metrics(a.otel.Meter)
			if err != nil {
				return nil, err
			}
			mux := httpserver.NewMux()
			chain := []Middleware{
				withErrorRenderers(b.renderers),
				middleware.RouteCapture(func(r *http.Request) string {
					_, pattern := mux.Handler(r)
					return pattern
				}),
				middleware.Recovery(a.log),
				middleware.RequestID(),
				middleware.Tracing(a.otel.Tracer, a.otel.Propagator),
				metrics,
				middleware.Logging(a.log),
				withMappers(a),
				middleware.Timeout(cfg.RequestTimeout),
				middleware.MaxBytes(cfg.MaxBodyBytes),
				middleware.SecurityHeaders(),
			}
			if o.cors != nil {
				chain = append(chain, middleware.CORS(*o.cors))
			}
			chain = append(chain, b.features...)
			if b.auth != nil {
				chain = append(chain, b.auth)
			}
			chain = append(chain, b.middleware...)

			httpserver.RegisterHealth(mux, a.health)
			handler := middleware.Chain(chain...)(httpserver.Handler(mux))
			srv := httpserver.New(httpserver.Config{
				Addr:              cfg.Addr,
				ReadHeaderTimeout: cfg.ReadHeaderTimeout,
				ReadTimeout:       cfg.ReadTimeout,
				WriteTimeout:      cfg.WriteTimeout,
				IdleTimeout:       cfg.IdleTimeout,
			}, handler, httpserver.WithLogger(a.log))
			b.Set(muxKey{}, mux)
			return srv, nil
		})
		return nil
	}
}

// withErrorRenderers installs the feature renderers above everything else
// so every rejection, including a recovered panic, can become a page.
func withErrorRenderers(renderers []httpx.ErrorRenderer) Middleware {
	return func(next http.Handler) http.Handler {
		if len(renderers) == 0 {
			return next
		}
		combined := func(w http.ResponseWriter, r *http.Request, status int, env httpx.Envelope) bool {
			for _, fn := range renderers {
				if fn(w, r, status, env) {
					return true
				}
			}
			return false
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithErrorRenderer(r.Context(), combined)))
		})
	}
}

// withMappers puts the app's error mappers in every request context so
// gox.Error can apply them.
func withMappers(a *App) Middleware {
	return func(next http.Handler) http.Handler {
		if len(a.mappers) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithMappers(r.Context(), a.mappers...)))
		})
	}
}

// WithMiddleware appends middleware after the default chain (and after the
// auth slot), outermost first.
func WithMiddleware(mw ...Middleware) Option {
	return func(b *Builder) error {
		b.middleware = append(b.middleware, mw...)
		return nil
	}
}

// WithAuth fills the auth slot of the chain: it runs after the default
// middleware and before WithMiddleware, for every route including the
// health probes, so an auth middleware should let those through.
func WithAuth(mw Middleware) Option {
	return func(b *Builder) error {
		if mw == nil {
			return fmt.Errorf("WithAuth(nil)")
		}
		b.auth = mw
		return nil
	}
}

// HTTPClient declares the default outbound client: timeouts and pool limits
// from HTTP_CLIENT_* config, trace and request-id propagation, and
// "User-Agent: <service>/<version>". It enables a.HTTPClient.
func HTTPClient() Option {
	return func(b *Builder) error {
		b.httpClient = true
		return nil
	}
}

func (b *Builder) buildHTTPClient(a *App) {
	ua := a.name
	if a.cfg.Version != "" {
		ua += "/" + a.cfg.Version
	}
	c := httpclient.New(httpclient.Config{
		Timeout:         a.cfg.HTTPClient.Timeout,
		MaxIdleConns:    a.cfg.HTTPClient.MaxIdleConns,
		IdleConnTimeout: a.cfg.HTTPClient.IdleConnTimeout,
	},
		httpclient.WithUserAgent(ua),
		httpclient.WithTracerProvider(a.otel.Tracer),
		httpclient.WithPropagator(a.otel.Propagator),
	)
	b.Set(httpClientKey{}, c)
}

// HasHTTP reports whether HTTP() was declared. Feature packages that
// decorate the public server check it to give a precise error.
func (a *App) HasHTTP() bool {
	_, ok := a.Value(muxKey{})
	return ok
}

// HasHTTPClient reports whether HTTPClient() was declared.
func (a *App) HasHTTPClient() bool {
	_, ok := a.Value(httpClientKey{})
	return ok
}

// Mux returns the public ServeMux. Register routes on it before Run.
// It panics if HTTP() was not declared.
func (a *App) Mux() *http.ServeMux {
	return MustValue[*http.ServeMux](a, muxKey{}, "a.Mux", "gox.HTTP()")
}

// HandleFunc registers a handler on the public mux using Go 1.22 patterns
// such as "GET /invoices/{id}".
func (a *App) HandleFunc(pattern string, h http.HandlerFunc) {
	a.Mux().HandleFunc(pattern, h)
}

// HTTPClient returns the default outbound client. It panics if
// HTTPClient() was not declared.
func (a *App) HTTPClient() *http.Client {
	return MustValue[*http.Client](a, httpClientKey{}, "a.HTTPClient", "gox.HTTPClient()")
}
