package i18n_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/guilhermebr/gox/pkg/i18n"
)

var catalogs = fstest.MapFS{
	"en.json":    {Data: []byte(`{"greeting":"Hello, {name}!","errors":{"required":"is required","too_long":"must be at most %{max} characters"},"only_en":"English only"}`)},
	"es.json":    {Data: []byte(`{"greeting":"¡Hola, {name}!","errors":{"required":"es obligatorio"}}`)},
	"pt-BR.json": {Data: []byte(`{"greeting":"Olá, {name}!"}`)},
	"README.md":  {Data: []byte("not a catalog")},
}

func TestTranslateInterpolatesAndFallsBack(t *testing.T) {
	b, err := i18n.Load(catalogs, "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ locale, key, want string }{
		{"es", "greeting", "¡Hola, Ana!"},
		{"pt-BR", "greeting", "Olá, Ana!"},
		{"es", "errors.required", "es obligatorio"},
		{"es", "errors.too_long", "must be at most 40 characters"}, // missing in es: the fallback locale
		{"es", "only_en", "English only"},
		{"fr", "greeting", "Hello, Ana!"},    // unknown locale: the fallback locale
		{"en", "no.such.key", "no.such.key"}, // a missing key shows itself
	} {
		if got := b.T(tc.locale, tc.key, "name", "Ana", "max", 40); got != tc.want {
			t.Errorf("T(%s, %s) = %q, want %q", tc.locale, tc.key, got, tc.want)
		}
	}
	if got := strings.Join(b.Locales(), ","); got != "en,es,pt-BR" {
		t.Errorf("Locales = %s", got)
	}
}

func TestMatchPicksTheBestSupportedLocale(t *testing.T) {
	b, _ := i18n.Load(catalogs, "en")
	for header, want := range map[string]string{
		"":                        "en",
		"es-MX,es;q=0.9,en;q=0.8": "es",    // a regional variant matches its language
		"pt-BR,pt;q=0.9":          "pt-BR", // exact match
		"pt":                      "pt-BR", // a language matches its only regional catalog
		"fr-FR, de;q=0.9":         "en",
		"de;q=0.9, es;q=0.95, fr": "es", // quality order, skipping unsupported
		"*":                       "en",
	} {
		if got := b.Match(header); got != want {
			t.Errorf("Match(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestMessagesReturnsASubtreeForTheFrontend(t *testing.T) {
	b, _ := i18n.Load(catalogs, "en")
	got := b.Messages("es", "errors")
	if got["errors.required"] != "es obligatorio" || got["errors.too_long"] != "must be at most %{max} characters" || len(got) != 2 {
		t.Fatalf("Messages = %v", got)
	}
}

func TestMiddlewareResolvesTheLocaleFromTheCookieThenTheHeader(t *testing.T) {
	b, _ := i18n.Load(catalogs, "en")
	h := i18n.Middleware(b, "locale")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(i18n.Locale(r.Context()) + ":" + b.Tr(r.Context(), "greeting", "name", "Ana")))
	}))
	call := func(cookie, accept string) (string, string) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "locale", Value: cookie})
		}
		r.Header.Set("Accept-Language", accept)
		h.ServeHTTP(rec, r)
		return rec.Body.String(), rec.Header().Get("Content-Language")
	}
	if body, lang := call("es", "en"); body != "es:¡Hola, Ana!" || lang != "es" {
		t.Errorf("cookie wins: %q %q", body, lang)
	}
	if body, lang := call("xx", "pt-BR"); body != "pt-BR:Olá, Ana!" || lang != "pt-BR" {
		t.Errorf("an unsupported cookie falls to the header: %q %q", body, lang)
	}
	if body, _ := call("", ""); body != "en:Hello, Ana!" {
		t.Errorf("default: %q", body)
	}
}

func TestLoadRejectsBrokenCatalogsAndAMissingFallback(t *testing.T) {
	if _, err := i18n.Load(fstest.MapFS{"en.json": {Data: []byte(`{"a":`)}}, "en"); err == nil || !strings.Contains(err.Error(), "en.json") {
		t.Errorf("broken JSON: %v", err)
	}
	if _, err := i18n.Load(fstest.MapFS{"es.json": {Data: []byte(`{}`)}}, "en"); err == nil || !strings.Contains(err.Error(), "en.json") {
		t.Errorf("missing fallback: %v", err)
	}
	if _, err := i18n.Load(fstest.MapFS{"en.json": {Data: []byte(`{"a":[1,2]}`)}}, "en"); err == nil {
		t.Errorf("a non-string leaf must be refused")
	}
}
