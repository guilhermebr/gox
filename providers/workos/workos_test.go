package workos_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	sdk "github.com/workos/workos-go/v10"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/workos"
)

const (
	clientID = "client_123"
	password = "a-cookie-password-that-is-long-enough!!"
	issuer   = "https://idp.test/"
)

// fake is the identity provider: JWKS, the authenticate endpoint for the
// code and refresh-token grants, single-use refresh tokens.
type fake struct {
	t         *testing.T
	srv       *httptest.Server
	key       *rsa.PrivateKey
	mu        sync.Mutex
	refresh   map[string]bool // live refresh tokens
	counter   int
	refreshes atomic.Int32
	ttl       time.Duration // lifetime of the next access tokens
	lastOrg   string
}

var (
	keyOnce sync.Once
	testKey *rsa.PrivateKey
)

func newFake(t *testing.T) *fake {
	t.Helper()
	keyOnce.Do(func() { testKey, _ = rsa.GenerateKey(rand.Reader, 2048) })
	f := &fake{t: t, key: testKey, refresh: map[string]bool{}, ttl: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sso/jwks/"+clientID, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /user_management/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GrantType, Code, RefreshToken, OrganizationID, ClientID, ClientSecret string
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		s := func(k string) string { v, _ := m[k].(string); return v }
		body.GrantType, body.Code, body.RefreshToken, body.OrganizationID = s("grant_type"), s("code"), s("refresh_token"), s("organization_id")
		org := "org_1"
		switch body.GrantType {
		case "authorization_code":
			if body.Code != "good-code" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
				return
			}
		case "refresh_token":
			f.refreshes.Add(1)
			f.mu.Lock()
			live := f.refresh[body.RefreshToken]
			delete(f.refresh, body.RefreshToken)
			f.mu.Unlock()
			if !live {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token revoked"}`))
				return
			}
			if body.OrganizationID != "" {
				org = body.OrganizationID
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.lastOrg = org
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user":          map[string]any{"object": "user", "id": "user_01", "email": "ana@example.com"},
			"access_token":  f.accessToken(org, f.ttl),
			"refresh_token": f.newRefreshToken(),
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) newRefreshToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	tok := "rt_" + strings.Repeat("x", f.counter)
	f.refresh[tok] = true
	return tok
}

func (f *fake) accessToken(org string, ttl time.Duration) string {
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, gojwt.MapClaims{
		"iss": issuer, "sub": "user_01", "sid": "session_01", "org_id": org, "role": "admin",
		"permissions": []string{"invoices:read"}, "feature_flags": []string{"new-billing"}, "exp": time.Now().Add(ttl).Unix(),
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.key)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

// sealed returns a session cookie value as the default codec writes it.
func (f *fake) sealed(ttl time.Duration) string {
	s, err := sdk.SealSession(&sdk.SessionData{
		AccessToken: f.accessToken("org_1", ttl), RefreshToken: f.newRefreshToken(),
		User: &sdk.User{ID: "user_01", Email: "ana@example.com"},
	}, password)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// start runs a service with the provider enabled and returns its base URL.
func start(t *testing.T, f *fake, register func(a *gox.App), opts ...workos.Option) string {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
	addr := freeAddr(t)
	t.Setenv("SHOP_HTTP_ADDR", addr)
	t.Setenv("SHOP_WORKOS_API_KEY", "sk_test_123")
	t.Setenv("SHOP_WORKOS_CLIENT_ID", clientID)
	t.Setenv("SHOP_WORKOS_COOKIE_PASSWORD", password)
	t.Setenv("SHOP_WORKOS_REDIRECT_URI", "http://"+addr+"/auth/callback")
	t.Setenv("SHOP_WORKOS_BASE_URL", f.srv.URL)
	t.Setenv("SHOP_WORKOS_ISSUER", issuer)
	t.Setenv("SHOP_WORKOS_WEBHOOK_SECRET", "whsec_test")
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(),
		workos.Enable(append([]workos.Option{workos.WithSessions()}, opts...)...))
	if err != nil {
		t.Fatal(err)
	}
	a.HandleFunc("GET /auth/login", workos.Login(a))
	a.HandleFunc("GET /auth/callback", workos.Callback(a))
	a.HandleFunc("GET /auth/logout", workos.Logout(a))
	a.HandleFunc("GET /me", workos.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		s, _ := workos.SessionFrom(r)
		email := "" // bearer tokens carry no profile
		if s.User != nil {
			email = s.User.Email
		}
		_ = gox.JSON(w, http.StatusOK, map[string]any{"user": s.UserID, "org": s.OrganizationID, "role": s.Role, "permissions": s.Permissions, "email": email, "sid": s.ID, "flags": s.FeatureFlags})
	}))
	a.HandleFunc("GET /public", func(w http.ResponseWriter, r *http.Request) {
		_, ok := workos.SessionFrom(r)
		_ = gox.JSON(w, http.StatusOK, map[string]bool{"signed_in": ok})
	})
	if register != nil {
		register(a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return "http://" + addr
}

func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func get(t *testing.T, c *http.Client, u string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func sessionCookie(c *http.Client, base string) string {
	u, _ := url.Parse(base)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == "wos_session" {
			return ck.Value
		}
	}
	return ""
}

func setSessionCookie(c *http.Client, base, value string) {
	u, _ := url.Parse(base)
	c.Jar.SetCookies(u, []*http.Cookie{{Name: "wos_session", Value: value, Path: "/"}})
}

func TestLoginCallbackSessionAndLogout(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	c := browser(t)

	if resp, _ := get(t, c, base+"/me"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /me = %d", resp.StatusCode)
	}

	resp, _ := get(t, c, base+"/auth/login?return_to=/invoices")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if !strings.HasPrefix(loc.String(), f.srv.URL+"/user_management/authorize") || loc.Query().Get("client_id") != clientID ||
		loc.Query().Get("provider") != "authkit" || loc.Query().Get("redirect_uri") != base+"/auth/callback" {
		t.Fatalf("authorize url = %s", loc)
	}
	state := loc.Query().Get("state")
	if len(state) < 20 {
		t.Fatalf("state = %q", state)
	}

	if resp, _ := get(t, c, base+"/auth/callback?code=good-code&state=forged"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged state = %d", resp.StatusCode)
	}
	// The state is single-use: a failed attempt burns it.
	if resp, _ := get(t, c, base+"/auth/callback?code=good-code&state="+state); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("state reused after a failed attempt = %d", resp.StatusCode)
	}
	resp, _ = get(t, c, base+"/auth/login?return_to=/invoices")
	loc, _ = url.Parse(resp.Header.Get("Location"))
	state = loc.Query().Get("state")
	resp, _ = get(t, c, base+"/auth/callback?code=good-code&state="+state)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/invoices" {
		t.Fatalf("callback = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	var ck *http.Cookie
	for _, x := range resp.Cookies() {
		if x.Name == "wos_session" {
			ck = x
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" {
		t.Fatalf("session cookie = %+v", ck)
	}

	resp, body := get(t, c, base+"/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/me = %d %s", resp.StatusCode, body)
	}
	for _, want := range []string{`"flags":["new-billing"]`, `"user":"user_01"`, `"org":"org_1"`, `"role":"admin"`, `"invoices:read"`, `"email":"ana@example.com"`, `"sid":"session_01"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/me lacks %s: %s", want, body)
		}
	}

	resp, _ = get(t, c, base+"/auth/logout")
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "/user_management/sessions/logout?session_id=session_01") {
		t.Fatalf("logout = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if sessionCookie(c, base) != "" {
		t.Fatal("logout must clear the session cookie")
	}
}

func TestLoginRefusesOffSiteReturnTo(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	c := browser(t)
	for _, bad := range []string{"https://evil.example/", "//evil.example", `/\evil.example`} {
		resp, _ := get(t, c, base+"/auth/login?return_to="+url.QueryEscape(bad))
		state, _ := url.Parse(resp.Header.Get("Location"))
		resp, _ = get(t, c, base+"/auth/callback?code=good-code&state="+state.Query().Get("state"))
		if resp.Header.Get("Location") != "/" {
			t.Errorf("return_to %q led to %q", bad, resp.Header.Get("Location"))
		}
	}
}

func TestExpiredAccessTokenIsRefreshedOnceAndTheCookieRotates(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	c := browser(t)
	old := f.sealed(-time.Minute)
	setSessionCookie(c, base, old)

	resp, body := get(t, c, base+"/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/me with an expired access token = %d %s", resp.StatusCode, body)
	}
	if got := sessionCookie(c, base); got == "" || got == old {
		t.Fatal("the refreshed session must replace the cookie")
	}
	if resp, _ := get(t, c, base+"/me"); resp.StatusCode != http.StatusOK {
		t.Fatalf("second request = %d", resp.StatusCode)
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("refresh grants = %d, want 1", n)
	}
}

func TestConcurrentRequestsShareOneRefresh(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	old := f.sealed(-time.Minute)
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 8 {
		wg.Go(func() {
			c := browser(t)
			setSessionCookie(c, base, old)
			if resp, _ := get(t, c, base+"/me"); resp.StatusCode == http.StatusOK {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 8 {
		t.Fatalf("authenticated = %d of 8", ok.Load())
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("refresh grants = %d, want 1 (single-use refresh tokens)", n)
	}
}

func TestRevokedOrGarbageSessionsBecomeAnonymousAndTheCookieIsCleared(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)

	c := browser(t)
	revoked, _ := sdk.SealSession(&sdk.SessionData{AccessToken: f.accessToken("org_1", -time.Minute), RefreshToken: "rt_never_issued"}, password)
	setSessionCookie(c, base, revoked)
	if resp, _ := get(t, c, base+"/me"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked = %d", resp.StatusCode)
	}
	if sessionCookie(c, base) != "" {
		t.Fatal("a revoked session must clear the cookie")
	}

	c = browser(t)
	setSessionCookie(c, base, "garbage")
	resp, body := get(t, c, base+"/public")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"signed_in":false`) {
		t.Fatalf("garbage cookie on a public route = %d %s", resp.StatusCode, body)
	}
	if sessionCookie(c, base) != "" {
		t.Fatal("an unreadable session must clear the cookie")
	}
}

func TestBearerAccessTokensAuthenticateWithoutACookie(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	c := browser(t)
	resp, body := get(t, c, base+"/me", "Authorization", "Bearer "+f.accessToken("org_7", time.Hour))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"org":"org_7"`) {
		t.Fatalf("bearer = %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, c, base+"/me", "Authorization", "Bearer "+f.accessToken("org_7", -time.Minute)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired bearer = %d", resp.StatusCode)
	}
}

func TestSwitchOrganizationRegrantsTheSession(t *testing.T) {
	f := newFake(t)
	base := start(t, f, func(a *gox.App) {
		a.HandleFunc("POST /org/{id}", workos.RequireSession(func(w http.ResponseWriter, r *http.Request) {
			s, err := workos.SwitchOrganization(w, r, r.PathValue("id"))
			if err != nil {
				gox.Error(w, r, err)
				return
			}
			_ = gox.JSON(w, http.StatusOK, map[string]string{"org": s.OrganizationID})
		}))
	})
	c := browser(t)
	setSessionCookie(c, base, f.sealed(time.Hour))
	req, _ := http.NewRequest(http.MethodPost, base+"/org/org_2", nil)
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("switch = %v %v", resp, err)
	}
	switched, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(switched), `"org":"org_2"`) {
		t.Fatalf("SwitchOrganization must return the re-granted session: %s", switched)
	}
	if _, body := get(t, c, base+"/me"); !strings.Contains(body, `"org":"org_2"`) {
		t.Fatalf("after the switch: %s", body)
	}
}

// reversed is a codec a service would plug in to stay compatible with
// cookies written by another SDK.
type reversed struct{}

func (reversed) Seal(d *sdk.SessionData, pw string) (string, error) {
	s, err := sdk.SealSession(d, pw)
	return "rev:" + s, err
}

func (reversed) Unseal(s, pw string) (*sdk.SessionData, error) {
	rest, ok := strings.CutPrefix(s, "rev:")
	if !ok {
		return nil, errors.New("not mine")
	}
	d, err := sdk.Unseal[sdk.SessionData](rest, pw)
	return &d, err
}

func TestACustomSessionCodecReadsAndWritesTheCookie(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil, workos.WithSessionCodec(reversed{}))
	c := browser(t)
	setSessionCookie(c, base, "rev:"+f.sealed(-time.Minute))
	if resp, body := get(t, c, base+"/me"); resp.StatusCode != http.StatusOK {
		t.Fatalf("custom codec = %d %s", resp.StatusCode, body)
	}
	if got := sessionCookie(c, base); !strings.HasPrefix(got, "rev:") {
		t.Fatalf("the rotated cookie must be written by the codec: %q", got)
	}
}

func TestVerifyWebhook(t *testing.T) {
	f := newFake(t)
	var got atomic.Value
	base := start(t, f, func(a *gox.App) {
		a.HandleFunc("POST /webhooks/workos", func(w http.ResponseWriter, r *http.Request) {
			ev, err := workos.VerifyWebhook(a, r)
			if err != nil {
				gox.Error(w, r, err)
				return
			}
			got.Store(ev.Event)
			w.WriteHeader(http.StatusNoContent)
		})
	})
	body := `{"object":"event","id":"event_1","event":"user.created","data":{},"created_at":"2026-01-01T00:00:00Z"}`
	ts := time.Now().UnixMilli()
	post := func(sig string) int {
		req, _ := http.NewRequest(http.MethodPost, base+"/webhooks/workos", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("WorkOS-Signature", sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	stamp := strings.TrimSpace(strings.Join([]string{big.NewInt(ts).String()}, ""))
	good := "t=" + stamp + ", v1=" + sdk.ComputeWebhookSignature("whsec_test", stamp, body)
	if code := post(good); code != http.StatusNoContent || got.Load() != "user.created" {
		t.Fatalf("signed webhook = %d %v", code, got.Load())
	}
	if code := post("t=" + stamp + ", v1=deadbeef"); code != http.StatusUnauthorized {
		t.Fatalf("forged webhook = %d", code)
	}
}

func TestEnableNamesTheMissingVariables(t *testing.T) {
	old := os.Args
	os.Args = []string{"svc"}
	defer func() { os.Args = old }()
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), workos.Enable())
	if err == nil || !strings.Contains(err.Error(), "SHOP_WORKOS_API_KEY") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("SHOP_WORKOS_API_KEY", "sk")
	t.Setenv("SHOP_WORKOS_CLIENT_ID", clientID)
	_, err = gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), workos.Enable(workos.WithSessions()))
	if err == nil || !strings.Contains(err.Error(), "SHOP_WORKOS_COOKIE_PASSWORD") || !strings.Contains(err.Error(), "32") {
		t.Fatalf("sessions without a cookie password: %v", err)
	}
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	old := os.Args
	os.Args = []string{"svc"}
	defer func() { os.Args = old }()
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: workos.From called but workos.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	workos.From(a)
}

// A single-page app ends the session with a fetch and navigates itself.
func TestEndSessionClearsTheCookieAndReturnsTheLogoutURL(t *testing.T) {
	f := newFake(t)
	base := start(t, f, func(a *gox.App) {
		a.HandleFunc("DELETE /api/session", func(w http.ResponseWriter, r *http.Request) {
			_ = gox.JSON(w, http.StatusOK, map[string]string{"logout_url": workos.EndSession(w, r, "https://shop.example/")})
		})
	})
	c := browser(t)
	setSessionCookie(c, base, f.sealed(time.Hour))
	req, _ := http.NewRequest(http.MethodDelete, base+"/api/session", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "/user_management/sessions/logout?session_id=session_01") || !strings.Contains(string(body), "return_to=") {
		t.Fatalf("body = %s", body)
	}
	if sessionCookie(c, base) != "" {
		t.Fatal("the cookie must be cleared")
	}
	// Anonymous: nothing to end remotely.
	anonymous, _ := http.NewRequest(http.MethodDelete, base+"/api/session", nil)
	resp, _ = browser(t).Do(anonymous)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"logout_url":""`) {
		t.Fatalf("anonymous = %s", body)
	}
}

// A refresh token is single-use, so the request that loses the race to
// exchange it must be given the session the winner obtained. Before this was
// handled, the loser exchanged the consumed token, WorkOS answered
// invalid_grant, and a burst of requests on an expired session logged the
// user out. The sequential form is the same bug without the timing.
func TestARequestStillHoldingTheConsumedRefreshTokenStaysAuthenticated(t *testing.T) {
	f := newFake(t)
	base := start(t, f, nil)
	old := f.sealed(-time.Minute)

	first := browser(t)
	setSessionCookie(first, base, old)
	if resp, body := get(t, first, base+"/me"); resp.StatusCode != http.StatusOK {
		t.Fatalf("the first request = %d %s", resp.StatusCode, body)
	}

	// Another request carrying the same cookie, arriving after the exchange
	// finished rather than while it was in flight.
	second := browser(t)
	setSessionCookie(second, base, old)
	resp, body := get(t, second, base+"/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the request holding the consumed token = %d %s, want 200", resp.StatusCode, body)
	}
	if n := f.refreshes.Load(); n != 1 {
		t.Fatalf("refresh grants = %d, want 1: the consumed token must not be exchanged again", n)
	}
}
