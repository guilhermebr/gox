package workos

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	sdk "github.com/workos/workos-go/v10"
)

func (f *feature) writeCookie(w http.ResponseWriter, r *http.Request, data *sdk.SessionData) error {
	sealed, err := f.codec.Seal(data, f.cfg.CookiePassword)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: f.cfg.CookieName, Value: sealed, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: f.secure(r), MaxAge: int(f.cfg.CookieMaxAge.Seconds()),
	})
	return nil
}

func (f *feature) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: f.cfg.CookieName, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: f.secure(r), MaxAge: -1})
}

func (f *feature) secure(r *http.Request) bool {
	switch f.cfg.SecureCookies {
	case "true":
		return true
	case "false":
		return false
	}
	return f.production || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sdkPassword seals the transient values handed to the SDK; bearer-only
// services have no cookie password, and nothing sealed with it leaves the
// process.
func (f *feature) sdkPassword() string {
	if f.cfg.CookiePassword != "" {
		return f.cfg.CookiePassword
	}
	return "gox-workos-transient-seal-password-0000"
}

// tokenClaims reads the claims the SDK's result does not carry, from an
// access token the SDK has already verified.
func tokenClaims(accessToken string) (subject string, featureFlags []string) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil
	}
	var claims struct {
		Sub          string   `json:"sub"`
		FeatureFlags []string `json:"feature_flags"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Sub, claims.FeatureFlags
}
