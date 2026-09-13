package web_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/a-h/templ"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

var assets = fstest.MapFS{
	"css/app.css": {Data: []byte("body{margin:0}")},
}

func text(s string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	})
}

// run builds a gox app with HTTP + web.Enable(webOpts...), lets setup register
// routes, runs it and returns a client with a cookie jar plus the base URL.
func run(t *testing.T, webOpts []web.Option, setup func(a *gox.App), opts ...gox.Option) (*http.Client, string) {
	t.Helper()
	setArgs(t)
	addr := freeAddr(t)
	t.Setenv("SHOP_HTTP_ADDR", addr)
	all := append([]gox.Option{gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), web.Enable(webOpts...)}, opts...)
	a, err := gox.New("shop", all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if setup != nil {
		setup(a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for !a.Health().IsReady() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("RunContext = %v", err)
		}
	})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, "http://" + addr
}

func get(t *testing.T, c *http.Client, url string, headers ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func postForm(t *testing.T, c *http.Client, url string, v url.Values, headers ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func TestEnableRequiresHTTP(t *testing.T) {
	setArgs(t)
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), web.Enable())
	if err == nil || !strings.Contains(err.Error(), "gox.HTTP()") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderAppliesLayoutExceptForHTMXPartials(t *testing.T) {
	layout := func(page *web.Page, body templ.Component) templ.Component {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			_, _ = io.WriteString(w, "<html><title>"+page.Title+"</title><body>")
			if err := body.Render(ctx, w); err != nil {
				return err
			}
			_, err := io.WriteString(w, "</body></html>")
			return err
		})
	}
	c, base := run(t, []web.Option{web.WithLayout(layout)}, func(a *gox.App) {
		a.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			web.PageFrom(r).Title = "Home"
			_ = web.Render(w, r, text("<p>hi</p>"))
		})
		a.HandleFunc("GET /part", func(w http.ResponseWriter, r *http.Request) {
			_ = web.Partial(w, r, text("<p>part</p>"))
		})
	})

	resp, body := get(t, c, base+"/")
	if resp.StatusCode != http.StatusOK || body != "<html><title>Home</title><body><p>hi</p></body></html>" {
		t.Fatalf("full page: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	_, body = get(t, c, base+"/", "HX-Request", "true")
	if body != "<p>hi</p>" {
		t.Fatalf("htmx partial request should skip the layout: %q", body)
	}
	_, body = get(t, c, base+"/", "HX-Request", "true", "HX-Boosted", "true")
	if !strings.HasPrefix(body, "<html>") {
		t.Fatalf("hx-boost navigation must get the full layout: %q", body)
	}
	_, body = get(t, c, base+"/part")
	if body != "<p>part</p>" {
		t.Fatalf("Partial must never wrap: %q", body)
	}
}

func TestDefaultLayoutAndErrorPages(t *testing.T) {
	c, base := run(t, nil, func(a *gox.App) {
		a.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			web.PageFrom(r).Title = "Shop"
			_ = web.Render(w, r, text("<p>welcome</p>"))
		})
		a.HandleFunc("GET /missing", func(w http.ResponseWriter, r *http.Request) {
			web.Error(w, r, gox.NotFound("no such thing"))
		})
		a.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	})

	_, body := get(t, c, base+"/")
	if !strings.Contains(body, "<title>Shop</title>") || !strings.Contains(body, "<p>welcome</p>") {
		t.Fatalf("default layout: %q", body)
	}

	resp, body := get(t, c, base+"/missing", "Accept", "text/html")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "<html") || !strings.Contains(body, "no such thing") {
		t.Fatalf("web.Error html: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, c, base+"/missing", "Accept", "application/json")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("web.Error json: %d %q", resp.StatusCode, body)
	}

	resp, body = get(t, c, base+"/nope", "Accept", "text/html")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "<html") || !strings.Contains(body, "404") {
		t.Fatalf("framework 404 as html: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, c, base+"/nope", "Accept", "application/json")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("framework 404 as json: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, c, base+"/boom", "Accept", "text/html")
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(body, "500") || strings.Contains(body, "kaboom") {
		t.Fatalf("panic page: %d %q", resp.StatusCode, body)
	}
}

