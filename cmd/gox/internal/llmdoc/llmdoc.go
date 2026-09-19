// Package llmdoc generates llm.txt: the one file a model needs to build a
// service on gox. Static fragments (purpose, canonical mains, layout,
// conventions) are read from a directory; the public API and the
// environment variables are extracted from Go source with go/doc and
// go/ast so they cannot drift from the code.
package llmdoc

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/guilhermebr/gox/pkg/config"
)

// Package describes one source package to document.
type Package struct {
	Title      string // heading, e.g. "gox (root)"
	ImportPath string
	Dir        string
	Config     string // config struct name, "" for none
	Section    string // env section name ("POSTGRES"), "" for the base config
	DeclaredBy string // option that registers the section
	ConfigOnly bool   // document the config and skip the API
}

// Spec is what Generate needs.
type Spec struct {
	Fragments string // directory of *.md fragments; names < "50" go before the API, the rest after
	Packages  []Package
}

// Generate assembles llm.txt.
func Generate(spec Spec) ([]byte, error) {
	entries, err := os.ReadDir(spec.Fragments)
	if err != nil {
		return nil, fmt.Errorf("llmdoc: fragments: %w", err)
	}
	var before, after []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() < "50" {
			before = append(before, e.Name())
		} else {
			after = append(after, e.Name())
		}
	}
	sort.Strings(before)
	sort.Strings(after)

	var buf bytes.Buffer
	write := func(names []string) error {
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(spec.Fragments, n))
			if err != nil {
				return err
			}
			buf.Write(bytes.TrimRight(b, "\n"))
			buf.WriteString("\n\n")
		}
		return nil
	}
	if err := write(before); err != nil {
		return nil, err
	}
	for _, p := range spec.Packages {
		if !p.ConfigOnly {
			api, err := API(p.Dir, p.ImportPath)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&buf, "## API: %s\n\nimport \"%s\"\n\n```go\n%s```\n\n", p.Title, p.ImportPath, api)
		}
		if p.Config == "" {
			continue
		}
		prefix := "<PREFIX>"
		if p.Section != "" {
			prefix += "_" + p.Section
		}
		vars, err := EnvVars(p.Dir, p.Config, prefix)
		if err != nil {
			return nil, err
		}
		if len(vars) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "## Config: %s", p.Title)
		if p.DeclaredBy != "" {
			fmt.Fprintf(&buf, " (declared by %s)", p.DeclaredBy)
		}
		buf.WriteString("\n\n")
		for _, v := range vars {
			buf.WriteString(v.Line())
			buf.WriteByte('\n')
		}
		buf.WriteString("\n")
	}
	if err := write(after); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// API renders every exported declaration of the package in dir as one line
// each: the signature and the first sentence of its doc comment.
func API(dir, importPath string) (string, error) {
	fset, files, err := parseDir(dir)
	if err != nil {
		return "", err
	}
	d, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return "", fmt.Errorf("llmdoc: doc %s: %w", dir, err)
	}
	var sb strings.Builder

	for _, c := range d.Consts {
		writeValues(&sb, fset, "const", c)
	}
	for _, v := range d.Vars {
		writeValues(&sb, fset, "var", v)
	}
	for _, t := range d.Types {
		fmt.Fprintf(&sb, "type %s %s  // %s\n", t.Name, typeExpr(fset, t), first(t.Doc))
		for _, c := range t.Consts {
			writeValues(&sb, fset, "const", c)
		}
		for _, v := range t.Vars {
			writeValues(&sb, fset, "var", v)
		}
		for _, f := range t.Funcs {
			fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, f.Decl), first(f.Doc))
		}
		for _, m := range t.Methods {
			fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, m.Decl), first(m.Doc))
		}
	}
	for _, f := range d.Funcs {
		fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, f.Decl), first(f.Doc))
	}
	return sb.String(), nil
}

