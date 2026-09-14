package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// Layout wraps a page body. The framework passes the request's Page; the
// layout owns <html>, <head> and the shell.
type Layout func(page *Page, body templ.Component) templ.Component

// ErrorPage renders an error for browser requests: status is the HTTP
// status, code the envelope code ("not_found"), message the public text. It
// returns a body fragment that gox wraps in the layout; page is never nil.
// Set page.Title inside the function, before returning, because the layout
// prints it before the body renders.
type ErrorPage func(page *Page, status int, code, message string) templ.Component

// IsHTMX reports whether the request was made by htmx.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// isPartialRequest is true for htmx requests that swap a fragment, not for
// hx-boost navigations, which expect a full page.
func isPartialRequest(r *http.Request) bool {
	return IsHTMX(r) && r.Header.Get("HX-Boosted") != "true"
}

// Render writes c with status 200 inside the layout, or alone for htmx
// fragment requests.
func Render(w http.ResponseWriter, r *http.Request, c templ.Component) error {
	return RenderStatus(w, r, http.StatusOK, c)
}

// RenderStatus is Render with an explicit status (422 for a form that
// failed validation).
func RenderStatus(w http.ResponseWriter, r *http.Request, status int, c templ.Component) error {
	if isPartialRequest(r) {
		return write(w, r, status, c)
	}
	f := featureFrom(r)
	return write(w, r, status, f.layout(PageFrom(r), c))
}

// Partial writes c without the layout, whatever the request.
func Partial(w http.ResponseWriter, r *http.Request, c templ.Component) error {
	return write(w, r, http.StatusOK, c)
}

func write(w http.ResponseWriter, r *http.Request, status int, c templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		log.FromContext(r.Context()).ErrorContext(r.Context(), "render failed", log.KeyError, err.Error())
		return fmt.Errorf("web: render: %w", err)
	}
	return nil
}

// Redirect sends the browser to url: an HX-Redirect for htmx requests,
// a 303 See Other otherwise (the PRG pattern after a form post).
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// Error renders err as an error page for requests that want HTML (htmx, or
// Accept preferring text/html) and as the JSON envelope otherwise, with the
// same status either way. It applies the app's error mappers like gox.Error.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	httpx.Error(w, r, err)
}

// wantsHTML decides between a page and the envelope: htmx requests and
// Accept headers that prefer text/html get the page.
func wantsHTML(r *http.Request) bool {
	if IsHTMX(r) {
		return true
	}
	accept := r.Header.Get("Accept")
	html := strings.Index(accept, "text/html")
	json := strings.Index(accept, "application/json")
	if html < 0 {
		return false
	}
	return json < 0 || html < json
}

// errorRenderer is the httpx hook: it turns envelopes into pages. It runs
// above the request middleware for panics and timeouts, so it must work
// with a bare context: pageOrBare builds a minimal Page in that case.
func (f *feature) errorRenderer(w http.ResponseWriter, r *http.Request, status int, env errors.Envelope) bool {
	if !wantsHTML(r) {
		return false
	}
	page := f.pageOrBare(r)
	body := f.errorPage(page, status, env.Code, env.Message)
	if !isPartialRequest(r) {
		body = f.layout(page, body)
	}
	return write(w, r, status, body) == nil
}

func (f *feature) pageOrBare(r *http.Request) *Page {
	if _, ok := r.Context().Value(pageKey{}).(*pageHolder); ok {
		return PageFrom(r)
	}
	return &Page{Path: r.URL.Path, Data: map[string]any{}, Request: r, assets: f.assets, Nonce: nonce()}
}

// defaultLayout is used until WithLayout replaces it: a minimal, valid
// document with the title and the flashes.
func defaultLayout(page *Page, body templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>`)
		_, _ = io.WriteString(w, templ.EscapeString(page.Title))
		_, _ = io.WriteString(w, `</title></head><body>`)
		for _, fl := range page.Flashes {
			_, _ = io.WriteString(w, `<div class="flash flash-`+templ.EscapeString(fl.Kind)+`">`+templ.EscapeString(fl.Message)+`</div>`)
		}
		if err := body.Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, `</body></html>`)
		return err
	})
}

// defaultErrorPage is used until WithErrorPage replaces it.
func defaultErrorPage(page *Page, status int, _ string, message string) templ.Component {
	page.Title = strconv.Itoa(status) + " " + http.StatusText(status)
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<main class="error"><h1>`+templ.EscapeString(page.Title)+`</h1><p>`+templ.EscapeString(message)+`</p></main>`)
		return err
	})
}
