package main

import (
	"strings"
	"testing"

	"github.com/guilhermebr/gox/cmd/gox/internal/llmdoc"
)

// llm.txt is for building a service: it carries the pkg/* packages
// 80-conventions.md lets a service import, and leaves the API feature
// packages use inside Enable to go doc.
func TestSpecCoversWhatServicesUse(t *testing.T) {
	out, err := llmdoc.Generate(spec("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`import "github.com/guilhermebr/gox/pkg/middleware"`, `import "github.com/guilhermebr/gox/pkg/httpclient"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("llm.txt lacks %s", want)
		}
	}
	for _, unwanted := range []string{"func (b *Builder) ", "func MustValue["} {
		if strings.Contains(string(out), unwanted) {
			t.Errorf("llm.txt has %q, which only feature packages call", unwanted)
		}
	}
}
