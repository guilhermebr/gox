// Package llmdoc generates llm.txt, the single-file export of gox's docs for
// tools without a shell. Static fragments (purpose, canonical mains, layout,
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
	"path"
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
	Config     string   // config struct name, "" for none
	Section    string   // env section name ("POSTGRES"), "" for the base config
	DeclaredBy string   // option that registers the section
	ConfigOnly bool     // document the config and skip the API
	Only       []string // when set, only these declarations (path.Match patterns; a method is "Type.Method")
	Exclude    []string // declarations to leave out, as in Only
}

// keep reports whether the declaration name (a method is "Type.Method")
// goes into llm.txt. The config type's Validate is left out: config
// loading runs it and the ## Config section documents the type.
func (p Package) keep(name string) bool {
	if p.Config != "" && name == p.Config+".Validate" {
		return false
	}
	if len(p.Only) > 0 && !matches(p.Only, name) {
		return false
	}
	return !matches(p.Exclude, name)
}

func matches(patterns []string, name string) bool {
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
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
			api, err := API(p)
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

// API renders the exported declarations of p that p.keep allows as one
// line each: the signature and the first sentence of its doc comment (of
// its Deprecated: paragraph when it has one).
func API(p Package) (string, error) {
	fset, files, err := parseDir(p.Dir)
	if err != nil {
		return "", err
	}
	d, err := doc.NewFromFiles(fset, files, p.ImportPath)
	if err != nil {
		return "", fmt.Errorf("llmdoc: doc %s: %w", p.Dir, err)
	}
	byName := map[string]*ast.File{}
	for _, f := range files {
		byName[fset.File(f.Pos()).Name()] = f
	}
	var sb strings.Builder

	for _, c := range d.Consts {
		writeValues(&sb, fset, "const", c, p.keep)
	}
	for _, v := range d.Vars {
		writeValues(&sb, fset, "var", v, p.keep)
	}
	for _, t := range d.Types {
		if p.keep(t.Name) {
			fmt.Fprintf(&sb, "type %s %s  // %s\n", t.Name, typeExpr(fset, t, p, byName), summary(t.Doc))
		}
		for _, c := range t.Consts {
			writeValues(&sb, fset, "const", c, p.keep)
		}
		for _, v := range t.Vars {
			writeValues(&sb, fset, "var", v, p.keep)
		}
		for _, f := range t.Funcs {
			if p.keep(f.Name) {
				fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, f.Decl), summary(f.Doc))
			}
		}
		for _, m := range t.Methods {
			if p.keep(t.Name + "." + m.Name) {
				fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, m.Decl), summary(m.Doc))
			}
		}
	}
	for _, f := range d.Funcs {
		if p.keep(f.Name) {
			fmt.Fprintf(&sb, "%s  // %s\n", signature(fset, f.Decl), summary(f.Doc))
		}
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

func writeValues(sb *strings.Builder, fset *token.FileSet, kind string, v *doc.Value, keep func(string) bool) {
	exportedNames := func(vs *ast.ValueSpec) []string {
		var names []string
		for _, n := range vs.Names {
			if n.IsExported() && keep(n.Name) {
				names = append(names, n.Name)
			}
		}
		return names
	}
	hasExported := func(vs *ast.ValueSpec) bool { return len(exportedNames(vs)) > 0 }
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
			docText := summary(v.Doc)
			if vs.Doc != nil {
				docText = summary(vs.Doc.Text())
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
	fmt.Fprintf(sb, "%s  // %s\n", valueLine(kind, names, typ), summary(v.Doc))
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

func typeExpr(fset *token.FileSet, t *doc.Type, p Package, files map[string]*ast.File) string {
	for _, spec := range t.Decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != t.Name {
			continue
		}
		if !ts.Assign.IsValid() {
			return typeString(fset, ts.Type)
		}
		if target, ok := aliasTarget(ts, p, files[fset.File(ts.Pos()).Name()]); ok {
			return target
		}
		return "= " + typeString(fset, ts.Type)
	}
	return ""
}

// aliasTarget renders what an alias names when the target lives in a
// package under p (the root's pkg/*), whose docs llm.txt does not carry:
// a struct as its fields, so the alias reads like the struct it is, and a
// func or interface type as "= <type>". Aliases of named basic types
// (errors.Code) and of other modules stay as they are.
func aliasTarget(ts *ast.TypeSpec, p Package, file *ast.File) (string, bool) {
	sel, ok := ts.Type.(*ast.SelectorExpr)
	if !ok || file == nil {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	rel, ok := strings.CutPrefix(importPath(file, pkg.Name), p.ImportPath+"/")
	if !ok {
		return "", false
	}
	fset, files, err := parseDir(filepath.Join(p.Dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				target, ok := spec.(*ast.TypeSpec)
				if !ok || target.Name.Name != sel.Sel.Name {
					continue
				}
				qualify(target.Type, pkg.Name)
				switch target.Type.(type) {
				case *ast.StructType:
					return typeString(fset, target.Type), true
				case *ast.FuncType, *ast.InterfaceType:
					return "= " + typeString(fset, target.Type), true
				}
				return "", false
			}
		}
	}
	return "", false
}

// importPath returns the path file imports under name, "" if none.
func importPath(file *ast.File, name string) string {
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		n := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			n = imp.Name.Name
		}
		if n == name {
			return p
		}
	}
	return ""
}

// qualify prefixes the exported type names in expr, which come from
// package pkg, with "pkg." so they read right outside it. Field, parameter
// and method names are left alone.
func qualify(expr ast.Expr, pkg string) {
	names := map[*ast.Ident]bool{}
	ast.Inspect(expr, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			return false
		case *ast.Field:
			for _, id := range n.Names {
				names[id] = true
			}
		case *ast.Ident:
			if !names[n] && n.IsExported() {
				n.Name = pkg + "." + n.Name
			}
		}
		return true
	})
}

// typeString renders a type; structs show their exported fields so a
// model knows what to read and set, and interfaces their methods without
// the comments, which would turn the rest of the line into a comment.
func typeString(fset *token.FileSet, expr ast.Expr) string {
	if it, ok := expr.(*ast.InterfaceType); ok {
		var methods []string
		for _, f := range it.Methods.List {
			ft, ok := f.Type.(*ast.FuncType)
			if !ok || len(f.Names) == 0 {
				methods = append(methods, exprString(fset, f.Type))
				continue
			}
			for _, n := range f.Names {
				methods = append(methods, n.Name+strings.TrimPrefix(exprString(fset, ft), "func"))
			}
		}
		if len(methods) == 0 {
			return "interface{}"
		}
		return "interface{ " + strings.Join(methods, "; ") + " }"
	}
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

// summary is what llm.txt shows for a declaration: the first sentence of
// its doc comment, or of its Deprecated: paragraph when it has one, so a
// deprecated name never reads as a way to do something.
func summary(text string) string {
	for _, para := range strings.Split(text, "\n\n") {
		if para = strings.TrimSpace(para); strings.HasPrefix(para, "Deprecated:") {
			return first(para)
		}
	}
	return first(text)
}

// first returns the first sentence of a doc comment: up to the first
// period that ends a line or is followed by a space.
func first(text string) string {
	text = strings.TrimSpace(text)
	end := len(text)
	for _, sep := range []string{". ", ".\n"} {
		if i := strings.Index(text, sep); i >= 0 && i+1 < end {
			end = i + 1
		}
	}
	return strings.Join(strings.Fields(text[:end]), " ")
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