// parseDir parses the non-test Go files of one directory (one package).
func parseDir(dir string) (*token.FileSet, []*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("llmdoc: read %s: %w", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("llmdoc: parse %s: %w", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("llmdoc: no Go files in %s", dir)
	}
	return fset, files, nil
}

func writeValues(sb *strings.Builder, fset *token.FileSet, kind string, v *doc.Value) {
	// Specs that carry their own comment get their own line; otherwise the
	// group is one line with the group's comment.
	perSpec := false
	for _, spec := range v.Decl.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok && vs.Doc != nil && hasExported(vs) {
			perSpec = true
			break
		}
	}
	groupType := ""
	for _, spec := range v.Decl.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok && vs.Type != nil {
			groupType = exprString(fset, vs.Type)
			break
		}
	}
	if perSpec {
		for _, spec := range v.Decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || !hasExported(vs) {
				continue
			}
			typ := groupType
			if vs.Type != nil {
				typ = exprString(fset, vs.Type)
			}
			if typ == "" && kind == "var" {
				typ = inferVarType(fset, vs)
			}
			docText := first(v.Doc)
			if vs.Doc != nil {
				docText = first(vs.Doc.Text())
			}
			fmt.Fprintf(sb, "%s  // %s\n", valueLine(kind, exportedNames(vs), typ), docText)
		}
		return
	}
	var names []string
	for _, spec := range v.Decl.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			names = append(names, exportedNames(vs)...)
		}
	}
	if len(names) == 0 {
		return
	}
	typ := groupType
	if typ == "" && kind == "var" {
		if vs, ok := v.Decl.Specs[0].(*ast.ValueSpec); ok {
			typ = inferVarType(fset, vs)
		}
	}
	fmt.Fprintf(sb, "%s  // %s\n", valueLine(kind, names, typ), first(v.Doc))
}

func hasExported(vs *ast.ValueSpec) bool { return len(exportedNames(vs)) > 0 }

func exportedNames(vs *ast.ValueSpec) []string {
	var names []string
	for _, n := range vs.Names {
		if n.IsExported() {
			names = append(names, n.Name)
		}
	}
	return names
}

func valueLine(kind string, names []string, typ string) string {
	line := kind + " " + strings.Join(names, ", ")
	if typ != "" {
		line += " " + typ
	}
	return line
}

// inferVarType handles `var X = expr` where the type is implied; it only
// names the common error case.
func inferVarType(fset *token.FileSet, vs *ast.ValueSpec) string {
	if len(vs.Values) == 0 {
		return ""
	}
	if call, ok := vs.Values[0].(*ast.CallExpr); ok {
		if s := exprString(fset, call.Fun); strings.HasSuffix(s, "errors.New") {
			return "error"
		}
	}
	if id, ok := vs.Values[0].(*ast.Ident); ok && strings.HasPrefix(id.Name, "err") {
		return "error"
	}
	return ""
}

func typeExpr(fset *token.FileSet, t *doc.Type) string {
	for _, spec := range t.Decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != t.Name {
			continue
		}
		prefix := ""
		if ts.Assign.IsValid() {
			prefix = "= "
		}
		return prefix + typeString(fset, ts.Type)
	}
	return ""
}

// typeString renders a type; structs show their exported fields so a
// model knows what to read and set.
func typeString(fset *token.FileSet, expr ast.Expr) string {
	st, ok := expr.(*ast.StructType)
	if !ok {
		return exprString(fset, expr)
	}
	var fields []string
	for _, f := range st.Fields.List {
		typ := typeString(fset, f.Type)
		if len(f.Names) == 0 {
			fields = append(fields, typ)
			continue
		}
		for _, n := range f.Names {
			if n.IsExported() {
				fields = append(fields, n.Name+" "+typ)
			}
		}
	}
	if len(fields) == 0 {
		return "struct{ /* unexported */ }"
	}
	return "struct{ " + strings.Join(fields, "; ") + " }"
}

