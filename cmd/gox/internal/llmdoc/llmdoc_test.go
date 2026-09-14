package llmdoc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guilhermebr/gox/cmd/gox/internal/llmdoc"
)

func TestEnvNameMatchesConfDerivation(t *testing.T) {
	tests := []struct {
		path []string
		want string
	}{
		{[]string{"HTTP", "Addr"}, "HTTP_ADDR"},
		{[]string{"HTTPClient", "Timeout"}, "HTTP_CLIENT_TIMEOUT"},
		{[]string{"Otel", "Enabled"}, "OTEL_ENABLED"},
		{[]string{"MaxConnLifetime"}, "MAX_CONN_LIFETIME"},
		{[]string{"URL"}, "URL"},
		{[]string{"SessionMaxAge"}, "SESSION_MAX_AGE"},
		{[]string{"Shutdown", "PreStopDelay"}, "SHUTDOWN_PRE_STOP_DELAY"},
		{[]string{"MaxBodyBytes"}, "MAX_BODY_BYTES"},
		{[]string{"ServiceName"}, "SERVICE_NAME"},
	}
	for _, tt := range tests {
		if got := llmdoc.EnvName(tt.path); got != tt.want {
			t.Errorf("EnvName(%v) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestEnvVarsFromStructTags(t *testing.T) {
	vars, err := llmdoc.EnvVars("testdata/fixture", "Config", "BILLING_FIXTURE")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]llmdoc.EnvVar{}
	for _, v := range vars {
		got[v.Name] = v
	}
	url := got["BILLING_FIXTURE_URL"]
	if url.Type != "string" || !url.Required || !url.Mask || url.Help != "where to connect" {
		t.Fatalf("URL = %+v", url)
	}
	if got["BILLING_FIXTURE_MAX_CONNS"].Default != "10" || got["BILLING_FIXTURE_MAX_CONNS"].Type != "int32" {
		t.Fatalf("MAX_CONNS = %+v", got["BILLING_FIXTURE_MAX_CONNS"])
	}
	if got["BILLING_FIXTURE_TIMEOUT"].Type != "duration" || got["BILLING_FIXTURE_TIMEOUT"].Default != "5s" {
		t.Fatalf("TIMEOUT = %+v", got["BILLING_FIXTURE_TIMEOUT"])
	}
	if _, ok := got["BILLING_FIXTURE_OLD_NAME"]; !ok {
		t.Fatalf("explicit env tag not honored: %v", got)
	}
	if _, ok := got["BILLING_FIXTURE_HTTP_CLIENT_TIMEOUT"]; !ok {
		t.Fatalf("nested struct not walked: %v", got)
	}
	if _, ok := got["BILLING_FIXTURE_SKIPPED"]; ok {
		t.Fatal("unexported field must be skipped")
	}
}

func TestAPIListsExportedDeclarationsWithFirstSentence(t *testing.T) {
	api, err := llmdoc.API("testdata/fixture", "github.com/example/fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func Enable(opts ...Option) Option  // Enable turns the fixture on.",
		"func From(a any) *Config  // From returns the thing.",
		"func (c *Config) Validate() error  // Validate checks the config.",
		"type Option func(*Config)  // Option configures Enable.",
		"type Kind int  // Kind classifies.",
		"var ErrNope error  // ErrNope is returned when nope.",
		"const KindA, KindB Kind  // Kinds.",
		"type Alias = Config  // Alias is Config under another name.",
		"type Config struct{ URL string; MaxConns int32; Timeout time.Duration; Legacy string; HTTPClient struct{ Timeout time.Duration } }  // Config is the FIXTURE section.",
	} {
		if !strings.Contains(api, want) {
			t.Errorf("api lacks %q:\n%s", want, api)
		}
	}
	if strings.Contains(api, "unexported") || strings.Contains(api, "Second sentence") {
		t.Fatalf("api leaked unexported or extra prose:\n%s", api)
	}
}

func TestGenerateAssemblesFragmentsAndSections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00-intro.md"), []byte("# intro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "90-outro.md"), []byte("# outro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := llmdoc.Generate(llmdoc.Spec{
		Fragments: dir,
		Packages: []llmdoc.Package{{
			Title: "fixture", ImportPath: "github.com/example/fixture", Dir: "testdata/fixture",
			Config: "Config", Section: "FIXTURE", DeclaredBy: "fixture.Enable()",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	intro, api, env, outro := strings.Index(s, "# intro"), strings.Index(s, "func Enable("), strings.Index(s, "<PREFIX>_FIXTURE_URL"), strings.Index(s, "# outro")
	if intro >= api || api >= env || env >= outro {
		t.Fatalf("section order wrong: intro=%d api=%d env=%d outro=%d\n%s", intro, api, env, outro, s)
	}
	if !strings.Contains(s, "declared by fixture.Enable()") {
		t.Fatalf("env table lacks the declaring option:\n%s", s)
	}
}
