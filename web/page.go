package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
)

// Page is the per-request context every layout receives. Handlers set
// Title (and Data for layout-only extras) before Render; the rest is
// assembled by the framework once per request.
type Page struct {
	Title     string
	Path      string
	User      any            // what WithSessionUser resolved, or nil
	Flashes   []Flash        // consumed on the first PageFrom of the request
	CSRFToken string         // "" without sessions
	Nonce     string         // per-request CSP nonce for inline scripts
	Data      map[string]any // layout-only escape hatch; pages take typed params
	Request   *http.Request  // for layouts that need headers or the URL

	assets *manifest
}

// Asset returns the URL for a static file: content-hashed in production,
// plain in development. Unknown names return their plain URL.
func (p *Page) Asset(name string) string {
	if p.assets == nil {
		return "/static/" + name
	}
	return p.assets.url(name)
}

type pageKey struct{}

type pageHolder struct {
	once sync.Once
	page *Page
	err  error
}

// PageFrom returns the request's Page, building it on first use: flashes
// are taken, the CSRF token is ensured and the session user is resolved.
func PageFrom(r *http.Request) *Page {
	h, ok := r.Context().Value(pageKey{}).(*pageHolder)
	if !ok {
		panic("gox: web.PageFrom called outside a gox/web request; was web.Enable() passed to gox.New?")
	}
	h.once.Do(func() {
		f := featureFrom(r)
		w, _ := r.Context().Value(writerKey{}).(http.ResponseWriter)
		p := &Page{Path: r.URL.Path, Data: map[string]any{}, Request: r, assets: f.assets, Nonce: nonce()}
		p.Flashes = takeFlashes(w, r)
		if s, ok := r.Context().Value(sessionKey{}).(*Session); ok {
			p.CSRFToken = csrfToken(s)
			if f.resolveUser != nil {
				u, err := f.resolveUser(r, s)
				if err != nil {
					h.err = err
					f.log.WarnContext(r.Context(), "session user not resolved", "error", err.Error())
				}
				p.User = u
			}
		}
		h.page = p
	})
	return h.page
}

type writerKey struct{}

// pageMiddleware installs the page holder and remembers the writer so
// PageFrom can clear flash cookies.
func (f *feature) pageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), pageKey{}, &pageHolder{})
		ctx = context.WithValue(ctx, writerKey{}, w)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func nonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawStdEncoding.EncodeToString(b[:])
}
