package gox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/config"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"svc"}, args...)
	t.Cleanup(func() { os.Args = old })
}

// base returns the options every test starts from: no admin server, no
// log noise.
func base(opts ...gox.Option) []gox.Option {
	return append([]gox.Option{gox.WithoutAdminServer(), gox.WithLogger(quiet())}, opts...)
}

// --- a fake feature package, written the way postgres.Enable/From will be ---

type fakeConfig struct {
	URL      string `conf:"required"`
	PoolSize int    `conf:"default:4"`
}

type fakeKey struct{}

type fakeClient struct {
	url    string
	events *events
	ready  error
}

func (c *fakeClient) Name() string { return "fake" }
func (c *fakeClient) Start(context.Context) error {
	c.events.add("start fake")
	return nil
}

func (c *fakeClient) Stop(context.Context) error {
	c.events.add("stop fake")
	return nil
}
func (c *fakeClient) Ready(context.Context) error { return c.ready }

func fakeEnable(ev *events) gox.Option {
	return func(b *gox.Builder) error {
		cfg := &fakeConfig{}
		b.ConfigSection("FAKE", cfg, "fake.Enable()")
		b.Component(gox.StageDatastore, func(_ *gox.App) (lifecycle.Component, error) {
			c := &fakeClient{url: cfg.URL, events: ev}
			b.Set(fakeKey{}, c)
			return c, nil
		})
		return nil
	}
}

func fakeFrom(a *gox.App) *fakeClient {
	return gox.MustValue[*fakeClient](a, fakeKey{}, "fake.From", "fake.Enable()")
}

// --- test helpers ---

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, s)
}

func (e *events) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.list...)
}

type userComponent struct {
	name   string
	events *events
	run    func(ctx context.Context) error
}

func (u *userComponent) Name() string { return u.name }
func (u *userComponent) Start(context.Context) error {
	u.events.add("start " + u.name)
	return nil
}

func (u *userComponent) Stop(context.Context) error {
	u.events.add("stop " + u.name)
	return nil
}

type userRunner struct{ *userComponent }

func (u *userRunner) Run(ctx context.Context) error { return u.run(ctx) }

// --- tests ---

