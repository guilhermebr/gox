package workos

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	sdk "github.com/workos/workos-go/v10"

	"github.com/guilhermebr/gox"
)

// Login returns the handler that starts an AuthKit sign-in: it redirects to
// the hosted login and remembers ?return_to= (local paths only) for
// Callback. Mount it where the application wants, for example
// "GET /auth/login".
func Login(a *gox.App) http.HandlerFunc {
	f := featureOf(a, "workos.Login")
	return func(w http.ResponseWriter, r *http.Request) {
		if f.cfg.RedirectURI == "" {
			gox.Error(w, r, gox.Internal("WORKOS_REDIRECT_URI is not configured"))
			return
		}
		raw := make([]byte, 24)
		_, _ = rand.Read(raw)
		st := base64.RawURLEncoding.EncodeToString(raw)
		provider := "authkit"
		url, err := f.client.GetAuthKitAuthorizationURL(sdk.AuthKitAuthorizationURLParams{RedirectURI: f.cfg.RedirectURI, State: &st, Provider: &provider})
		if err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeInternal, "could not start the sign-in"))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: f.stateCookie(), Value: st + "|" + localPath(r.URL.Query().Get("return_to")), Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: f.secure(r), MaxAge: 600,
		})
		http.Redirect(w, r, url, http.StatusFound)
	}
}

// Callback returns the handler for the redirect URI: it checks the state,
// exchanges the code, writes the session cookie and redirects to the path
// Login remembered (or "/").
func Callback(a *gox.App) http.HandlerFunc {
	f := featureOf(a, "workos.Callback")
	return func(w http.ResponseWriter, r *http.Request) {
		want, returnTo := "", "/"
		if ck, err := r.Cookie(f.stateCookie()); err == nil {
			want, returnTo, _ = strings.Cut(ck.Value, "|")
		}
		http.SetCookie(w, &http.Cookie{Name: f.stateCookie(), Path: "/", MaxAge: -1})
		got := r.URL.Query().Get("state")
		if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			gox.Error(w, r, gox.InvalidArgument("the sign-in state does not match; start again"))
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			gox.Error(w, r, gox.Unauthenticated("the sign-in was not completed"))
			return
		}
		resp, err := f.client.UserManagement().AuthenticateWithCode(r.Context(), &sdk.UserManagementAuthenticateWithCodeParams{Code: code})
		if err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeUnauthenticated, "the sign-in could not be verified"))
			return
		}
		data := &sdk.SessionData{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken, User: resp.User, Impersonator: resp.Impersonator}
		if err := f.writeCookie(w, r, data); err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeInternal, "could not store the session"))
			return
		}
		http.Redirect(w, r, localPath(returnTo), http.StatusSeeOther)
	}
}

// Logout returns the handler that clears the session cookie and redirects
// to the WorkOS logout URL, which ends the session there and returns to
// ?return_to= when the application configured it as a logout redirect.
func Logout(a *gox.App) http.HandlerFunc {
	f := featureOf(a, "workos.Logout")
	return func(w http.ResponseWriter, r *http.Request) {
		target := "/"
		if st, ok := r.Context().Value(sessionKey{}).(*state); ok && st.data != nil {
			if sealed, err := sdk.SealSession(st.data, f.sdkPassword()); err == nil {
				s := sdk.NewSession(f.client, sealed, f.sdkPassword(), sdk.WithSessionIssuer(f.cfg.Issuer))
				if u, err := s.GetLogoutURL(r.Context(), r.URL.Query().Get("return_to")); err == nil {
					target = u
				}
			}
		}
		f.clearCookie(w, r)
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// VerifyWebhook reads the request body, checks the WorkOS-Signature header
// against WORKOS_WEBHOOK_SECRET and returns the event. A bad signature is an
// unauthenticated error.
func VerifyWebhook(a *gox.App, r *http.Request) (*sdk.EventSchema, error) {
	f := featureOf(a, "workos.VerifyWebhook")
	if f.cfg.WebhookSecret == "" {
		return nil, gox.Internal("WORKOS_WEBHOOK_SECRET is not configured")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read the webhook body")
	}
	ev, err := sdk.NewWebhookVerifier(f.cfg.WebhookSecret).ConstructEvent(r.Header.Get("WorkOS-Signature"), string(body))
	if err != nil {
		return nil, gox.WrapError(err, gox.CodeUnauthenticated, "the webhook signature is not valid")
	}
	return ev, nil
}

func (f *feature) stateCookie() string { return f.cfg.CookieName + "_state" }

// localPath keeps redirects on this site: anything that is not a plain
// absolute path becomes "/".
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) || strings.ContainsAny(p, "\r\n") {
		return "/"
	}
	return p
}