func TestSessionsCSRFAndFlash(t *testing.T) {
	t.Setenv("SHOP_WEB_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	c, base := run(t, []web.Option{web.WithSessions()}, func(a *gox.App) {
		a.HandleFunc("GET /form", func(w http.ResponseWriter, r *http.Request) {
			_ = web.Partial(w, r, text("csrf="+web.PageFrom(r).CSRFToken))
		})
		a.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
			s := web.SessionFrom(r)
			s.Set("name", "ana")
			s.SetToken("tok-1")
			web.AddFlash(w, r, "success", "signed in")
			web.Redirect(w, r, "/me")
		})
		a.HandleFunc("GET /me", web.RequireSession(func(w http.ResponseWriter, r *http.Request) {
			name, _ := web.SessionFrom(r).Get("name")
			p := web.PageFrom(r)
			flashes := ""
			for _, f := range p.Flashes {
				flashes += f.Kind + ":" + f.Message
			}
			_ = web.Partial(w, r, text("name="+name+" token="+web.SessionFrom(r).Token()+" flash="+flashes))
		}))
		a.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
			web.SessionFrom(r).Clear()
			web.Redirect(w, r, "/form")
		})
	})

	// Anonymous: protected page redirects to /login.
	resp, _ := get(t, c, base+"/me")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("anonymous /me = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// CSRF: a POST without the token is refused.
	resp, body := postForm(t, c, base+"/login", url.Values{}, "Accept", "text/html")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "403") {
		t.Fatalf("csrf-less post = %d %q", resp.StatusCode, body)
	}

	// Get a token from a page, then post with it.
	_, body = get(t, c, base+"/form")
	token := strings.TrimPrefix(body, "csrf=")
	if len(token) < 16 {
		t.Fatalf("csrf token = %q", token)
	}
	resp, _ = postForm(t, c, base+"/login", url.Values{"_csrf": {token}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/me" {
		t.Fatalf("login = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "session" {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v", sessionCookie)
	}
	if strings.Contains(sessionCookie.Value, "tok-1") {
		t.Fatal("session cookie must not expose the token")
	}

	// Session and one-shot flash on the next page.
	_, body = get(t, c, base+"/me")
	if body != "name=ana token=tok-1 flash=success:signed in" {
		t.Fatalf("/me = %q", body)
	}
	_, body = get(t, c, base+"/me")
	if !strings.HasSuffix(body, "flash=") {
		t.Fatalf("flash must be consumed once: %q", body)
	}

	// Header form of the token, then logout clears everything.
	_, body = get(t, c, base+"/form")
	token = strings.TrimPrefix(body, "csrf=")
	resp, _ = postForm(t, c, base+"/logout", url.Values{}, "X-CSRF-Token", token)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	resp, _ = get(t, c, base+"/me")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("after logout /me = %d; session should be cleared", resp.StatusCode)
	}
}

func TestSessionsRequireASecretInProductionOnly(t *testing.T) {
	setArgs(t)
	t.Setenv("SHOP_ENVIRONMENT", "production")
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), web.Enable(web.WithSessions()))
	if err == nil || !strings.Contains(err.Error(), "SHOP_WEB_SESSION_SECRET") {
		t.Fatalf("production err = %v", err)
	}
	t.Setenv("SHOP_ENVIRONMENT", "development")
	if _, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), web.Enable(web.WithSessions())); err != nil {
		t.Fatalf("development should fall back to an ephemeral secret: %v", err)
	}
}

func TestSessionFromPanicsWithoutSessions(t *testing.T) {
	c, base := run(t, nil, func(a *gox.App) {
		a.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				r := recover()
				_, _ = io.WriteString(w, strings.TrimSpace(strings.SplitN(r.(string), "\n", 2)[0]))
			}()
			web.SessionFrom(r)
		})
	})
	_, body := get(t, c, base+"/")
	if body != "gox: web.SessionFrom called but web.WithSessions() was not passed to web.Enable" {
		t.Fatalf("panic = %q", body)
	}
}

