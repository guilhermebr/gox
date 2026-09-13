package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var assets = fstest.MapFS{
	"css/app.css":  {Data: []byte("body{margin:0}")},
	"js/app.js":    {Data: []byte("console.log(1)")},
	"img/logo.svg": {Data: []byte("<svg/>")},
}

func TestManifestHashesInProductionOnly(t *testing.T) {
	hashed, err := newManifest(assets, "/static/", true)
	if err != nil {
		t.Fatal(err)
	}
	u := hashed.url("css/app.css")
	if !strings.HasPrefix(u, "/static/css/app.") || !strings.HasSuffix(u, ".css") || u == "/static/css/app.css" {
		t.Fatalf("hashed url = %q", u)
	}
	if hashed.url("nope.css") != "/static/nope.css" {
		t.Fatalf("unknown asset should fall back to its plain path, got %q", hashed.url("nope.css"))
	}

	plain, _ := newManifest(assets, "/static/", false)
	if plain.url("css/app.css") != "/static/css/app.css" {
		t.Fatalf("development url = %q", plain.url("css/app.css"))
	}
}

func TestStaticHandlerServesHashedImmutableAndPlain(t *testing.T) {
	m, _ := newManifest(assets, "/static/", true)
	h := m.handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m.url("css/app.css"), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "body{margin:0}" {
		t.Fatalf("hashed: %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("hashed Cache-Control = %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q", ct)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("plain: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/app.deadbeef.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stale hash should be 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/../go.mod", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("path traversal served a file")
	}
}
