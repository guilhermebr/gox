package gox_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/guilhermebr/gox"
)

func TestWithErrorRendererReplacesTheEnvelopeEverywhere(t *testing.T) {
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
			gox.Error(w, r, gox.InvalidArgument("the invoice is not valid").
				WithDetail("errors", []map[string]string{{"code": "blank", "message": "is required", "pointer": "/amountCents"}}))
		})
		a.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	}, nil, gox.WithErrorRenderer(gox.ProblemJSON))
	defer stop()

	resp, body := do(t, http.MethodGet, base+"/invoices/1", nil)
	if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p["title"] != "Bad Request" || p["status"] != float64(400) || p["code"] != "invalid_argument" ||
		p["detail"] != "the invoice is not valid" || p["request_id"] == "" || p["request_id"] == nil {
		t.Fatalf("problem = %s", body)
	}
	errs, _ := p["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["pointer"] != "/amountCents" {
		t.Fatalf("details must become extension members: %s", body)
	}

	// Framework rejections use the same renderer: unmatched routes and panics.
	for path, want := range map[string]int{"/nope": http.StatusNotFound, "/boom": http.StatusInternalServerError} {
		resp, body := do(t, http.MethodGet, base+path, nil)
		if resp.StatusCode != want || resp.Header.Get("Content-Type") != "application/problem+json" || !strings.Contains(string(body), `"title"`) {
			t.Errorf("%s = %d %q %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
	}
}

func TestCrossOriginStateChangingRequestsAreRejected(t *testing.T) {
	hits := 0
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("POST /transfer", func(w http.ResponseWriter, _ *http.Request) {
			hits++
			w.WriteHeader(http.StatusNoContent)
		})
		a.HandleFunc("GET /transfer", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}, []gox.HTTPOption{gox.WithCORS(gox.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}})})
	defer stop()

	cases := []struct {
		name    string
		method  string
		headers []string
		want    int
	}{
		{"no browser headers (curl, webhooks, server to server)", http.MethodPost, nil, http.StatusNoContent},
		{"same-origin browser request", http.MethodPost, []string{"Sec-Fetch-Site", "same-origin"}, http.StatusNoContent},
		{"cross-site browser request", http.MethodPost, []string{"Sec-Fetch-Site", "cross-site"}, http.StatusForbidden},
		{"old browser, foreign Origin", http.MethodPost, []string{"Origin", "https://evil.example"}, http.StatusForbidden},
		{"origin allowed by WithCORS", http.MethodPost, []string{"Origin", "https://app.example.com", "Sec-Fetch-Site", "cross-site"}, http.StatusNoContent},
		{"safe methods are never checked", http.MethodGet, []string{"Sec-Fetch-Site", "cross-site"}, http.StatusNoContent},
	}
	for _, tc := range cases {
		resp, body := do(t, tc.method, base+"/transfer", nil, tc.headers...)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: %d %s", tc.name, resp.StatusCode, body)
		}
		if tc.want == http.StatusForbidden && !strings.Contains(string(body), `"code":"permission_denied"`) {
			t.Errorf("%s: the rejection must be the envelope: %s", tc.name, body)
		}
	}
	if hits != 3 {
		t.Fatalf("handler ran %d times, want 3", hits)
	}
}

var spaFS = fstest.MapFS{
	"index.html":               {Data: []byte("<html><head><title>shop</title></head><body><div id=root></div></body></html>")},
	"assets/app-BqL3x9Zk.js":   {Data: []byte("console.log('app')")},
	"favicon.ico":              {Data: []byte("ico")},
	"assets/nested/index.html": {Data: []byte("nested")},
}

func TestSPAServesFilesTheShellAndKeepsServerPrefixesOutOfIt(t *testing.T) {
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /api/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	}, []gox.HTTPOption{gox.WithSPA(gox.SPAConfig{
		FS:             spaFS,
		ServerPrefixes: []string{"/api/", "/webhooks/"},
		Head: func(r *http.Request) string {
			return `<meta name="current-path" content="` + r.URL.Path + `">`
		},
	})})
	defer stop()

	resp, body := do(t, http.MethodGet, base+"/assets/app-BqL3x9Zk.js", nil)
	if resp.StatusCode != http.StatusOK || string(body) != "console.log('app')" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed asset = %d %q cache %q", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
	}
	resp, _ = do(t, http.MethodGet, base+"/favicon.ico", nil)
	if resp.StatusCode != http.StatusOK || strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("unhashed file = %d cache %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}

	for _, path := range []string{"/", "/invoices/42", "/settings/team"} {
		resp, body := do(t, http.MethodGet, base+path, nil)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") ||
			resp.Header.Get("Cache-Control") != "no-cache" || !strings.Contains(string(body), `<div id=root>`) {
			t.Fatalf("%s = %d %q %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
		if !strings.Contains(string(body), `<meta name="current-path" content="`+path+`"></head>`) {
			t.Fatalf("%s: Head must be injected before </head>: %s", path, body)
		}
	}

	if _, body := do(t, http.MethodGet, base+"/api/ping", nil); string(body) != "pong" {
		t.Fatalf("registered routes win: %s", body)
	}
	for _, path := range []string{"/api/nope", "/webhooks/unknown", "/api"} {
		resp, body := do(t, http.MethodGet, base+path, nil)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"not_found"`) {
			t.Errorf("%s must be an envelope 404, got %d %s", path, resp.StatusCode, body)
		}
	}
	if resp, body := do(t, http.MethodPost, base+"/invoices/42", nil); resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"not_found"`) {
		t.Errorf("POST to a client-side path = %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, http.MethodGet, base+"/healthz", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("health probes must keep working: %d", resp.StatusCode)
	}
	// A missing file with an extension is a 404, not the shell.
	if resp, _ := do(t, http.MethodGet, base+"/assets/missing.js", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing asset = %d", resp.StatusCode)
	}
}

func TestSPARequiresAnIndex(t *testing.T) {
	setArgs(t)
	_, err := gox.New("billing", base(gox.HTTP(gox.WithSPA(gox.SPAConfig{FS: fstest.MapFS{}})))...)
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("err = %v", err)
	}
}
