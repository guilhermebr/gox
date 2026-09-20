package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/middleware"
)

func TestClientIPTrustsForwardedHeadersOnlyFromTrustedProxies(t *testing.T) {
	cases := []struct {
		name, trusted, remote, xff, want string
	}{
		{"no proxy configured: the peer, whatever the header says", "", "203.0.113.9:4000", "1.2.3.4", "203.0.113.9"},
		{"peer is a trusted proxy: the address it reports", "10.0.0.0/8", "10.1.2.3:4000", "198.51.100.7", "198.51.100.7"},
		{"a spoofed entry in front of the real one is skipped", "10.0.0.0/8", "10.1.2.3:4000", "6.6.6.6, 198.51.100.7", "198.51.100.7"},
		{"two trusted hops: walk past both", "10.0.0.0/8,192.168.0.0/16", "10.1.2.3:4000", "198.51.100.7, 192.168.1.1", "198.51.100.7"},
		{"an untrusted peer cannot set the header", "10.0.0.0/8", "203.0.113.9:4000", "198.51.100.7", "203.0.113.9"},
		{"trusted peer, no header: the peer", "10.0.0.0/8", "10.1.2.3:4000", "", "10.1.2.3"},
		{"ipv6", "fd00::/8", "[fd00::1]:4000", "2001:db8::7", "2001:db8::7"},
		{"garbage in the header is ignored", "10.0.0.0/8", "10.1.2.3:4000", "not-an-ip", "10.1.2.3"},
	}
	for _, tc := range cases {
		mw, err := middleware.ClientIP(tc.trusted)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got string
		h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = middleware.ClientIPFrom(r) }))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if _, err := middleware.ClientIP("10.0.0.0/8, nonsense"); err == nil {
		t.Error("a bad CIDR must be an error")
	}
	// Without the middleware, the peer address.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:4000"
	if got := middleware.ClientIPFrom(r); got != "203.0.113.9" {
		t.Errorf("fallback = %q", got)
	}
}

func TestRateLimitAllowsABurstThenRejectsUntilTokensReturn(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	limited := middleware.RateLimit(3, time.Minute, func(r *http.Request) string { return r.Header.Get("X-Key") }, middleware.WithClock(clock))
	h := limited(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.Header.Set("X-Key", key)
		h.ServeHTTP(rec, r)
		return rec
	}
	for i := range 3 {
		if rec := call("a"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d = %d", i+1, rec.Code)
		}
	}
	rec := call("a")
	if rec.Code != http.StatusTooManyRequests || !contains(rec.Body.String(), `"code":"resource_exhausted"`) {
		t.Fatalf("fourth request = %d %s", rec.Code, rec.Body)
	}
	if retry, _ := strconv.Atoi(rec.Header().Get("Retry-After")); retry < 1 || retry > 20 {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	if rec := call("b"); rec.Code != http.StatusNoContent {
		t.Fatalf("another key has its own budget: %d", rec.Code)
	}
	now = now.Add(20 * time.Second) // one token every 20s
	if rec := call("a"); rec.Code != http.StatusNoContent {
		t.Fatalf("after a refill = %d", rec.Code)
	}
	if rec := call("a"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("only one token came back: %d", rec.Code)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
