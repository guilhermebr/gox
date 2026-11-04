package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/a-h/templ"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
)

// HeaderCSRF ("X-CSRF-Token") is the header htmx and fetch callers send the token in.
const HeaderCSRF = "X-CSRF-Token"

// FieldCSRF ("_csrf") is the hidden form field regular forms send the token in; a missing or wrong token is a 403.
const FieldCSRF = "_csrf"

// csrfMultipartMemory is how much of a multipart body the CSRF check keeps in
// memory while parsing it (the http.Request.FormFile default); larger files
// spill to temporary files. The parsed form stays on the request, so the
// handler's FormFile and FormValue read it without parsing again.
const csrfMultipartMemory = 32 << 20

// csrfToken returns the session's token, creating it on first use.
func csrfToken(s *Session) string {
	if t, ok := s.Get(keyCSRF); ok && t != "" {
		return t
	}
	var b [24]byte
	_, _ = rand.Read(b[:])
	t := hex.EncodeToString(b[:])
	s.Set(keyCSRF, t)
	return t
}

// csrfMiddleware refuses form submissions (urlencoded or multipart) whose token does not match the
// session's. Safe methods pass, exempt prefixes pass, and so do requests
// whose body is not a form (JSON APIs): browsers cannot send those
// cross-site with cookies without a CORS preflight, and the session cookie
// is SameSite=Lax. It runs only when sessions are enabled.
func (f *feature) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}
		for _, p := range f.csrfExempt {
			if strings.HasPrefix(r.URL.Path, p) {
				next.ServeHTTP(w, r)
				return
			}
		}
		if ct := r.Header.Get("Content-Type"); ct != "" && !isForm(r) {
			next.ServeHTTP(w, r)
			return
		}
		s := SessionFrom(r)
		expected, _ := s.Get(keyCSRF)
		if expected == "" {
			// No token was ever issued: refuse before reading (and, for
			// multipart, spilling to disk) a body that cannot pass.
			httpx.Error(w, r, errors.PermissionDenied("invalid or missing CSRF token"))
			return
		}
		got := r.Header.Get(HeaderCSRF)
		if got == "" && isForm(r) {
			if err := parseForm(r); err != nil {
				httpx.Error(w, r, errors.InvalidArgument("unreadable form body: %v", err))
				return
			}
			got = r.PostForm.Get(FieldCSRF)
		}
		if got == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(got)) != 1 {
			httpx.Error(w, r, errors.PermissionDenied("invalid or missing CSRF token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parseForm fills r.PostForm from a urlencoded or a multipart body.
// ParseForm alone ignores multipart bodies, which left every file-upload
// form without its token.
func parseForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(csrfMultipartMemory)
	}
	return r.ParseForm()
}

func isForm(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data")
}

// CSRFField renders the hidden input regular forms include.
func CSRFField(page *Page) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<input type="hidden" name="`+FieldCSRF+`" value="`+templ.EscapeString(page.CSRFToken)+`">`)
		return err
	})
}

// CSRFMeta renders <meta name="csrf-token">; the scaffold's htmx config
// reads it into the X-CSRF-Token header on every request.
func CSRFMeta(page *Page) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<meta name="csrf-token" content="`+templ.EscapeString(page.CSRFToken)+`">`)
		return err
	})
}
