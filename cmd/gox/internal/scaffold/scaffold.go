// Package scaffold renders a new service from the embedded template:
// `gox new <name> [--postgres] [--web]`. The result builds, tests, runs and
// answers /healthz with zero edits; a test proves it.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

//go:embed all:_template
var templates embed.FS

// TemplVersion is the templ release generated code in the template was
// produced with; the generated Makefile pins the CLI to it. A test keeps it
// equal to web/go.mod.
const TemplVersion = "v0.3.1020"

// Pinned front-end assets the generated Makefile downloads into static/js.
const (
	HTMXVersion   = "2.0.4"
	AlpineVersion = "3.14.8"
)

// Options describe the service to generate.
type Options struct {
	Name     string // service name: lowercase letters, digits, hyphens; also the binary and cmd/<name>
	Module   string // Go module path
	Postgres bool   // add gox/postgres with a migrations package
	Web      bool   // add gox/web with a layout, a home page and static assets
	GoxDir   string // when set, replace directives point the gox modules at this checkout (development)
}

// data is what the templates see.
type data struct {
	Options
	Prefix        string // uppercased name with hyphens as underscores: the env prefix
	GoVersion     string
	TemplVersion  string
	HTMXVersion   string
	AlpineVersion string
}

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Render produces every file of the new service as path → content. Paths
// are relative to the service root and use forward slashes.
func Render(opts Options) (map[string][]byte, error) {
	if !nameRE.MatchString(opts.Name) {
		return nil, fmt.Errorf("scaffold: name %q must be lowercase letters, digits and hyphens, starting with a letter", opts.Name)
	}
	if opts.Module == "" {
		return nil, errors.New("scaffold: module path is required")
	}
	d := data{
		Options:       opts,
		Prefix:        strings.ToUpper(strings.ReplaceAll(opts.Name, "-", "_")),
		GoVersion:     "1.26",
		TemplVersion:  TemplVersion,
		HTMXVersion:   HTMXVersion,
		AlpineVersion: AlpineVersion,
	}
	out := map[string][]byte{}
	layers := []string{"base"}
	if opts.Postgres {
		layers = append(layers, "postgres")
	}
	if opts.Web {
		layers = append(layers, "web")
	}
	for _, layer := range layers {
		if err := renderLayer(out, d, layer); err != nil {
			return nil, err
		}
	}
	out["CLAUDE.md"] = out["AGENTS.md"]
	return out, nil
}

func renderLayer(out map[string][]byte, d data, layer string) error {
	root := path.Join("_template", layer)
	return fs.WalkDir(templates, root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		rel = strings.ReplaceAll(rel, "__name__", d.Name)
		content, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".tmpl") {
			rel = strings.TrimSuffix(rel, ".tmpl")
			t, err := template.New(rel).Parse(string(content))
			if err != nil {
				return fmt.Errorf("scaffold: template %s: %w", p, err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, d); err != nil {
				return fmt.Errorf("scaffold: render %s: %w", p, err)
			}
			content = buf.Bytes()
		}
		out[rel] = content
		return nil
	})
}

// Write creates the files under dir. dir must not already contain any of
// them; an existing service is never overwritten.
func Write(dir string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("scaffold: %s already exists", full)
		}
	}
	for _, p := range paths {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			return err
		}
	}
	return nil
}
