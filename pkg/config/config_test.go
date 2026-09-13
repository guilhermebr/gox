package config_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/config"
)

// setArgs replaces os.Args for the test because ardanlabs/conf reads it
// directly for --help and --version.
func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"svc"}, args...)
	t.Cleanup(func() { os.Args = old })
}

func TestBaseDefaults(t *testing.T) {
	setArgs(t)
	var b config.Base
	if err := config.Load("BILLING", &b); err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Environment", b.Environment, "development"},
		{"HTTP.Addr", b.HTTP.Addr, ":8080"},
		{"HTTP.ReadHeaderTimeout", b.HTTP.ReadHeaderTimeout, 10 * time.Second},
		{"Admin.Addr", b.Admin.Addr, ":9090"},
		{"Shutdown.Timeout", b.Shutdown.Timeout, 30 * time.Second},
		{"Shutdown.PreStopDelay", b.Shutdown.PreStopDelay, time.Duration(0)},
		{"Log.Level", b.Log.Level, "info"},
		{"Log.Format", b.Log.Format, "auto"},
		{"OTel.Enabled", b.Otel.Enabled, "auto"},
		{"OTel.Protocol", b.Otel.Protocol, "grpc"},
		{"HTTPClient.Timeout", b.HTTPClient.Timeout, 30 * time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestPrefixedAndUnprefixedEnvOverrides(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_HTTP_ADDR", ":9000")
	t.Setenv("BILLING_LOG_LEVEL", "debug")
	t.Setenv("HTTP_ADDR", ":7000")

	var prefixed config.Base
	if err := config.Load("BILLING", &prefixed); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prefixed.HTTP.Addr != ":9000" || prefixed.Log.Level != "debug" {
		t.Fatalf("prefixed = %+v", prefixed.HTTP)
	}

	var bare config.Base
	if err := config.Load("", &bare); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if bare.HTTP.Addr != ":7000" {
		t.Fatalf("unprefixed HTTP.Addr = %q, want :7000", bare.HTTP.Addr)
	}
}

func TestPortEnvIsHonoredUnlessAddrIsExplicit(t *testing.T) {
	setArgs(t)
	t.Setenv("PORT", "7777")

	var b config.Base
	if err := config.Load("SVC", &b); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.HTTP.Addr != ":7777" {
		t.Fatalf("HTTP.Addr = %q, want :7777 from PORT", b.HTTP.Addr)
	}

	t.Setenv("SVC_HTTP_ADDR", "127.0.0.1:8081")
	var explicit config.Base
	if err := config.Load("SVC", &explicit); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if explicit.HTTP.Addr != "127.0.0.1:8081" {
		t.Fatalf("explicit HTTP.Addr = %q; an explicit address must beat PORT", explicit.HTTP.Addr)
	}
}

func TestPresetFieldsSurviveAsDefaults(t *testing.T) {
	setArgs(t)
	b := config.Base{ServiceName: "billing", Version: "1.2.3"}
	if err := config.Load("BILLING", &b); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.ServiceName != "billing" || b.Version != "1.2.3" {
		t.Fatalf("preset fields were clobbered: %+v", b)
	}
}

type userConfig struct {
	config.Base
	InvoiceTTL time.Duration `conf:"default:24h"`
	Region     string        `conf:"required"`
}

type pgSection struct {
	URL      string `conf:"required,mask"`
	MaxConns int    `conf:"default:10"`
}

func TestLoadSectionsIsOnePassOverUserStructAndSections(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_REGION", "eu")
	t.Setenv("BILLING_INVOICE_TTL", "1h")
	t.Setenv("BILLING_POSTGRES_URL", "postgres://u:secret@db/x")

	var cfg userConfig
	var pg pgSection
	err := config.LoadSections("BILLING", &cfg, []config.Section{
		{Name: "POSTGRES", Dst: &pg, DeclaredBy: "postgres.Enable()"},
	})
	if err != nil {
		t.Fatalf("LoadSections: %v", err)
	}
	if cfg.Region != "eu" || cfg.InvoiceTTL != time.Hour {
		t.Fatalf("user struct = %+v", cfg)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("embedded Base not loaded: %+v", cfg.Base)
	}
	if pg.URL != "postgres://u:secret@db/x" || pg.MaxConns != 10 {
		t.Fatalf("section = %+v", pg)
	}
}

func TestMissingRequiredSectionValueNamesTheVariableAndTheFeature(t *testing.T) {
	setArgs(t)
	var b config.Base
	var pg pgSection
	err := config.LoadSections("BILLING", &b, []config.Section{
		{Name: "POSTGRES", Dst: &pg, DeclaredBy: "postgres.Enable()"},
	})
	if err == nil {
		t.Fatal("expected an error for the missing required URL")
	}
	msg := err.Error()
	for _, want := range []string{"BILLING_POSTGRES_URL", "required", "postgres.Enable()"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestBaseValidation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"bad environment", map[string]string{"X_ENVIRONMENT": "prod"}, "ENVIRONMENT"},
		{"bad log level", map[string]string{"X_LOG_LEVEL": "loud"}, "LOG_LEVEL"},
		{"bad log format", map[string]string{"X_LOG_FORMAT": "xml"}, "LOG_FORMAT"},
		{"bad otel enabled", map[string]string{"X_OTEL_ENABLED": "maybe"}, "OTEL_ENABLED"},
		{"bad shutdown timeout", map[string]string{"X_SHUTDOWN_TIMEOUT": "0s"}, "SHUTDOWN_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setArgs(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			var b config.Base
			err := config.Load("X", &b)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want mention of %s", err, tt.want)
			}
		})
	}
}

