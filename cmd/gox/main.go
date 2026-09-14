// Command gox is the framework's CLI.
//
//	gox docs [-o llm.txt] [-check]   regenerate llm.txt from source (or verify it is current)
//
// `gox new` (the scaffolder) arrives in Phase 5.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/guilhermebr/gox/cmd/gox/internal/llmdoc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "docs":
		os.Exit(docs(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gox docs [-root DIR] [-o FILE] [-check]")
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
	rootCfg := llmdoc.Package{Title: "gox (root)", ImportPath: rootPkg.ImportPath, Dir: configDir, Config: "Base", APIOnly: false, ConfigOnly: true}
	pkgs := append([]llmdoc.Package{rootAPI, rootCfg}, s.Packages[1:]...)
	s.Packages = pkgs
	return s
}
