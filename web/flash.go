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

// AddFlash queues a message for the next rendered page. Flashes travel in a
// short-lived cookie of their own (templ escapes them on output).
func AddFlash(w http.ResponseWriter, r *http.Request, kind, message string) {
	flashes := append(readFlashes(r), Flash{Kind: kind, Message: message})
	b, _ := json.Marshal(flashes)
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: base64.RawURLEncoding.EncodeToString(b),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: featureFrom(r).secureCookie(r), MaxAge: 300,
	})
}

// takeFlashes returns and clears the queued flashes for this request.
func takeFlashes(w http.ResponseWriter, r *http.Request) []Flash {
	flashes := readFlashes(r)
	if flashes != nil {
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	}
	return flashes
}

func readFlashes(r *http.Request) []Flash {
	ck, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(ck.Value)
	if err != nil {
		return nil
	}
	var flashes []Flash
	_ = json.Unmarshal(raw, &flashes)
	return flashes
}