type validatingSection struct {
	Mode string `conf:"default:fast"`
}

func (v *validatingSection) Validate() error {
	if v.Mode != "fast" && v.Mode != "safe" {
		return errors.New("mode must be fast or safe")
	}
	return nil
}

func TestSectionValidateIsCalled(t *testing.T) {
	setArgs(t)
	t.Setenv("X_CACHE_MODE", "yolo")
	var b config.Base
	var c validatingSection
	err := config.LoadSections("X", &b, []config.Section{{Name: "CACHE", Dst: &c, DeclaredBy: "cache.Enable()"}})
	if err == nil || !strings.Contains(err.Error(), "mode must be fast or safe") || !strings.Contains(err.Error(), "CACHE") {
		t.Fatalf("error = %v", err)
	}
}

func TestHelpListsEveryVariableWithItsDeclaringFeature(t *testing.T) {
	setArgs(t, "--help")
	var cfg userConfig
	var pg pgSection
	err := config.LoadSections("BILLING", &cfg, []config.Section{
		{Name: "POSTGRES", Dst: &pg, DeclaredBy: "postgres.Enable()"},
	})
	var help *config.HelpError
	if !errors.As(err, &help) {
		t.Fatalf("err = %v, want *HelpError", err)
	}
	if !errors.Is(err, config.ErrHelp) {
		t.Fatal("HelpError must match ErrHelp with errors.Is")
	}
	for _, want := range []string{"BILLING_HTTP_ADDR", "BILLING_REGION", "BILLING_POSTGRES_URL", "postgres.Enable()"} {
		if !strings.Contains(help.Usage, want) {
			t.Errorf("usage does not mention %q:\n%s", want, help.Usage)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	setArgs(t, "--version")
	b := config.Base{ServiceName: "billing", Version: "1.2.3"}
	err := config.Load("BILLING", &b)
	var v *config.VersionError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want *VersionError", err)
	}
	if !strings.Contains(v.Info, "1.2.3") {
		t.Fatalf("version info = %q", v.Info)
	}
}

func TestDescribeMasksSecrets(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", "postgres://u:supersecret@db/x")
	var b config.Base
	var pg pgSection
	sections := []config.Section{{Name: "POSTGRES", Dst: &pg, DeclaredBy: "postgres.Enable()"}}
	if err := config.LoadSections("BILLING", &b, sections); err != nil {
		t.Fatalf("LoadSections: %v", err)
	}
	out, err := config.Describe("BILLING", &b, sections)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if strings.Contains(out, "supersecret") {
		t.Fatalf("Describe leaked a masked value:\n%s", out)
	}
	if !strings.Contains(out, "BILLING_POSTGRES_URL") || !strings.Contains(out, "BILLING_HTTP_ADDR") {
		t.Fatalf("Describe is missing variables:\n%s", out)
	}
}

func TestLoadRejectsStructsThatDoNotEmbedBase(t *testing.T) {
	setArgs(t)
	var plain struct {
		Region string `conf:"default:eu"`
	}
	err := config.Load("X", &plain)
	if err == nil || !strings.Contains(err.Error(), "embed") {
		t.Fatalf("err = %v, want a message about embedding config.Base", err)
	}
}
