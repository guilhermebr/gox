// Command gox is the framework's CLI.
//
//	gox new <name> [-module PATH] [-dir DIR] [-postgres] [-web] [-gox-dir DIR]
//	gox docs [-root DIR] [-o FILE] [-check]
//
// new renders a service that builds, tests, runs and answers /healthz with
// zero edits. docs regenerates llm.txt from source (or verifies it).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/guilhermebr/gox/cmd/gox/internal/llmdoc"
	"github.com/guilhermebr/gox/cmd/gox/internal/scaffold"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "new":
		os.Exit(newService(os.Args[2:]))
	case "docs":
		os.Exit(docs(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  gox new <name> [-module PATH] [-dir DIR] [-postgres] [-web] [-gox-dir DIR]")
	fmt.Fprintln(os.Stderr, "  gox docs [-root DIR] [-o FILE] [-check]")
}

func newService(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	module := fs.String("module", "", "Go module path (default: github.com/<user>/<name> is NOT guessed; default is the name)")
	dir := fs.String("dir", "", "target directory (default ./<name>)")
	postgres := fs.Bool("postgres", false, "add gox/postgres with a migrations package")
	web := fs.Bool("web", false, "add gox/web with a layout, a home page and static assets")
	goxDir := fs.String("gox-dir", "", "use a local gox checkout via replace directives (development)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gox new <name> [flags]")
		fs.PrintDefaults()
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fs.Usage()
		return 2
	}
	name := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *module == "" {
		*module = name
	}
	if *dir == "" {
		*dir = name
	}
	if *goxDir != "" {
		abs, err := filepath.Abs(*goxDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		*goxDir = abs
	}
	files, err := scaffold.Render(scaffold.Options{Name: name, Module: *module, Postgres: *postgres, Web: *web, GoxDir: *goxDir})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := scaffold.Write(*dir, files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("created %s (%d files)\n", *dir, len(files))

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = *dir
	tidy.Stdout, tidy.Stderr = os.Stdout, os.Stderr
	if err := tidy.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "go mod tidy failed (%v); run it yourself once the gox modules are reachable\n", err)
	}

	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	fmt.Printf("\nnext:\n  cd %s\n  make run                # :8080 public, :9090 admin\n  make test lint\n", *dir)
	if *postgres {
		fmt.Printf("  export %s_POSTGRES_URL=postgres://user:pass@localhost:5432/%s?sslmode=disable\n", prefix, name)
	}
	if *web {
		fmt.Printf("  make assets             # vendor htmx and Alpine.js into static/js\n")
	}
	fmt.Printf("  read AGENTS.md before handing it to an agent\n")
	return 0
}

// spec lists what llm.txt documents, in order. Adding a feature package
// means adding a line here.
func spec(root string) llmdoc.Spec {
	return llmdoc.Spec{
		Fragments: filepath.Join(root, "docs", "llm"),
		Packages: []llmdoc.Package{
			{Title: "gox (root)", ImportPath: "github.com/guilhermebr/gox", Dir: root, Config: "Base", Section: ""},
			{Title: "postgres", ImportPath: "github.com/guilhermebr/gox/postgres", Dir: filepath.Join(root, "postgres"), Config: "Config", Section: "POSTGRES", DeclaredBy: "postgres.Enable()"},
			{Title: "jwt", ImportPath: "github.com/guilhermebr/gox/jwt", Dir: filepath.Join(root, "jwt"), Config: "Config", Section: "JWT", DeclaredBy: "jwt.Enable()"},
			{Title: "supabase", ImportPath: "github.com/guilhermebr/gox/supabase", Dir: filepath.Join(root, "supabase"), Config: "Config", Section: "SUPABASE", DeclaredBy: "supabase.Enable()"},
			{Title: "web", ImportPath: "github.com/guilhermebr/gox/web", Dir: filepath.Join(root, "web"), Config: "Config", Section: "WEB", DeclaredBy: "web.Enable()"},
		},
	}
}

func docs(args []string) int {
	fs := flag.NewFlagSet("docs", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	out := fs.String("o", "llm.txt", "output file (relative to root)")
	check := fs.Bool("check", false, "exit 1 if the output file is not current")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// The base config lives in pkg/config, not in the root package: point
	// the root entry's config extraction there.
	s := spec(*root)
	generated, err := llmdoc.Generate(withBaseConfigDir(s, filepath.Join(*root, "pkg", "config")))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	target := filepath.Join(*root, *out)
	if *check {
		current, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(current, generated) {
			fmt.Fprintf(os.Stderr, "%s is stale: run `make llm`\n", *out)
			return 1
		}
		fmt.Printf("%s is current\n", *out)
		return 0
	}
	if err := os.WriteFile(target, generated, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(generated))
	return 0
}

// withBaseConfigDir splits the root entry so its API comes from the root
// package and its env vars from pkg/config.Base.
func withBaseConfigDir(s llmdoc.Spec, configDir string) llmdoc.Spec {
	rootPkg := s.Packages[0]
	rootAPI := rootPkg
	rootAPI.Config = ""
	rootCfg := llmdoc.Package{Title: "gox (root)", ImportPath: rootPkg.ImportPath, Dir: configDir, Config: "Base", ConfigOnly: true}
	pkgs := append([]llmdoc.Package{rootAPI, rootCfg}, s.Packages[1:]...)
	s.Packages = pkgs
	return s
}
