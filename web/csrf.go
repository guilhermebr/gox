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

// HeaderCSRF is the header HTMX and fetch callers send the token in.
const HeaderCSRF = "X-CSRF-Token"

// FieldCSRF is the form field regular forms send the token in.
const FieldCSRF = "_csrf"

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

// csrfMiddleware refuses state-changing requests whose token does not match
// the session's. Safe methods pass. It runs only when sessions are enabled.
func (f *feature) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}
		s := SessionFrom(r)
		expected, _ := s.Get(keyCSRF)
		got := r.Header.Get(HeaderCSRF)
		if got == "" && isForm(r) {
			_ = r.ParseForm()
			got = r.PostForm.Get(FieldCSRF)
		}
		if expected == "" || got == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(got)) != 1 {
			httpx.Error(w, r, errors.PermissionDenied("invalid or missing CSRF token"))
			return
		}
		next.ServeHTTP(w, r)
	})
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
