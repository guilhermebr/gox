package scaffold_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/cmd/gox/internal/scaffold"
)

func render(t *testing.T, opts scaffold.Options) map[string]string {
	t.Helper()
	files, err := scaffold.Render(opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := map[string]string{}
	for k, v := range files {
		out[k] = string(v)
	}
	return out
}

func TestRenderBaseService(t *testing.T) {
	files := render(t, scaffold.Options{Name: "billing-api", Module: "github.com/acme/billing-api"})

	for _, want := range []string{
		"cmd/billing-api/main.go", "internal/hello/handler.go", "internal/hello/handler_test.go",
		"go.mod", "Makefile", "Dockerfile", ".golangci.yml", ".github/workflows/ci.yml",
		".env.example", ".gitignore", "README.md", "AGENTS.md", "CLAUDE.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s (have %v)", want, keys(files))
		}
	}
	main := files["cmd/billing-api/main.go"]
	if !strings.Contains(main, `gox.MustNew("billing-api"`) || !strings.Contains(main, "gox.HTTP()") {
		t.Fatalf("main.go:\n%s", main)
	}
	if strings.Contains(main, "postgres") || strings.Contains(main, "gox/web") {
		t.Fatalf("base main must not reference optional features:\n%s", main)
	}
	if !strings.Contains(main, `"github.com/acme/billing-api/internal/hello"`) {
		t.Fatalf("main.go must import the service's own packages by module path:\n%s", main)
	}
	if !strings.HasPrefix(files["go.mod"], "module github.com/acme/billing-api\n") {
		t.Fatalf("go.mod:\n%s", files["go.mod"])
	}
	env := files[".env.example"]
	if !strings.Contains(env, "BILLING_API_HTTP_ADDR=") || !strings.Contains(env, "BILLING_API_LOG_LEVEL=") {
		t.Fatalf(".env.example must use the uppercased name as prefix:\n%s", env)
	}
	if strings.Contains(env, "POSTGRES_URL") || strings.Contains(env, "WEB_SESSION_SECRET") {
		t.Fatalf(".env.example lists variables of features that are not enabled:\n%s", env)
	}
	if files["AGENTS.md"] != files["CLAUDE.md"] {
		t.Fatal("CLAUDE.md must be a copy of AGENTS.md")
	}
	if !strings.Contains(files["Dockerfile"], "distroless") || !strings.Contains(files["Dockerfile"], "./cmd/billing-api") {
		t.Fatalf("Dockerfile:\n%s", files["Dockerfile"])
	}
	for path := range files {
		if strings.Contains(path, "__name__") || strings.HasSuffix(path, ".tmpl") {
			t.Errorf("unrendered path %s", path)
		}
	}
}

func TestRenderPostgresAndWeb(t *testing.T) {
	files := render(t, scaffold.Options{Name: "shop", Module: "example.com/shop", Postgres: true, Web: true})

	for _, want := range []string{
		"migrations/migrations.go", "migrations/000001_init.up.sql", "migrations/000001_init.down.sql",
		"web/layout/layout.templ", "web/layout/layout_templ.go",
		"internal/home/handler.go", "internal/home/views/home.templ", "internal/home/views/home_templ.go",
		"static/static.go", "static/css/app.css",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	main := files["cmd/shop/main.go"]
	for _, want := range []string{
		`"github.com/guilhermebr/gox/postgres"`, "postgres.Enable(postgres.WithMigrations(migrations.FS))",
		`"github.com/guilhermebr/gox/web"`, "web.WithStatic(static.FS)", "web.WithLayout(layout.Layout)",
		`"example.com/shop/migrations"`, `"example.com/shop/web/layout"`, "home.Register(a)",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s:\n%s", want, main)
		}
	}
	env := files[".env.example"]
	if !strings.Contains(env, "SHOP_POSTGRES_URL=") || !strings.Contains(env, "SHOP_WEB_SESSION_SECRET=") {
		t.Fatalf(".env.example:\n%s", env)
	}
	if !strings.Contains(files["Makefile"], "templ") {
		t.Fatal("a web service's Makefile must have a generate target")
	}
}

