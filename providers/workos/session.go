package workos

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sdk "github.com/workos/workos-go/v10"

	"github.com/guilhermebr/gox"
)

// Session is the authenticated caller of a request.
type Session struct {
	ID             string // WorkOS session id (sid)
	UserID         string
	OrganizationID string
	Role           string
	Permissions    []string
	Entitlements   []string
	FeatureFlags   []string                              // WorkOS feature flags enabled for this user and organization
	User           *sdk.User                             // display profile sealed in the cookie; nil for bearer tokens
	Impersonator   *sdk.AuthenticateResponseImpersonator // set when a WorkOS admin impersonates the user
}

type sessionKey struct{}

// state travels with the request so handlers reach the feature and the
// session the middleware resolved.
type state struct {
	f       *feature
	session *Session
	data    *sdk.SessionData
}

// SessionFrom returns the request's session; ok is false for anonymous
// requests. It panics if WithSessions was not passed to Enable.
func SessionFrom(r *http.Request) (*Session, bool) {
	st := stateFrom(r, "workos.SessionFrom")
	return st.session, st.session != nil
}

func stateFrom(r *http.Request, accessor string) *state {
	st, ok := r.Context().Value(sessionKey{}).(*state)
	if !ok {
		panic("gox: " + accessor + " used but workos.WithSessions() was not passed to workos.Enable")
	}
	return st
}

// RequireSession wraps a handler so anonymous requests get a 401.
func RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SessionFrom(r); !ok {
			gox.Error(w, r, gox.Unauthenticated("authentication required"))
			return
		}
		next(w, r)
	}
}

func (f *feature) sessions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &state{f: f}
		if token, ok := bearer(r); ok {
			st.data = &sdk.SessionData{AccessToken: token}
			res, err := f.authenticate(r.Context(), st.data)
			if err == nil && res.Authenticated {
				st.session = sessionOf(res, token)
			} else if f.log.Enabled(r.Context(), slog.LevelDebug) {
				// Debug: API callers with stale or foreign tokens are routine.
				f.log.DebugContext(r.Context(), "workos bearer token rejected",
					withErr([]any{"reason", reasonOf(res), "issuer", f.issuer, "token_issuer", peekClaims(token).Iss}, err)...)
			}
		} else if ck, err := r.Cookie(f.cfg.CookieName); err == nil && ck.Value != "" {
			st.data, st.session = f.resolve(w, r, ck.Value)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, st)))
	})
}

// resolve turns a cookie into a session, refreshing an expired access token
// once and clearing a cookie that can never authenticate again.
func (f *feature) resolve(w http.ResponseWriter, r *http.Request, sealed string) (*sdk.SessionData, *Session) {
	ctx := r.Context()
	data, err := f.codec.Unseal(sealed, f.cfg.CookiePassword)
	if err != nil || data == nil {
		// A rotated WORKOS_COOKIE_PASSWORD or a tampered cookie.
		f.log.WarnContext(ctx, "workos session cookie unreadable", withErr(nil, err)...)
		f.clearCookie(w, r)
		return nil, nil
	}
	res, err := f.authenticate(ctx, data)
	if err == nil && res.NeedsRefresh {
		fresh, rerr := f.refresh(ctx, data, "")
		if rerr != nil {
			revoked := errors.Is(rerr, errRevoked)
			level := slog.LevelWarn
			if revoked {
				level = slog.LevelInfo // the session ended at WorkOS: signed out elsewhere or expired
				f.clearCookie(w, r)
			}
			f.log.Log(ctx, level, "workos session refresh failed", "revoked", revoked, "error", rerr)
			return nil, nil // a transient failure keeps the cookie for the next request
		}
		if werr := f.writeCookie(w, r, fresh); werr != nil {
			f.log.WarnContext(ctx, "workos session cookie not written", "error", werr)
			return nil, nil
		}
		data = fresh
		res, err = f.authenticate(ctx, fresh)
	}
	if err != nil {
		// The session could not be checked at all (sealing it for the SDK
		// failed): keep the cookie for the next request. The SDK reports an
		// unreachable JWKS as invalid_jwt, not as an error; keysMayBeAtFault
		// and the key probe below handle that case.
		f.log.WarnContext(ctx, "workos session check failed", "error", err)
		return nil, nil
	}
	if !res.Authenticated {
		if res.NeedsRefresh {
			// Only after a refresh that just succeeded: the fresh token is
			// already expired here, usually a clock far off WorkOS's.
			f.log.WarnContext(ctx, "workos session expired right after a refresh", "reason", res.Reason)
			return nil, nil
		}
		if keysMayBeAtFault(res.Reason, f.issuer, data.AccessToken) {
			v, fetched := f.keys.check(ctx, data.AccessToken)
			if v != tokenRejected {
				// The key set is down, or the token passes every check the SDK
				// makes with keys the SDK did not have: keep the cookie; the
				// session resumes once the SDK fetches the keys again. Warn once
				// per probe fetch.
				level, msg := slog.LevelDebug, "workos keys unreachable; session kept"
				if fetched {
					level = slog.LevelWarn
				}
				if v == tokenValid {
					msg = "workos keys missing in the SDK; session kept"
				}
				f.log.Log(ctx, level, msg, "keys_url", f.keys.url)
				return nil, nil
			}
		}
		// invalid_jwt here is usually a WORKOS_ISSUER that does not match the
		// tokens' iss (set WORKOS_ISSUER to token_issuer); the cookie can never
		// authenticate again.
		f.log.WarnContext(ctx, "workos session rejected", "reason", res.Reason,
			"issuer", f.issuer, "token_issuer", peekClaims(data.AccessToken).Iss)
		f.clearCookie(w, r)
		return nil, nil
	}
	return data, sessionOf(res, data.AccessToken)
}

