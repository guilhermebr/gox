# 0007. gox/web: server-rendered HTML with templ

Date: 2026-09-13
Status: accepted (API confirmed 2026-09-13; implemented in Phase 3b)

## Context

Two of the four audited services are server-rendered templ apps
(`docs/audit-consumers.md` §7). What they share: templ with explicit typed
page parameters, Alpine.js for interactivity, HTMX used lightly as a form
poster, the raw backend JWT in an `HttpOnly` cookie, flash state threaded by
hand, no CSRF, no custom error pages, and a web layer that calls the
service's REST API as the signed-in user (the house rule). What they lack
is exactly what a framework should provide once.

## Decision

`gox/web` is a feature module (`github.com/guilhermebr/gox/web`, depends on
`a-h/templ`) layered on `gox.HTTP()`. It exists so an HTML app needs the
same three lines as a JSON API and so generated templ code lands in one
structure.

### Stack (fixed)

templ (CLI pinned to go.mod), Alpine.js as the primary interactivity layer,
HTMX 2.x pinned for server interactions, Tailwind v4 standalone CLI with
committed output, embedded assets with content-hashed URLs. Alpine and HTMX
are vendored by the scaffold's Makefile; the framework ships no JavaScript.

### API

```go
package web

func Enable(opts ...Option) gox.Option      // requires gox.HTTP(); registers section WEB; mounts static,
                                            // sessions, CSRF, flash and HTML error pages
type Config struct {                        // <PREFIX>_WEB_*
    SessionSecret string        `conf:"mask"`             // required by WithSessions; >= 32 bytes
    SessionName   string        `conf:"default:session"`
    SessionMaxAge time.Duration `conf:"default:720h"`
    SecureCookies string        `conf:"default:auto"`      // auto | true | false; auto = production or X-Forwarded-Proto: https
    StaticPrefix  string        `conf:"default:/static/"`
    BackendURL    string        `conf:"help:base URL of the API this UI calls as the signed-in user"`
}

// options
func WithStatic(fsys fs.FS) Option                        // embed.FS rooted at the assets dir
func WithSessions() Option                                // signed+encrypted cookie sessions; CSRF on for non-GET
func WithLayout(l Layout) Option                          // one shell; Page carries the variant
func WithErrorPage(p ErrorPage) Option                     // one function for every status; the default is a minimal page
func WithBackend() Option                                 // enables API(r); needs gox.HTTPClient() and WEB_BACKEND_URL

type Layout func(page *Page, body templ.Component) templ.Component
type ErrorPage func(page *Page, status int, code, message string) templ.Component

// page context: assembled lazily once per request, passed to the layout by pointer
// so handlers can set Title before Render; never a map
type Page struct {
    Title     string
    Path      string
    User      any            // what WithSessionUser resolved, or nil
    Flashes   []Flash        // consumed on the first PageFrom of the request
    CSRFToken string
    Nonce     string         // per-request nonce for a CSP the app chooses to set
    Data      map[string]any // escape hatch for the layout only, not for pages
    Request   *http.Request
}
func PageFrom(r *http.Request) *Page
func (p *Page) Asset(name string) string                  // hashed URL in production, plain in development

// rendering
func Render(w http.ResponseWriter, r *http.Request, c templ.Component) error         // 200, layout applied
func RenderStatus(w http.ResponseWriter, r *http.Request, status int, c templ.Component) error
func Partial(w http.ResponseWriter, r *http.Request, c templ.Component) error        // never wraps the layout
func Error(w http.ResponseWriter, r *http.Request, err error)                        // HTML page or JSON envelope by Accept / HX-Request
func IsHTMX(r *http.Request) bool
func Redirect(w http.ResponseWriter, r *http.Request, url string)                    // HX-Redirect for HTMX, 303 otherwise

// sessions (WithSessions)
type Session struct{ /* unexported */ }
func SessionFrom(r *http.Request) *Session                // panics: "gox: web.SessionFrom called but web.WithSessions() was not passed to web.Enable"
func (s *Session) Get(key string) (string, bool)
func (s *Session) Set(key, value string)                  // saved automatically when the response is written
func (s *Session) Delete(key string)
func (s *Session) Clear()                                 // logout
func (s *Session) Token() string                          // the backend bearer token, if SetToken was called
func (s *Session) SetToken(token string)
func WithSessionUser(fn func(r *http.Request, s *Session) (any, error)) Option   // resolves Page.User once per request
func RequireSession(next http.HandlerFunc) http.HandlerFunc   // redirects to WithLoginPath (default /login) when no token
func WithLoginPath(p string) Option

// CSRF (automatic with sessions): token in session, checked on POST/PUT/PATCH/DELETE
// from the _csrf form field or the X-CSRF-Token header; HTMX gets it from a meta tag
func CSRFField(page *Page) templ.Component               // hidden input
func CSRFMeta(page *Page) templ.Component                // <meta name="csrf-token">; the scaffold's htmx config reads it

// flash (cookie-backed, one-shot)
type Flash struct{ Kind, Message string }
func AddFlash(w http.ResponseWriter, r *http.Request, kind, message string)

// forms: decode + validate into a struct; errors keyed by field for re-rendering
type FieldErrors map[string]string
func (e FieldErrors) Error() string
func Form[T any](r *http.Request) (T, FieldErrors, error)
//   tags: `form:"email,required"`, `form:"age,min=18,max=120"`, `form:"agree,required"`;
//   types: string, int, int64, float64, bool, time.Duration, []string

// backend client (WithBackend): the house rule made safe
type API struct{ /* unexported */ }
func APIFrom(r *http.Request) *API                        // derived per request from a.HTTPClient() + Session.Token()
func (a *API) Get(ctx context.Context, path string, out any) error
func (a *API) Post(ctx context.Context, path string, in, out any) error
func (a *API) Put(ctx context.Context, path string, in, out any) error
func (a *API) Delete(ctx context.Context, path string) error
//   a non-2xx response with the gox envelope becomes a coded error, so
//   web.Error renders it correctly (404 stays 404, invalid_argument carries details)
```

