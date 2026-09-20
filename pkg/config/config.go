// Package config loads and validates a service's configuration in one pass:
// the framework's Base, the service's own struct (which embeds Base), and
// every config section a feature package registered, all under one env
// prefix. It is built on github.com/ardanlabs/conf/v3.
//
// One prefix per service: BILLING_HTTP_ADDR, BILLING_LOG_LEVEL,
// BILLING_POSTGRES_URL. An empty prefix is fully supported (HTTP_ADDR, ...).
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ardanlabs/conf/v3"
)

// DefaultHTTPAddr is the public listen address when neither HTTP_ADDR nor
// PORT is set.
const DefaultHTTPAddr = ":8080"

// Base is the configuration every gox service has. Services embed it in their
// own struct and pass that to gox.WithConfig. No datastore fields live here:
// those are sections registered by feature packages.
type Base struct {
	ServiceName string `conf:"env:SERVICE_NAME,help:service name; set by gox.New"`
	Version     string `conf:"env:VERSION,help:build version; set by gox.WithVersion"`
	Environment string `conf:"default:development,help:development | staging | production"`

	HTTP       HTTPConfig
	Admin      AdminConfig
	Shutdown   ShutdownConfig
	Log        LogConfig
	Otel       OtelConfig
	HTTPClient HTTPClientConfig
}

// HTTPConfig configures the public HTTP server.
type HTTPConfig struct {
	Addr              string        `conf:"help:listen address; PORT is honored when unset"`
	ReadHeaderTimeout time.Duration `conf:"default:10s"`
	ReadTimeout       time.Duration `conf:"default:30s"`
	WriteTimeout      time.Duration `conf:"default:30s"`
	IdleTimeout       time.Duration `conf:"default:120s"`
	RequestTimeout    time.Duration `conf:"default:30s,help:per-request deadline enforced by the middleware chain"`
	MaxBodyBytes      int64         `conf:"default:1048576,help:request body limit in bytes"`
	TrustedProxies    string        `conf:"help:CIDRs of the proxies in front of the service such as 10.0.0.0/8; X-Forwarded-For is believed only from them"`
}

// AdminConfig configures the ops server (/metrics, /healthz, /readyz, pprof).
type AdminConfig struct {
	Addr string `conf:"default::9090"`
}