// authenticate lets the SDK verify the access token (signature against the
// cached JWKS, issuer, audience, expiry). A nil error means res is set; a
// token that fails verification is a result with Authenticated false and a
// Reason, not an error.
func (f *feature) authenticate(ctx context.Context, data *sdk.SessionData) (*sdk.AuthenticateSessionResult, error) {
	sealed, err := sdk.SealSession(data, f.sdkPassword())
	if err != nil {
		return nil, err
	}
	// WithoutCancel: the SDK records a fetch attempt before making it and then
	// refuses every token for 30s if it fails, so a client that disconnects
	// mid-fetch must not cancel it. The SDK bounds the fetch at 5s.
	res, err := sdk.NewSession(f.client, sealed, f.sdkPassword(), sdk.WithSessionIssuer(f.issuer)).AuthenticateContext(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	return res, nil
}

// withErr appends the error to log attributes when there is one.
func withErr(attrs []any, err error) []any {
	if err != nil {
		return append(attrs, "error", err)
	}
	return attrs
}

// reasonOf is the SDK's reason for refusing a token, for the logs.
func reasonOf(res *sdk.AuthenticateSessionResult) string {
	if res == nil {
		return ""
	}
	return res.Reason
}

var errRevoked = errors.New("workos: refresh token revoked")

// grantWindow is how long a spent refresh token still resolves to the session
// it bought. It covers the requests that were already in flight behind it and
// nothing more: a token replayed after this is treated as revoked, which is
// what makes reuse detectable.
const grantWindow = 30 * time.Second

type grant struct {
	data *sdk.SessionData
	at   time.Time
}

// grantedFor returns the session a refresh token was exchanged for, while the
// grant window lasts.
func (f *feature) grantedFor(key string) *sdk.SessionData {
	v, ok := f.granted.Load(key)
	if !ok {
		return nil
	}
	g := v.(grant)
	if time.Since(g.at) > grantWindow {
		f.granted.Delete(key)
		return nil
	}
	return g.data
}

// remember records the exchange and drops the grants that have expired, so
// the map holds only what is still inside the window.
func (f *feature) remember(key string, data *sdk.SessionData) {
	f.granted.Store(key, grant{data: data, at: time.Now()})
	f.granted.Range(func(k, v any) bool {
		if time.Since(v.(grant).at) > grantWindow {
			f.granted.Delete(k)
		}
		return true
	})
}

// refresh exchanges the refresh token, scoped to organizationID when set.
// Refresh tokens are single-use, so concurrent requests carrying the same
// one share a single grant.
func (f *feature) refresh(ctx context.Context, data *sdk.SessionData, organizationID string) (*sdk.SessionData, error) {
	key := data.RefreshToken + "|" + organizationID
	v, err, _ := f.refreshes.Do(key, func() (any, error) {
		// Coalescing is not enough on its own: a request that arrives just
		// after the exchange finished starts a flight of its own, and the
		// token it carries has already been spent. Without this the provider
		// answers invalid_grant and a burst of requests on an expired
		// session signs the user out.
		if fresh := f.grantedFor(key); fresh != nil {
			return fresh, nil
		}
		params := &sdk.UserManagementAuthenticateWithRefreshTokenParams{RefreshToken: data.RefreshToken}
		if organizationID != "" {
			params.OrganizationID = &organizationID
		}
		resp, err := f.client.UserManagement().AuthenticateWithRefreshToken(context.WithoutCancel(ctx), params)
		if err != nil {
			var apiErr *sdk.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode == "invalid_grant" {
				return nil, errRevoked
			}
			return nil, err
		}
		fresh := &sdk.SessionData{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken, User: resp.User, Impersonator: resp.Impersonator}
		f.remember(key, fresh)
		return fresh, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*sdk.SessionData), nil
}

// SwitchOrganization re-grants the request's session into another
// organization the user belongs to, rotates the cookie and returns the new
// session (the one SessionFrom holds is the old one for the rest of this
// request). WorkOS refusing the switch is a permission-denied error.
func SwitchOrganization(w http.ResponseWriter, r *http.Request, organizationID string) (*Session, error) {
	st := stateFrom(r, "workos.SwitchOrganization")
	if st.session == nil || st.data == nil || st.data.RefreshToken == "" {
		return nil, gox.Unauthenticated("a cookie session is required to switch organization")
	}
	fresh, err := st.f.refresh(r.Context(), st.data, organizationID)
	if err != nil {
		if errors.Is(err, errRevoked) {
			return nil, gox.PermissionDenied("organization %s is not available to this session", organizationID)
		}
		return nil, gox.WrapError(err, gox.CodeUnavailable, "the identity provider is unavailable")
	}
	res, err := st.f.authenticate(r.Context(), fresh)
	if err != nil || !res.Authenticated {
		return nil, gox.Unauthenticated("the re-granted session could not be verified")
	}
	if err := st.f.writeCookie(w, r, fresh); err != nil {
		return nil, err
	}
	return sessionOf(res, fresh.AccessToken), nil
}

func sessionOf(res *sdk.AuthenticateSessionResult, accessToken string) *Session {
	s := &Session{
		ID: res.SessionID, OrganizationID: res.OrganizationID, Role: res.Role,
		Permissions: res.Permissions, Entitlements: res.Entitlements, User: res.User, Impersonator: res.Impersonator,
	}
	s.UserID, s.FeatureFlags = tokenClaims(accessToken)
	if s.UserID == "" && res.User != nil {
		s.UserID = res.User.ID
	}
	return s
}

func bearer(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