func TestNewLoadsBaseConfigUnderTheServicePrefix(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_HTTP_ADDR", ":9001")
	t.Setenv("BILLING_ENVIRONMENT", "staging")

	a, err := gox.New("billing", base(gox.WithVersion("1.2.3"))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Name() != "billing" {
		t.Fatalf("Name = %q", a.Name())
	}
	cfg := a.Config()
	if cfg.ServiceName != "billing" || cfg.Version != "1.2.3" || cfg.Environment != "staging" || cfg.HTTP.Addr != ":9001" {
		t.Fatalf("Config = %+v", cfg)
	}
	if a.Log() == nil || a.Health() == nil {
		t.Fatal("Log and Health must be available after New")
	}
}

func TestWithConfigLoadsTheUserStructInTheSamePass(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_REGION", "eu")

	type Config struct {
		gox.BaseConfig
		Region     string        `conf:"required"`
		InvoiceTTL time.Duration `conf:"default:24h"`
	}
	var cfg Config
	a, err := gox.New("billing", base(gox.WithConfig(&cfg))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cfg.Region != "eu" || cfg.InvoiceTTL != 24*time.Hour {
		t.Fatalf("user config = %+v", cfg)
	}
	if a.Config().ServiceName != "billing" || cfg.ServiceName != "billing" {
		t.Fatal("the embedded BaseConfig must be the one the app reports")
	}
}

func TestWithConfigRejectsStructsThatDoNotEmbedBaseConfig(t *testing.T) {
	setArgs(t)
	var plain struct {
		Region string `conf:"default:eu"`
	}
	_, err := gox.New("billing", base(gox.WithConfig(&plain))...)
	if err == nil || !strings.Contains(err.Error(), "embed") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigPrefixCanBeEmpty(t *testing.T) {
	setArgs(t)
	t.Setenv("HTTP_ADDR", ":9002")
	a, err := gox.New("billing", base(gox.WithConfigPrefix(""))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Config().HTTP.Addr != ":9002" {
		t.Fatalf("HTTP.Addr = %q; unprefixed variables must be honored", a.Config().HTTP.Addr)
	}
}

func TestInvalidConfigFailsNew(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_LOG_LEVEL", "loud")
	_, err := gox.New("billing", base()...)
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("err = %v", err)
	}
}

func TestHelpFlagSurfacesAsHelpError(t *testing.T) {
	setArgs(t, "--help")
	_, err := gox.New("billing", base(fakeEnable(&events{}))...)
	var help *config.HelpError
	if !errors.As(err, &help) {
		t.Fatalf("err = %v, want *config.HelpError", err)
	}
	if !strings.Contains(help.Usage, "BILLING_FAKE_URL") || !strings.Contains(help.Usage, "fake.Enable()") {
		t.Fatalf("usage lacks the feature section:\n%s", help.Usage)
	}
}

func TestFeatureSectionIsLoadedAndAccessorWorks(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_FAKE_URL", "fake://db")
	ev := &events{}

	a, err := gox.New("billing", base(fakeEnable(ev))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := fakeFrom(a)
	if c.url != "fake://db" {
		t.Fatalf("component built with url %q", c.url)
	}
	if got, ok := gox.Value[*fakeClient](a, fakeKey{}); !ok || got != c {
		t.Fatal("Value should return the same client")
	}
}

func TestMissingFeatureVariableNamesTheVariableAndTheOption(t *testing.T) {
	setArgs(t)
	_, err := gox.New("billing", base(fakeEnable(&events{}))...)
	if err == nil {
		t.Fatal("expected an error for the missing BILLING_FAKE_URL")
	}
	for _, want := range []string{"BILLING_FAKE_URL", "required", "fake.Enable()"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err %q does not mention %q", err, want)
		}
	}
}

func TestAccessorPanicsNamingTheOptionToAdd(t *testing.T) {
	setArgs(t)
	a, err := gox.New("billing", base()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		want := "gox: fake.From called but fake.Enable() was not passed to gox.New"
		if got, _ := r.(string); got != want {
			t.Fatalf("panic = %q\nwant  %q", r, want)
		}
	}()
	fakeFrom(a)
}

func TestRunStartsByStageMarksReadyAndStopsInReverse(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_FAKE_URL", "fake://db")
	ev := &events{}
	user := &userComponent{name: "worker", events: ev}

	a, err := gox.New("billing", base(fakeEnable(ev), gox.Component(user))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Health().IsReady() {
		t.Fatal("app must not be ready before Run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()

	waitFor(t, func() bool { return a.Health().IsReady() })
	if rep := a.Health().Ready(context.Background()); rep.Checks["fake"].Status != "ok" {
		t.Fatalf("the fake component's Ready check was not auto-registered: %+v", rep)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunContext = %v", err)
	}
	if a.Health().IsReady() {
		t.Fatal("app must be not-ready after shutdown")
	}
	want := []string{"start fake", "start worker", "stop worker", "stop fake"}
	if got := ev.snapshot(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRunReturnsFatalRunnerError(t *testing.T) {
	setArgs(t)
	ev := &events{}
	fatal := errors.New("listener died")
	r := &userRunner{userComponent: &userComponent{name: "smtp", events: ev}}
	r.run = func(context.Context) error { return fatal }

	a, err := gox.New("billing", base(gox.Component(r))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = a.RunContext(ctx)
	if !errors.Is(err, fatal) {
		t.Fatalf("RunContext = %v, want %v", err, fatal)
	}
	if got := ev.snapshot(); strings.Join(got, ",") != "start smtp,stop smtp" {
		t.Fatalf("events = %v", got)
	}
}

func TestRunReturnsStartErrorWithoutBecomingReady(t *testing.T) {
	setArgs(t)
	boom := errors.New("boom")
	a, err := gox.New("billing", base(gox.Component(failing{err: boom}))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.RunContext(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("RunContext = %v", err)
	}
	if a.Health().IsReady() {
		t.Fatal("must not be ready after a start failure")
	}
}

type failing struct{ err error }

func (f failing) Name() string                { return "failing" }
func (f failing) Start(context.Context) error { return f.err }
func (f failing) Stop(context.Context) error  { return nil }

func TestAddRegistersUserComponentsAfterNew(t *testing.T) {
	setArgs(t)
	ev := &events{}
	a, err := gox.New("billing", base()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Add(&userComponent{name: "late", events: ev})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool { return a.Health().IsReady() })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := ev.snapshot(); strings.Join(got, ",") != "start late,stop late" {
		t.Fatalf("events = %v", got)
	}
}

func TestPeriodicRunsAsAComponent(t *testing.T) {
	setArgs(t)
	var mu sync.Mutex
	runs := 0
	a, err := gox.New("billing", base(gox.Periodic("tick", 5*time.Millisecond, func(context.Context) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	}))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runs >= 2
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAdminServerServesHealthWhenEnabled(t *testing.T) {
	setArgs(t)
	addr := freeAddr(t)
	t.Setenv("BILLING_ADMIN_ADDR", addr)

	a, err := gox.New("billing", gox.WithLogger(quiet()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitFor(t, func() bool { return a.Health().IsReady() })

	resp, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz = %d", resp.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestErrorReExportsBuildCodedErrors(t *testing.T) {
	err := gox.NotFound("invoice %s", "x")
	if gox.CodeOf(err) != gox.CodeNotFound {
		t.Fatalf("CodeOf = %v", gox.CodeOf(err))
	}
	if gox.HTTPStatus(err) != http.StatusNotFound {
		t.Fatalf("HTTPStatus = %d", gox.HTTPStatus(err))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
