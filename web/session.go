package web

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// maxCookieValue keeps the encoded session under the 4 KiB browsers allow
// per cookie, with room for the name and attributes.
const maxCookieValue = 3800

const (
	keyToken = "_token"
	keyCSRF  = "_csrf"
	keyFlash = "_flash"
)

// codec seals session values with AES-256-GCM under a key derived from the
// configured secret. Sealing both hides and authenticates the payload.
type codec struct {
	aead cipher.AEAD
}

func newCodec(secret []byte) (*codec, error) {
	if len(secret) < 32 {
		return nil, errors.New("web: session secret must be at least 32 bytes")
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &codec{aead: aead}, nil
}

func (c *codec) encode(values map[string]string) (string, error) {
	plain, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plain, nil)
	out := base64.RawURLEncoding.EncodeToString(sealed)
	if len(out) > maxCookieValue {
		return "", fmt.Errorf("web: session too large (%d bytes encoded, limit %d)", len(out), maxCookieValue)
	}
	return out, nil
}

func (c *codec) decode(value string) (map[string]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("web: session cookie is not base64")
	}
	if len(raw) < c.aead.NonceSize() {
		return nil, errors.New("web: session cookie too short")
	}
	nonce, sealed := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, errors.New("web: session cookie failed authentication")
	}
	values := map[string]string{}
	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, errors.New("web: session cookie is malformed")
	}
	return values, nil
}

// Session is the per-request, cookie-backed session. Changes are written
// to the cookie when the response starts.
type Session struct {
	mu       sync.Mutex
	values   map[string]string
	modified bool
	cleared  bool
}

// Get returns a value.
func (s *Session) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}

// Set stores a value. Keys starting with "_" are reserved.
func (s *Session) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	s.modified = true
}

// Delete removes a value.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	s.modified = true
}

// Clear drops every value and expires the cookie: logout.
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = map[string]string{}
	s.modified = true
	s.cleared = true
}

// Token returns the backend bearer token stored with SetToken, or "".
func (s *Session) Token() string {
	v, _ := s.Get(keyToken)
	return v
}

// SetToken stores the backend bearer token the API client sends.
func (s *Session) SetToken(token string) { s.Set(keyToken, token) }

// IsAuthenticated reports whether a token is stored.
func (s *Session) IsAuthenticated() bool { return s.Token() != "" }

func (s *Session) snapshot() (map[string]string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out, s.modified, s.cleared
}

type sessionKey struct{}

// SessionFrom returns the request's session. It panics if WithSessions was
// not passed to web.Enable.
func SessionFrom(r *http.Request) *Session {
	s, ok := r.Context().Value(sessionKey{}).(*Session)
	if !ok {
		panic("gox: web.SessionFrom called but web.WithSessions() was not passed to web.Enable")
	}
	return s
}

// sessionMiddleware loads the cookie into a Session and writes it back on
// the first byte of the response when it changed.
func (f *feature) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := &Session{values: map[string]string{}}
		if ck, err := r.Cookie(f.cfg.SessionName); err == nil {
			if values, err := f.codec.decode(ck.Value); err == nil {
				s.values = values
			}
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, s)
		sw := &sessionWriter{ResponseWriter: w, f: f, r: r, s: s}
		next.ServeHTTP(sw, r.WithContext(ctx))
		sw.flush()
	})
}

// sessionWriter defers Set-Cookie until headers go out.
type sessionWriter struct {
	http.ResponseWriter
	f       *feature
	r       *http.Request
	s       *Session
	flushed bool
}

func (w *sessionWriter) flush() {
	if w.flushed {
		return
	}
	w.flushed = true
	values, modified, cleared := w.s.snapshot()
	if !modified {
		return
	}
	cookie := &http.Cookie{
		Name:     w.f.cfg.SessionName,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   w.f.secureCookie(w.r),
	}
	if cleared && len(values) == 0 {
		cookie.MaxAge = -1
		http.SetCookie(w.ResponseWriter, cookie)
		return
	}
	value, err := w.f.codec.encode(values)
	if err != nil {
		w.f.log.Error("session not saved", "error", err.Error())
		return
	}
	cookie.Value = value
	cookie.MaxAge = int(w.f.cfg.SessionMaxAge.Seconds())
	http.SetCookie(w.ResponseWriter, cookie)
}

func (w *sessionWriter) WriteHeader(code int) {
	w.flush()
	w.ResponseWriter.WriteHeader(code)
}

func (w *sessionWriter) Write(b []byte) (int, error) {
	w.flush()
	return w.ResponseWriter.Write(b)
}

func (w *sessionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *sessionWriter) Flush() {
	w.flush()
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (f *feature) secureCookie(r *http.Request) bool {
	switch strings.ToLower(f.cfg.SecureCookies) {
	case "true":
		return true
	case "false":
		return false
	}
	if f.production {
		return true
	}
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// RequireSession wraps a handler so requests without a session token are
// redirected to the login path (WithLoginPath, default /login). HTMX
// requests get an HX-Redirect instead.
func RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := r.Context().Value(sessionKey{}).(*Session)
		if !ok {
			panic("gox: web.RequireSession used but web.WithSessions() was not passed to web.Enable")
		}
		if !s.IsAuthenticated() {
			f := featureFrom(r)
			Redirect(w, r, f.loginPath)
			return
		}
		next(w, r)
	}
}
