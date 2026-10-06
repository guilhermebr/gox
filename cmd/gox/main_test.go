package main

import (
	"strings"
	"testing"

	"github.com/guilhermebr/gox/cmd/gox/internal/llmdoc"
)

func generate(t *testing.T) string {
	t.Helper()
	out, err := llmdoc.Generate(spec("../.."))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// The pkg/* packages 80-conventions.md lets a service import must have
// their signatures in llm.txt, and only the symbols it names.
func TestSpecDocumentsThePkgSymbolsServicesImport(t *testing.T) {
	out := generate(t)
	for _, want := range []string{
		"import \"github.com/guilhermebr/gox/pkg/middleware\"",
		"func Bearer(",
		"func Principal(",
		"import \"github.com/guilhermebr/gox/pkg/httpclient\"",
		"func New(cfg Config, opts ...Option) *http.Client",
		"func WithRetry(",
		"func WithBearer(",
		"func WithUserAgent(",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("llm.txt lacks %q", want)
		}
	}
	for _, unwanted := range []string{"func Chain(", "func RouteCapture(", "func WithTracerProvider("} {
		if strings.Contains(out, unwanted) {
			t.Errorf("llm.txt has %q, which services never call", unwanted)
		}
	}
}

// llm.txt is for building a service: the API feature packages use inside
// Enable stays in go doc.
func TestSpecLeavesOutTheFeatureAuthorAPI(t *testing.T) {
	out := generate(t)
	for _, unwanted := range []string{
		"func (b *Builder) ",
		"func MustValue[",
		"func Value[",
		"func (a *App) Value(",
		"func (a *App) ConfigPrefix(",
		"func (a *App) HasHTTPClient(",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("llm.txt has %q", unwanted)
		}
	}
	for _, want := range []string{"type Builder struct", "func (a *App) HasHTTP() bool"} {
		if !strings.Contains(out, want) {
			t.Errorf("llm.txt lacks %q", want)
		}
	}
}