### Conventions models can rely on

- Every page component takes explicit typed parameters plus `web.Page`;
  layouts take `(page *Page, body templ.Component)`.
- Forms: `GET` renders; `POST` decodes with `web.Form`, on `FieldErrors`
  re-renders the same component with status 422, on success `AddFlash` +
  `Redirect` (PRG).
- Partials live in the same `views` package; `Render` skips the layout for
  HTMX requests that are not `hx-boost` navigations; `Partial` never wraps.
- `web.Error` for HTML handlers, `gox.Error` for JSON handlers; the shared
  error middleware picks the page or the envelope for framework rejections.
- Layout: `cmd/<name>/main.go`, `internal/<feature>/handler.go` and
  `internal/<feature>/views/*.templ`, `web/layout/*.templ`,
  `web/components/*.templ`, `static/`.

### Security defaults

Cookies: `HttpOnly`, `SameSite=Lax`, `Secure` from config or the
forwarded proto. The session payload is AES-256-GCM sealed (encrypted and
authenticated) with a key derived from `WEB_SESSION_SECRET`, capped under
4 KiB. The secret is required in production; in development an ephemeral
one is generated with a warning so `go run` works with no setup. CSRF is
on whenever sessions are: token in the session, checked on every
state-changing request from the `_csrf` field or the `X-CSRF-Token`
header, failures render a 403 page. Security headers come from the HTTP
chain. No Content-Security-Policy is set by default: Alpine.js's standard
build needs `unsafe-eval`, so a CSP is an app decision; `Page.Nonce` is
provided for apps that set one.

### Error pages for framework rejections

`Enable` registers an error renderer with `Builder.ErrorRenderer`, which
the root installs above the whole middleware chain. Panics, timeouts,
unmatched routes and CSRF failures therefore render the error page for
requests that want HTML (htmx, or `Accept` preferring `text/html`) and the
JSON envelope otherwise, with the same status and code either way.

## Consequences

- One layout, one page-context struct, one form pattern, one error path.
  The two existing apps lose their duplicated shells, their `?message=`
  flash plumbing, their hand-rolled cookies and their shared mutable API
  client.
- Server-side session storage is not provided. If a service outgrows the
  cookie, a `Store` interface is the next feature, driven by a real
  consumer.
- Datastar or HTMX 4 would change the scaffold's vendored files and the
  `IsHTMX`/`Redirect` helpers, nothing else.
