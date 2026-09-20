package gox_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
)

func TestClientIPComesFromTrustedProxiesAndReachesTheAccessLog(t *testing.T) {
	t.Setenv("BILLING_HTTP_TRUSTED_PROXIES", "127.0.0.0/8")
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /ip", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(gox.ClientIP(r))) })
	}, nil)
	defer stop()
	if _, body := do(t, http.MethodGet, base+"/ip", nil, "X-Forwarded-For", "6.6.6.6, 198.51.100.7"); string(body) != "198.51.100.7" {
		t.Fatalf("client ip = %s", body)
	}
}

func TestABadTrustedProxyListFailsAtStartup(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_HTTP_TRUSTED_PROXIES", "everything")
	_, err := gox.New("billing", base(gox.HTTP())...)
	if err == nil || !strings.Contains(err.Error(), "HTTP_TRUSTED_PROXIES") {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitIsPerClientAndWrapsOneRoute(t *testing.T) {
	base, stop := runHTTP(t, func(a *gox.App) {
		limit := gox.RateLimit(2, time.Minute)
		a.Mux().Handle("POST /login", limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
		a.HandleFunc("POST /other", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}, nil)
	defer stop()
	for i, want := range []int{204, 204, 429} {
		if resp, _ := do(t, http.MethodPost, base+"/login", nil); resp.StatusCode != want {
			t.Fatalf("attempt %d = %d, want %d", i+1, resp.StatusCode, want)
		}
	}
	if resp, _ := do(t, http.MethodPost, base+"/other", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("other routes are not limited: %d", resp.StatusCode)
	}
}

type aliasConfig struct {
	gox.BaseConfig
	Upstream string `conf:"default:none"`
}

func TestWithEnvAliasReadsAPlatformVariableUnlessTheRealOneIsSet(t *testing.T) {
	setArgs(t)
	t.Setenv("LEGACY_UPSTREAM_URL", "https://from-the-platform")
	var cfg aliasConfig
	if _, err := gox.New("billing", base(gox.WithConfig(&cfg), gox.WithEnvAlias("UPSTREAM", "LEGACY_UPSTREAM_URL"))...); err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != "https://from-the-platform" {
		t.Fatalf("Upstream = %q", cfg.Upstream)
	}

	t.Setenv("BILLING_UPSTREAM", "https://explicit")
	var cfg2 aliasConfig
	if _, err := gox.New("billing", base(gox.WithConfig(&cfg2), gox.WithEnvAlias("UPSTREAM", "LEGACY_UPSTREAM_URL"))...); err != nil {
		t.Fatal(err)
	}
	if cfg2.Upstream != "https://explicit" {
		t.Fatalf("the service's own variable wins: %q", cfg2.Upstream)
	}
}

func TestPageHelpersAreReExported(t *testing.T) {
	token, err := gox.EncodeCursor(map[string]string{"id": "x1"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "/items?limit=5&after="+token, nil)
	p, err := gox.ParsePage(r, 50, 100)
	if err != nil || p.Limit != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	var c map[string]string
	if err := p.Cursor(&c); err != nil || c["id"] != "x1" {
		t.Fatalf("%v %v", c, err)
	}
}
