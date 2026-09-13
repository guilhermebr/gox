package gox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/log"
)

// runHTTP builds an app with gox.HTTP() on a free port, wires routes with
// setup, runs it, and returns the base URL and a stop function.
func runHTTP(t *testing.T, setup func(a *gox.App), httpOpts []gox.HTTPOption, opts ...gox.Option) (url string, stop func()) {
	t.Helper()
	setArgs(t)
	addr := freeAddr(t)
	t.Setenv("BILLING_HTTP_ADDR", addr)

	a, err := gox.New("billing", append(base(gox.HTTP(httpOpts...), gox.WithVersion("1.2.3")), opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if setup != nil {
		setup(a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool { return a.Health().IsReady() })
	return "http://" + addr, func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("RunContext = %v", err)
		}
	}
}

func do(t *testing.T, method, url string, body io.Reader, headers ...string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, b
}

func TestHTTPServesRoutesHealthAndEnvelopes(t *testing.T) {
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
			_ = gox.JSON(w, http.StatusOK, map[string]string{"hello": r.PathValue("name")})
		})
		a.HandleFunc("GET /missing", func(w http.ResponseWriter, r *http.Request) {
			gox.Error(w, r, gox.NotFound("invoice %s", "inv_1"))
		})
	}, nil)
	defer stop()

	resp, body := do(t, http.MethodGet, base+"/hello/ana", nil)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"hello":"ana"}` {
		t.Fatalf("hello = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("responses must carry X-Request-ID")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}

	resp, body = do(t, http.MethodGet, base+"/missing", nil, "X-Request-ID", "client-1")
	var env map[string]any
	_ = json.Unmarshal(body, &env)
	if resp.StatusCode != http.StatusNotFound || env["code"] != "not_found" || env["request_id"] != "client-1" {
		t.Fatalf("missing = %d %s", resp.StatusCode, body)
	}

	resp, body = do(t, http.MethodGet, base+"/nope", nil)
	_ = json.Unmarshal(body, &env)
	if resp.StatusCode != http.StatusNotFound || env["code"] != "not_found" {
		t.Fatalf("unmatched route = %d %s; want the envelope, not net/http's text", resp.StatusCode, body)
	}

	for _, p := range []string{"/healthz", "/readyz"} {
		if resp, _ := do(t, http.MethodGet, base+p, nil); resp.StatusCode != http.StatusOK {
			t.Errorf("%s on the public port = %d", p, resp.StatusCode)
		}
	}
}

func TestHTTPAppliesErrorMappersAndUserMiddlewareAndAuth(t *testing.T) {
	errNoRows := errors.New("no rows")
	mapper := func(err error) error {
		if errors.Is(err, errNoRows) {
			return gox.WrapError(err, gox.CodeNotFound, "not found")
		}
		return err
	}
	stamp := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Stamp", "yes")
			next.ServeHTTP(w, r)
		})
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" && !strings.HasPrefix(r.URL.Path, "/health") && r.URL.Path != "/readyz" {
				gox.Error(w, r, gox.Unauthenticated("missing token"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /row", func(w http.ResponseWriter, r *http.Request) {
			gox.Error(w, r, errNoRows)
		})
	}, nil, gox.WithErrorMapper(mapper), gox.WithMiddleware(stamp), gox.WithAuth(auth))
	defer stop()

	resp, body := do(t, http.MethodGet, base+"/row", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without token = %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"unauthenticated"`) {
		t.Fatalf("auth rejection must use the envelope: %s", body)
	}

	resp, body = do(t, http.MethodGet, base+"/row", nil, "Authorization", "Bearer x")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("mapped error = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Stamp") != "yes" {
		t.Fatal("user middleware was not applied")
	}
}