// ShutdownConfig bounds graceful shutdown.
type ShutdownConfig struct {
	Timeout      time.Duration `conf:"default:30s,help:budget for each component's Stop"`
	PreStopDelay time.Duration `conf:"default:0s,help:wait after going not-ready before stopping"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level  string `conf:"default:info,help:debug | info | warn | error"`
	Format string `conf:"default:auto,help:auto | json | text"`
}

// OtelConfig configures tracing and metrics export.
type OtelConfig struct {
	Enabled  string `conf:"default:auto,help:auto | true | false; auto is on in production or when an endpoint is set"`
	Endpoint string `conf:"help:OTLP collector host:port"`
	Protocol string `conf:"default:grpc,help:grpc | http"`
	Insecure bool   `conf:"default:false,help:plaintext connection to the collector"`
}

// HTTPClientConfig configures the default outbound HTTP client.
type HTTPClientConfig struct {
	Timeout         time.Duration `conf:"default:30s"`
	MaxIdleConns    int           `conf:"default:100"`
	IdleConnTimeout time.Duration `conf:"default:90s"`
}

// goxBase marks Base and every struct that embeds it. It is unexported on
// purpose: an embedded field named after the type would shadow an exported
// method, while an unexported promoted method cannot be shadowed by accident.
func (b *Base) goxBase() *Base { return b }

// BaseOf returns the Base embedded in dst, or false if dst does not embed
// Base.
func BaseOf(dst any) (*Base, bool) {
	h, ok := dst.(baseHolder)
	if !ok {
		return nil, false
	}
	return h.goxBase(), true
}

// Validate checks the framework fields. Messages name the environment
// variable (without prefix) so the fix is obvious.
func (b *Base) Validate() error {
	var errs []error
	if !oneOf(b.Environment, "development", "staging", "production") {
		errs = append(errs, fmt.Errorf("config: ENVIRONMENT must be development, staging or production, got %q", b.Environment))
	}
	if !oneOf(strings.ToLower(b.Log.Level), "debug", "info", "warn", "warning", "error") {
		errs = append(errs, fmt.Errorf("config: LOG_LEVEL must be debug, info, warn or error, got %q", b.Log.Level))
	}
	if !oneOf(strings.ToLower(b.Log.Format), "auto", "json", "text") {
		errs = append(errs, fmt.Errorf("config: LOG_FORMAT must be auto, json or text, got %q", b.Log.Format))
	}
	if !oneOf(strings.ToLower(b.Otel.Enabled), "auto", "true", "false") {
		errs = append(errs, fmt.Errorf("config: OTEL_ENABLED must be auto, true or false, got %q", b.Otel.Enabled))
	}
	if !oneOf(strings.ToLower(b.Otel.Protocol), "grpc", "http") {
		errs = append(errs, fmt.Errorf("config: OTEL_PROTOCOL must be grpc or http, got %q", b.Otel.Protocol))
	}
	if b.HTTP.RequestTimeout <= 0 {
		errs = append(errs, fmt.Errorf("config: HTTP_REQUEST_TIMEOUT must be > 0, got %v", b.HTTP.RequestTimeout))
	}
	if b.HTTP.MaxBodyBytes <= 0 {
		errs = append(errs, fmt.Errorf("config: HTTP_MAX_BODY_BYTES must be > 0, got %v", b.HTTP.MaxBodyBytes))
	}
	if b.Shutdown.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("config: SHUTDOWN_TIMEOUT must be > 0, got %v", b.Shutdown.Timeout))
	}
	if b.Shutdown.PreStopDelay < 0 {
		errs = append(errs, fmt.Errorf("config: SHUTDOWN_PRE_STOP_DELAY must be >= 0, got %v", b.Shutdown.PreStopDelay))
	}
	return errors.Join(errs...)
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Validator is implemented by config structs that check cross-field rules.
type Validator interface {
	Validate() error
}

type baseHolder interface {
	goxBase() *Base
}

// Section is a feature package's config struct, loaded under
// <prefix>_<Name>_*. DeclaredBy names the option that registered it so error
// messages and --help say where a variable comes from.
type Section struct {
	Name       string
	Dst        any
	DeclaredBy string
}

// ErrHelp is matched by the error Load returns when --help was passed.
var ErrHelp = errors.New("config: help requested")

// ErrVersion is matched by the error Load returns when --version was passed.
var ErrVersion = errors.New("config: version requested")

// HelpError carries the usage text for --help.
type HelpError struct {
	Usage string
}

func (e *HelpError) Error() string { return ErrHelp.Error() }

// Is makes errors.Is(err, ErrHelp) true.
func (e *HelpError) Is(target error) bool { return target == ErrHelp }

// VersionError carries the version text for --version.
type VersionError struct {
	Info string
}

func (e *VersionError) Error() string { return ErrVersion.Error() }

// Is makes errors.Is(err, ErrVersion) true.
func (e *VersionError) Is(target error) bool { return target == ErrVersion }

// Load parses dst (which must embed Base) from the environment under prefix
// and validates it. Fields already set on dst act as defaults.
func Load(prefix string, dst any) error {
	return LoadSections(prefix, dst, nil)
}

// LoadSections is Load plus every feature section, in one pass, with one
// combined --help.
func LoadSections(prefix string, dst any, sections []Section) error {
	base, ok := BaseOf(dst)
	if !ok {
		return fmt.Errorf("config: %T must embed config.Base (gox.BaseConfig)", dst)
	}

	if _, err := conf.Parse(prefix, dst); err != nil {
		err = nameVariable(prefix, err)
		switch {
		case errors.Is(err, conf.ErrHelpWanted):
			usage, uerr := usage(prefix, dst, sections)
			if uerr != nil {
				return uerr
			}
			return &HelpError{Usage: usage}
		case errors.Is(err, conf.ErrVersionWanted):
			return &VersionError{Info: versionInfo(base)}
		}
		return fmt.Errorf("config: %w", err)
	}
	applyPort(prefix, base)

	var errs []error
	if err := base.Validate(); err != nil {
		errs = append(errs, err)
	}
	if v, ok := dst.(Validator); ok {
		if err := v.Validate(); err != nil && !sameError(err, errs) {
			errs = append(errs, err)
		}
	}

	for _, s := range sections {
		if _, err := conf.Parse(sectionPrefix(prefix, s.Name), s.Dst); err != nil {
			err = nameVariable(sectionPrefix(prefix, s.Name), err)
			errs = append(errs, fmt.Errorf("config: section %s (declared by %s): %w", s.Name, s.DeclaredBy, err))
			continue
		}
		if v, ok := s.Dst.(Validator); ok {
			if err := v.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("config: section %s (declared by %s): %w", s.Name, s.DeclaredBy, err))
			}
		}
	}
	return errors.Join(errs...)
}

// sameError avoids reporting Base.Validate twice when the user struct did
// not define its own Validate and the embedded one was promoted.
func sameError(err error, seen []error) bool {
	for _, s := range seen {
		if s.Error() == err.Error() {
			return true
		}
	}
	return false
}

var requiredField = regexp.MustCompile(`required field (\w+) is missing value`)

// nameVariable rewrites conf's "required field URL is missing value" into a
// message that names the environment variable to set.
func nameVariable(prefix string, err error) error {
	m := requiredField.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	return fmt.Errorf("%s is required: %w", EnvName(prefix, EnvKey(m[1])), err)
}

// EnvKey derives the environment key segment conf uses for a Go field name:
// HTTPClient -> HTTP_CLIENT, MaxConns -> MAX_CONNS, Otel -> OTEL.
func EnvKey(name string) string {
	runes := []rune(name)
	var sb strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prevLower := unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				sb.WriteByte('_')
			}
		}
		sb.WriteRune(unicode.ToUpper(r))
	}
	return sb.String()
}

func sectionPrefix(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "_" + name
}

// EnvName returns the full environment variable name for a key under prefix.
func EnvName(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "_" + key
}

// applyPort fills HTTP.Addr from PORT (set by Tsuru, Fly, Heroku) when the
// service did not set an address explicitly.
func applyPort(prefix string, b *Base) {
	if b.HTTP.Addr != "" {
		return
	}
	if port := os.Getenv("PORT"); port != "" && os.Getenv(EnvName(prefix, "HTTP_ADDR")) == "" {
		b.HTTP.Addr = ":" + port
		return
	}
	b.HTTP.Addr = DefaultHTTPAddr
}

func usage(prefix string, dst any, sections []Section) (string, error) {
	var sb strings.Builder
	u, err := conf.UsageInfo(prefix, dst)
	if err != nil {
		return "", fmt.Errorf("config: usage: %w", err)
	}
	sb.WriteString(u)
	for _, s := range sections {
		u, err := conf.UsageInfo(sectionPrefix(prefix, s.Name), s.Dst)
		if err != nil {
			return "", fmt.Errorf("config: usage for section %s: %w", s.Name, err)
		}
		fmt.Fprintf(&sb, "\n\n# Section %s (declared by %s)\n", s.Name, s.DeclaredBy)
		sb.WriteString(u)
	}
	return sb.String(), nil
}

func versionInfo(b *Base) string {
	if b.Version == "" {
		return b.ServiceName + " (version unknown)"
	}
	return b.ServiceName + " " + b.Version
}

// Describe renders every loaded value as ENV_NAME=value lines with masked
// fields hidden, for logging at startup.
func Describe(prefix string, dst any, sections []Section) (string, error) {
	var sb strings.Builder
	if err := describeOne(&sb, prefix, dst); err != nil {
		return "", err
	}
	for _, s := range sections {
		if err := describeOne(&sb, sectionPrefix(prefix, s.Name), s.Dst); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// describeOne converts conf's "--flag-name=value" lines to env-var form.
func describeOne(sb *strings.Builder, prefix string, v any) error {
	s, err := conf.String(v)
	if err != nil {
		return fmt.Errorf("config: describe: %w", err)
	}
	for line := range strings.SplitSeq(s, "\n") {
		flag, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		key := strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(flag, "--"), "-", "_"))
		sb.WriteString(EnvName(prefix, key))
		sb.WriteString("=")
		sb.WriteString(value)
		sb.WriteString("\n")
	}
	return nil
}