func signature(fset *token.FileSet, decl *ast.FuncDecl) string {
	clone := *decl
	clone.Body = nil
	clone.Doc = nil
	return exprString(fset, &clone)
}

func exprString(fset *token.FileSet, node any) string {
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces, Tabwidth: 4}
	if err := cfg.Fprint(&buf, fset, node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// first returns the first sentence of a doc comment.
func first(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	} else if i := strings.Index(text, ".\n"); i >= 0 {
		text = text[:i+1]
	}
	return strings.Join(strings.Fields(text), " ")
}

// EnvVar is one environment variable derived from a config struct.
type EnvVar struct {
	Name     string
	Type     string
	Default  string
	Required bool
	Mask     bool
	Help     string
}

// Line renders the variable for llm.txt.
func (v EnvVar) Line() string {
	var sb strings.Builder
	sb.WriteString(v.Name)
	sb.WriteString(" (" + v.Type)
	switch {
	case v.Required:
		sb.WriteString(", required")
	case v.Default != "":
		sb.WriteString(", default " + v.Default)
	}
	if v.Mask {
		sb.WriteString(", secret")
	}
	sb.WriteString(")")
	if v.Help != "" {
		sb.WriteString(": " + v.Help)
	}
	return sb.String()
}

// EnvVars walks the named struct in dir and returns the variables conf
// derives from it, prefixed with prefix.
func EnvVars(dir, structName, prefix string) ([]EnvVar, error) {
	fset, files, err := parseDir(dir)
	if err != nil {
		return nil, err
	}
	structs := map[string]*ast.StructType{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						structs[ts.Name.Name] = st
					}
				}
			}
		}
	}
	root, ok := structs[structName]
	if !ok {
		return nil, fmt.Errorf("llmdoc: struct %s not found in %s", structName, dir)
	}
	var out []EnvVar
	walk(fset, structs, root, prefix, nil, &out)
	return out, nil
}

func walk(fset *token.FileSet, structs map[string]*ast.StructType, st *ast.StructType, prefix string, path []string, out *[]EnvVar) {
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			// Embedded struct (gox.BaseConfig in a user struct): flatten.
			if id, ok := f.Type.(*ast.Ident); ok {
				if nested, ok := structs[id.Name]; ok {
					walk(fset, structs, nested, prefix, path, out)
				}
			}
			continue
		}
		name := f.Names[0]
		if !name.IsExported() {
			continue
		}
		tag := ""
		if f.Tag != nil {
			tag = reflect.StructTag(strings.Trim(f.Tag.Value, "`")).Get("conf")
		}
		if tag == "-" {
			continue
		}
		fieldPath := append(append([]string(nil), path...), name.Name)

		switch t := f.Type.(type) {
		case *ast.StructType:
			walk(fset, structs, t, prefix, fieldPath, out)
			continue
		case *ast.Ident:
			if nested, ok := structs[t.Name]; ok {
				walk(fset, structs, nested, prefix, fieldPath, out)
				continue
			}
		}

		v := EnvVar{Type: goType(exprString(fset, f.Type))}
		envKey := EnvName(fieldPath)
		for _, opt := range strings.Split(tag, ",") {
			key, val, _ := strings.Cut(opt, ":")
			switch key {
			case "env":
				envKey = val
			case "default":
				v.Default = val
			case "required":
				v.Required = true
			case "mask":
				v.Mask = true
			case "help":
				v.Help = val
			}
		}
		v.Name = prefix + "_" + envKey
		if prefix == "" {
			v.Name = envKey
		}
		*out = append(*out, v)
	}
}

func goType(t string) string {
	switch t {
	case "time.Duration":
		return "duration"
	}
	return t
}

// EnvName derives the env-var name conf produces from a field path:
// HTTPClient.Timeout -> HTTP_CLIENT_TIMEOUT, Otel.Enabled -> OTEL_ENABLED.
func EnvName(path []string) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = config.EnvKey(p)
	}
	return strings.Join(parts, "_")
}
