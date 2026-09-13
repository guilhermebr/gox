package web

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// Flash is a one-shot message shown on the next page: "success",
// "error", "info" and so on are the conventional kinds.
type Flash struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

const flashCookie = "flash"

// AddFlash queues a message for the next rendered page. With sessions
// enabled it travels in the session cookie; without, in a plain cookie
// (templ escapes it on output).
func AddFlash(w http.ResponseWriter, r *http.Request, kind, message string) {
	f := featureFrom(r)
	if s, ok := r.Context().Value(sessionKey{}).(*Session); ok {
		var flashes []Flash
		if raw, ok := s.Get(keyFlash); ok {
			_ = json.Unmarshal([]byte(raw), &flashes)
		}
		flashes = append(flashes, Flash{Kind: kind, Message: message})
		b, _ := json.Marshal(flashes)
		s.Set(keyFlash, string(b))
		return
	}
	var flashes []Flash
	if ck, err := r.Cookie(flashCookie); err == nil {
		if raw, err := base64.RawURLEncoding.DecodeString(ck.Value); err == nil {
			_ = json.Unmarshal(raw, &flashes)
		}
	}
	flashes = append(flashes, Flash{Kind: kind, Message: message})
	b, _ := json.Marshal(flashes)
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: base64.RawURLEncoding.EncodeToString(b),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: f.secureCookie(r), MaxAge: 300,
	})
}

// takeFlashes returns and clears the queued flashes for this request.
func (f *feature) takeFlashes(w http.ResponseWriter, r *http.Request) []Flash {
	var flashes []Flash
	if s, ok := r.Context().Value(sessionKey{}).(*Session); ok {
		if raw, ok := s.Get(keyFlash); ok && raw != "" {
			_ = json.Unmarshal([]byte(raw), &flashes)
			s.Delete(keyFlash)
		}
		return flashes
	}
	if ck, err := r.Cookie(flashCookie); err == nil {
		if raw, err := base64.RawURLEncoding.DecodeString(ck.Value); err == nil {
			_ = json.Unmarshal(raw, &flashes)
		}
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	}
	return flashes
}