func TestRenderRejectsBadNames(t *testing.T) {
	for _, name := range []string{"", "My Service", "1abc", "a/b", "-x"} {
		if _, err := scaffold.Render(scaffold.Options{Name: name, Module: "example.com/x"}); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := scaffold.Render(scaffold.Options{Name: "ok"}); err == nil {
		t.Error("empty module accepted")
	}
}

func TestGoxDirAddsReplaceDirectives(t *testing.T) {
	files := render(t, scaffold.Options{Name: "shop", Module: "example.com/shop", Postgres: true, GoxDir: "/src/gox"})
	gomod := files["go.mod"]
	if !strings.Contains(gomod, "replace github.com/guilhermebr/gox => /src/gox") ||
		!strings.Contains(gomod, "replace github.com/guilhermebr/gox/postgres => /src/gox/postgres") {
		t.Fatalf("go.mod:\n%s", gomod)
	}
	if strings.Contains(gomod, "gox/web =>") {
		t.Fatal("no replace for features that are not enabled")
	}
}

// TestGeneratedServiceBuildsTestsAndAnswersHealthz is the zero-edit
// guarantee: services from gox new must build, pass their own tests, run,
// and answer /healthz on the admin port without touching a file. The web
// variant runs without any environment; the postgres variant needs a
// database and is only run when DATABASE_URL is set (CI provides one),
// otherwise it is built and tested but not started. DATABASE_URL must
// point at a database the tests own: the service runs its migrations, and
// a schema_migrations table left by another project makes migrate fail.
func TestGeneratedServiceBuildsTestsAndAnswersHealthz(t *testing.T) {
	if testing.Short() {
		t.Skip("builds whole services; skipped with -short")
	}
	goxDir, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("web", func(t *testing.T) {
		dir := generate(t, scaffold.Options{Name: "shop", Module: "example.com/shop", Web: true, GoxDir: goxDir})
		probe(t, dir, "shop", nil)
	})
	t.Run("postgres+web", func(t *testing.T) {
		dir := generate(t, scaffold.Options{Name: "shop", Module: "example.com/shop", Postgres: true, Web: true, GoxDir: goxDir})
		url := os.Getenv("DATABASE_URL")
		if url == "" {
			t.Log("DATABASE_URL not set: built and tested, not started")
			return
		}
		probe(t, dir, "shop", []string{"SHOP_POSTGRES_URL=" + url})
	})
}

// generate renders and writes a service, then builds, vets and tests it.
func generate(t *testing.T, opts scaffold.Options) string {
	t.Helper()
	dir := t.TempDir()
	files, err := scaffold.Render(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffold.Write(dir, files); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("mod", "tidy")
	run("build", "./...")
	run("vet", "./...")
	run("test", "./...")
	run("build", "-o", filepath.Join(dir, opts.Name), "./cmd/"+opts.Name)
	return dir
}

// probe starts the built binary and checks /healthz on the admin port and
// GET / on the public port.
func probe(t *testing.T, dir, name string, extraEnv []string) {
	t.Helper()
	adminAddr, httpAddr := freeAddr(t), freeAddr(t)
	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, name))
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), prefix+"_ADMIN_ADDR="+adminAddr, prefix+"_HTTP_ADDR="+httpAddr, prefix+"_LOG_FORMAT=json"), extraEnv...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	deadline := time.Now().Add(15 * time.Second)
	status := 0
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + adminAddr + "/readyz")
		if err == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("/readyz on the admin port = %d; service output:\n%s", status, output.String())
	}
	resp, err := http.Get("http://" + adminAddr + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz: %v %v", err, resp)
	}
	_ = resp.Body.Close()
	resp, err = http.Get("http://" + httpAddr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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

func TestTemplVersionMatchesTheWebModule(t *testing.T) {
	b, err := os.ReadFile("../../../../web/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "github.com/a-h/templ "+scaffold.TemplVersion) {
		t.Fatalf("scaffold.TemplVersion %s does not match web/go.mod:\n%s", scaffold.TemplVersion, b)
	}
}
