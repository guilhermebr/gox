// Package web is server-rendered HTML on gox: templ pages behind the
// gox.HTTP() server with a layout, static assets, cookie sessions with
// CSRF, one-shot flashes, a form decoder, HTML error pages, and a
// per-request client for the service's own API. See ADR 0007.
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/config"
)

type options struct {
	static      fs.FS
	sessions    bool
	backend     bool
	layout      Layout
	errorPage   ErrorPage
	resolveUser func(r *http.Request, s *Session) (any, error)
	loginPath   string
}

// Option configures Enable.
type Option func(*options)

// WithStatic serves fsys under WEB_STATIC_PREFIX (default /static/) with
// content-hashed URLs in production; Page.Asset resolves names.
func WithStatic(fsys fs.FS) Option {
	return func(o *options) { o.static = fsys }
}

// WithSessions enables the encrypted cookie session (SessionFrom) and CSRF
// checks on every state-changing request. Needs WEB_SESSION_SECRET.
func WithSessions() Option {
	return func(o *options) { o.sessions = true }
}

// WithBackend enables APIFrom: a per-request client for the service's own
// API at WEB_BACKEND_URL, authenticated with the session token. Needs
// gox.HTTPClient().
func WithBackend() Option {
	return func(o *options) { o.backend = true }
}

// WithLayout sets the layout Render wraps pages in.
func WithLayout(l Layout) Option {
	return func(o *options) { o.layout = l }
}

// WithErrorPage replaces the default error page.
func WithErrorPage(p ErrorPage) Option {
	return func(o *options) { o.errorPage = p }
}

// WithSessionUser resolves Page.User once per request from the session
// (typically by calling the backend with APIFrom).
func WithSessionUser(fn func(r *http.Request, s *Session) (any, error)) Option {
	return func(o *options) { o.resolveUser = fn }
}

// WithLoginPath sets where RequireSession redirects (default /login).
func WithLoginPath(p string) Option {
	return func(o *options) { o.loginPath = p }
}

// feature is the built state shared by the middleware and helpers.
type feature struct {
	cfg         *Config
	log         *slog.Logger
	production  bool
	codec       *codec
	assets      *manifest
	layout      Layout
	errorPage   ErrorPage
	resolveUser func(r *http.Request, s *Session) (any, error)
	loginPath   string
	client      *http.Client
	backend     bool
}

type featureKey struct{}

func featureFrom(r *http.Request) *feature {
	f, ok := r.Context().Value(featureKey{}).(*feature)
	if !ok {
		panic("gox: web helper called outside a gox/web request; was web.Enable() passed to gox.New?")
	}
	return f
}

// Enable declares the web layer. It requires gox.HTTP() and registers the
// WEB config section, the request middleware (page context, sessions,
// CSRF), the static handler, and an error renderer that turns every
// rejection into the error page for requests that want HTML (htmx, or
// Accept preferring text/html) while other clients keep the JSON envelope.
func Enable(opts ...Option) gox.Option {
	return func(b *gox.Builder) error {
		o := options{layout: defaultLayout, errorPage: defaultErrorPage, loginPath: "/login"}
		for _, opt := range opts {
			opt(&o)
		}
		cfg := &Config{}
		b.ConfigSection("WEB", cfg, "web.Enable()")
		f := &feature{cfg: cfg, layout: o.layout, errorPage: o.errorPage, resolveUser: o.resolveUser, loginPath: o.loginPath, backend: o.backend}

		b.Middleware(f.middleware(&o))
		b.ErrorRenderer(f.errorRenderer)

		b.Finish(func(a *gox.App) error {
			if !a.HasHTTP() {
				return errors.New("web.Enable() requires gox.HTTP(); add gox.HTTP() to gox.New")
			}
			f.log = a.Log()
			f.production = a.Config().Environment == "production"
			if o.sessions {
				if cfg.SessionSecret == "" {
					if f.production {
						return fmt.Errorf("%s is required by web.WithSessions() in production", config.EnvName(a.ConfigPrefix(), "WEB_SESSION_SECRET"))
					}
					cfg.SessionSecret = ephemeralSecret()
					f.log.Warn("using an ephemeral session secret; sessions will not survive a restart",
						slog.String("set", config.EnvName(a.ConfigPrefix(), "WEB_SESSION_SECRET")))
				}
				c, err := newCodec([]byte(cfg.SessionSecret))
				if err != nil {
					return fmt.Errorf("%s: %w", config.EnvName(a.ConfigPrefix(), "WEB_SESSION_SECRET"), err)
				}
				f.codec = c
			}
			if o.backend {
				if !a.HasHTTPClient() {
					return errors.New("web.WithBackend() requires gox.HTTPClient(); add gox.HTTPClient() to gox.New")
				}
				if cfg.BackendURL == "" {
					return fmt.Errorf("%s is required by web.WithBackend()", config.EnvName(a.ConfigPrefix(), "WEB_BACKEND_URL"))
				}
				f.client = a.HTTPClient()
			}
			if o.static != nil {
				m, err := newManifest(o.static, cfg.StaticPrefix, f.production)
				if err != nil {
					return err
				}
				f.assets = m
				a.Mux().Handle("GET "+cfg.StaticPrefix, m.handler())
			}
			return nil
		})
		return nil
	}
}

// middleware assembles the per-request chain, outermost first: feature
// context → session → backend client → page holder → CSRF check → handler.
// CSRF runs last so its rejection can render a page with everything
// (session, flashes, user) in place. The HTML error renderer is registered
// with Builder.ErrorRenderer, above the whole root chain, so recovered
// panics and timeouts reach it too.
func (f *feature) middleware(o *options) gox.Middleware {
	return func(next http.Handler) http.Handler {
		h := next
		if o.sessions {
			h = f.csrfMiddleware(h)
		}
		h = f.pageMiddleware(h)
		if o.backend {
			h = f.backendMiddleware(h)
		}
		if o.sessions {
			h = f.sessionMiddleware(h)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), featureKey{}, f)))
		})
	}
}

func ephemeralSecret() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// backendMiddleware derives the API client for the request from the
// session token; the shared client is never mutated.
func (f *feature) backendMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if s, ok := r.Context().Value(sessionKey{}).(*Session); ok {
			token = s.Token()
		}
		api := newAPI(f.client, f.cfg.BackendURL, token)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKey{}, api)))
	})
}
