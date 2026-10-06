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

func fixture() llmdoc.Package {
	return llmdoc.Package{Title: "fixture", ImportPath: "github.com/example/fixture", Dir: "testdata/fixture"}
}

func TestAPIListsExportedDeclarationsWithFirstSentence(t *testing.T) {
	api, err := llmdoc.API(fixture())
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
		// The first sentence ends at the earliest period, even at a line break.
		"func Wrapped()  // Wrapped ends its first sentence at a line break.\n",
		// A Deprecated: paragraph replaces the first sentence.
		"func Old()  // Deprecated: use Enable and From.\n",
		// Interfaces leave their method comments out.
		"type Store interface{ Put(ctx context.Context, key string) error; Get(key string) (string, error) }  // Store is an interface whose method comments stay out of llm.txt.\n",
		// Aliases of types inside the module show fields or signatures; others stay aliases.
		"type Options struct{ Name string; Nested sub.Inner; Pointer *sub.Inner; Every time.Duration }  // Options is sub.Options, shown with its fields.\n",
		"type Handler = func(name string, in sub.Inner) error  // Handler is sub.Handler, shown with its signature.\n",
		"type Level = sub.Level  // Level is sub.Level, left as an alias.\n",
	} {
		if !strings.Contains(api, want) {
			t.Errorf("api lacks %q:\n%s", want, api)
		}
	}
	if strings.Contains(api, "unexported") || strings.Contains(api, "Second sentence") {
		t.Fatalf("api leaked unexported or extra prose:\n%s", api)
	}
}

func TestAPIOnlyAndExcludeFilterDeclarations(t *testing.T) {
	only := fixture()
	only.Only = []string{"Enable", "Config", "Kind*"}
	api, err := llmdoc.API(only)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func Enable(", "type Config struct", "type Kind int", "const KindA, KindB Kind"} {
		if !strings.Contains(api, want) {
			t.Errorf("Only: api lacks %q:\n%s", want, api)
		}
	}
	for _, unwanted := range []string{"func From(", "Validate", "type Option ", "ErrNope"} {
		if strings.Contains(api, unwanted) {
			t.Errorf("Only: api has %q:\n%s", unwanted, api)
		}
	}

	exclude := fixture()
	exclude.Exclude = []string{"From", "Config.*", "KindB"}
	api, err = llmdoc.API(exclude)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"func From(", "Validate", "KindB"} {
		if strings.Contains(api, unwanted) {
			t.Errorf("Exclude: api has %q:\n%s", unwanted, api)
		}
	}
	for _, want := range []string{"func Enable(", "type Config struct", "const KindA Kind"} {
		if !strings.Contains(api, want) {
			t.Errorf("Exclude: api lacks %q:\n%s", want, api)
		}
	}
}

func TestAPISkipsTheConfigValidateItsSectionDocuments(t *testing.T) {
	p := fixture()
	p.Config, p.Section = "Config", "FIXTURE"
	api, err := llmdoc.API(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(api, "Validate") {
		t.Fatalf("api lists Validate, which config loading runs:\n%s", api)
	}
	if !strings.Contains(api, "type Config struct") {
		t.Fatalf("api lacks the Config type:\n%s", api)
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