func TestRedirectIsHTMXAware(t *testing.T) {
	c, base := run(t, nil, func(a *gox.App) {
		a.HandleFunc("GET /go", func(w http.ResponseWriter, r *http.Request) { web.Redirect(w, r, "/there") })
	})
	resp, _ := get(t, c, base+"/go")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/there" {
		t.Fatalf("plain redirect = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = get(t, c, base+"/go", "HX-Request", "true")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("HX-Redirect") != "/there" {
		t.Fatalf("htmx redirect = %d %q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
}

func TestStaticAssetsAndPageAsset(t *testing.T) {
	t.Setenv("SHOP_ENVIRONMENT", "production")
	c, base := run(t, []web.Option{web.WithStatic(assets)}, func(a *gox.App) {
		a.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			_ = web.Partial(w, r, text(web.PageFrom(r).Asset("css/app.css")))
		})
	})
	_, body := get(t, c, base+"/")
	if !strings.HasPrefix(body, "/static/css/app.") || body == "/static/css/app.css" {
		t.Fatalf("production asset url = %q", body)
	}
	resp, css := get(t, c, base+body)
	if resp.StatusCode != http.StatusOK || css != "body{margin:0}" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %d %q %q", resp.StatusCode, css, resp.Header.Get("Cache-Control"))
	}
}

func TestSessionUserAndBackendAPI(t *testing.T) {
	var auth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "u1", "name": "Ana"})
	}))
	defer backend.Close()
	t.Setenv("SHOP_WEB_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("SHOP_WEB_BACKEND_URL", backend.URL)

	type user struct{ ID, Name string }
	resolve := func(r *http.Request, s *web.Session) (any, error) {
		if s.Token() == "" {
			return nil, nil
		}
		var u user
		if err := web.APIFrom(r).Get(r.Context(), "/me", &u); err != nil {
			return nil, err
		}
		return u, nil
	}
	c, base := run(t, []web.Option{web.WithSessions(), web.WithBackend(), web.WithSessionUser(resolve)}, func(a *gox.App) {
		a.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
			web.SessionFrom(r).SetToken("tok-u1")
			_ = web.Partial(w, r, text("ok"))
		})
		a.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			u, _ := web.PageFrom(r).User.(user)
			_ = web.Partial(w, r, text("user="+u.Name))
		})
	}, gox.HTTPClient())

	get(t, c, base+"/login")
	_, body := get(t, c, base+"/")
	if body != "user=Ana" || auth != "Bearer tok-u1" {
		t.Fatalf("body=%q auth=%q", body, auth)
	}
}

func TestWithBackendRequiresHTTPClient(t *testing.T) {
	setArgs(t)
	t.Setenv("SHOP_WEB_BACKEND_URL", "http://api.internal")
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), web.Enable(web.WithBackend()))
	if err == nil || !strings.Contains(err.Error(), "gox.HTTPClient()") {
		t.Fatalf("err = %v", err)
	}
}

func TestFormPRGPattern(t *testing.T) {
	type signup struct {
		Email string `form:"email,required"`
	}
	c, base := run(t, nil, func(a *gox.App) {
		a.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
			in, ferrs, err := web.Form[signup](r)
			if err != nil {
				web.Error(w, r, err)
				return
			}
			if ferrs != nil {
				_ = web.RenderStatus(w, r, http.StatusUnprocessableEntity, text("email:"+ferrs["email"]))
				return
			}
			web.Redirect(w, r, "/welcome/"+in.Email)
		})
	})
	resp, body := postForm(t, c, base+"/signup", url.Values{}, "HX-Request", "true")
	if resp.StatusCode != http.StatusUnprocessableEntity || body != "email:required" {
		t.Fatalf("invalid = %d %q", resp.StatusCode, body)
	}
	resp, _ = postForm(t, c, base+"/signup", url.Values{"email": {"a@b.c"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/welcome/a@b.c" {
		t.Fatalf("valid = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
