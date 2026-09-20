package workos

import (
	"context"
	"errors"
	"net/http"
	"strings"

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
			if res := f.authenticate(r.Context(), st.data); res != nil && res.Authenticated {
				st.session = sessionOf(res, token)
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
	data, err := f.codec.Unseal(sealed, f.cfg.CookiePassword)
	if err != nil || data == nil {
		f.clearCookie(w, r)
		return nil, nil
	}
	res := f.authenticate(r.Context(), data)
	if res != nil && res.NeedsRefresh {
		fresh, err := f.refresh(r.Context(), data, "")
		if err != nil {
			if errors.Is(err, errRevoked) {
				f.clearCookie(w, r)
			}
			return nil, nil // a transient failure keeps the cookie for the next request
		}
		if err := f.writeCookie(w, r, fresh); err != nil {
			return nil, nil
		}
		data, res = fresh, f.authenticate(r.Context(), fresh)
	}
	if res == nil || !res.Authenticated {
		if res != nil && !res.NeedsRefresh {
			f.clearCookie(w, r)
		}
		return nil, nil
	}
	return data, sessionOf(res, data.AccessToken)
}

// authenticate lets the SDK verify the access token (signature against the
// cached JWKS, issuer, audience, expiry).
func (f *feature) authenticate(ctx context.Context, data *sdk.SessionData) *sdk.AuthenticateSessionResult {
	sealed, err := sdk.SealSession(data, f.sdkPassword())
	if err != nil {
		return nil
	}
	res, err := sdk.NewSession(f.client, sealed, f.sdkPassword(), sdk.WithSessionIssuer(f.cfg.Issuer)).AuthenticateContext(ctx)
	if err != nil {
		return nil
	}
	return res
}

var errRevoked = errors.New("workos: refresh token revoked")

// refresh exchanges the refresh token, scoped to organizationID when set.
// Refresh tokens are single-use, so concurrent requests carrying the same
// one share a single grant.
func (f *feature) refresh(ctx context.Context, data *sdk.SessionData, organizationID string) (*sdk.SessionData, error) {
	v, err, _ := f.refreshes.Do(data.RefreshToken+"|"+organizationID, func() (any, error) {
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
		return &sdk.SessionData{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken, User: resp.User, Impersonator: resp.Impersonator}, nil
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
	res := st.f.authenticate(r.Context(), fresh)
	if res == nil || !res.Authenticated {
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