func TestHTTPCORSAndBodyLimitComeFromOptionsAndConfig(t *testing.T) {
	t.Setenv("BILLING_HTTP_MAX_BODY_BYTES", "16")
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
			var v map[string]any
			if err := gox.Decode(r, &v); err != nil {
				gox.Error(w, r, err)
				return
			}
			_ = gox.JSON(w, http.StatusOK, v)
		})
	}, []gox.HTTPOption{gox.WithCORS(gox.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}})})
	defer stop()

	resp, _ := do(t, http.MethodOptions, base+"/echo", nil,
		"Origin", "https://app.example.com", "Access-Control-Request-Method", "POST")
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("preflight = %d %v", resp.StatusCode, resp.Header)
	}

	resp, body := do(t, http.MethodPost, base+"/echo", strings.NewReader(`{"k":"`+strings.Repeat("v", 50)+`"}`))
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), "too large") {
		t.Fatalf("over-limit body = %d %s", resp.StatusCode, body)
	}

	resp, body = do(t, http.MethodPost, base+"/echo", strings.NewReader(`{"k":1}`))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"k":1}` {
		t.Fatalf("echo = %d %s", resp.StatusCode, body)
	}
}

func TestHTTPRequestTimeoutFromConfig(t *testing.T) {
	t.Setenv("BILLING_HTTP_REQUEST_TIMEOUT", "30ms")
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /slow", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		})
	}, nil)
	defer stop()
	resp, body := do(t, http.MethodGet, base+"/slow", nil)
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(string(body), "deadline_exceeded") {
		t.Fatalf("slow = %d %s", resp.StatusCode, body)
	}
}

func TestHTTPAccessLogCarriesRequestID(t *testing.T) {
	setArgs(t)
	addr := freeAddr(t)
	t.Setenv("BILLING_HTTP_ADDR", addr)
	var buf bytes.Buffer
	logger, _ := log.New(log.Config{Level: "info", Format: "json"}, log.WithWriter(&buf))

	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(logger), gox.HTTP())
	if err != nil {
		t.Fatal(err)
	}
	a.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool { return a.Health().IsReady() })
	do(t, http.MethodGet, "http://"+addr+"/ping", nil, "X-Request-ID", "trace-me")
	cancel()
	<-done

	if !strings.Contains(buf.String(), `"request_id":"trace-me"`) || !strings.Contains(buf.String(), `"route":"GET /ping"`) {
		t.Fatalf("access log missing request id or route:\n%s", buf.String())
	}
}

func TestMuxPanicsWithoutHTTPOption(t *testing.T) {
	setArgs(t)
	a, err := gox.New("billing", base()...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		want := "gox: a.Mux called but gox.HTTP() was not passed to gox.New"
		if r := recover(); r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	a.Mux()
}

func TestHTTPClientOptionBuildsAClientWithServiceUserAgent(t *testing.T) {
	setArgs(t)
	a, err := gox.New("billing", base(gox.HTTPClient(), gox.WithVersion("1.2.3"))...)
	if err != nil {
		t.Fatal(err)
	}
	c := a.HTTPClient()
	if c == nil || c.Timeout != 30*time.Second {
		t.Fatalf("client = %+v", c)
	}
	var ua string
	srvURL, stop := runHTTP(t, func(app *gox.App) {
		app.HandleFunc("GET /ua", func(_ http.ResponseWriter, r *http.Request) { ua = r.Header.Get("User-Agent") })
	}, nil)
	defer stop()
	resp, err := c.Get(srvURL + "/ua")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if ua != "billing/1.2.3" {
		t.Fatalf("User-Agent = %q", ua)
	}

	plain, _ := gox.New("billing", base()...)
	defer func() {
		if recover() == nil {
			t.Fatal("HTTPClient without the option must panic")
		}
	}()
	plain.HTTPClient()
}

func TestAdminMetricsExposeHTTPServerDuration(t *testing.T) {
	setArgs(t)
	httpAddr, adminAddr := freeAddr(t), freeAddr(t)
	t.Setenv("BILLING_HTTP_ADDR", httpAddr)
	t.Setenv("BILLING_ADMIN_ADDR", adminAddr)

	a, err := gox.New("billing", gox.WithLogger(quiet()), gox.HTTP())
	if err != nil {
		t.Fatal(err)
	}
	a.HandleFunc("GET /x", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool { return a.Health().IsReady() })
	defer func() {
		cancel()
		<-done
	}()

	do(t, http.MethodGet, "http://"+httpAddr+"/x", nil)
	_, body := do(t, http.MethodGet, "http://"+adminAddr+"/metrics", nil)
	if !strings.Contains(string(body), "http_server_request_duration_seconds") || !strings.Contains(string(body), `http_route="/x"`) {
		t.Fatalf("/metrics lacks the request histogram:\n%.2000s", body)
	}
}
