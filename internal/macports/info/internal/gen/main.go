// Command gen writes info's comparison table from the declaration of
// info.Semantic.
//
// It exists because Go gives no exhaustiveness over struct fields. The
// comparison used to be a hand-kept slice beside the struct, so a field
// added without a row was invisible to every diff and nothing said so —
// not the compiler, not a test, only a reviewer who happened to know
// the table was there (D8). Generating the table moves that from
// vigilance to arithmetic: the Field constants, their MacPorts names,
// and the accessors all come from one place, and the generated file
// ends in unkeyed composite literals that cannot compile if a field was
// added since it last ran.
//
// IT READS SOURCE, NOT TYPES, and that is deliberate. A generator that
// imported info and reflected over it would stop building at exactly
// the moment it is needed — when the generated file in the package is
// stale and the package does not compile. Parsing the declarations with
// go/ast keeps the generator runnable no matter what state the package
// is in, which is what makes "regenerate and the build comes back" true
// rather than a race.
//
// Usage, from the info package directory (this is what the //go:generate
// directive on info.Semantic runs):
//
//	go run ./internal/gen [-o semantic_compare_gen.go]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// generatedName is the file this writes, and the one it refuses to read
// its own input from.
const generatedName = "semantic_compare_gen.go"

// rootType is the struct the whole table is derived from. Naming it as a
// constant rather than a flag is the point: there is one prediction
// contract, and a second one would be a design decision and not an
// invocation.
const rootType = "Semantic"

// tagKey is the struct tag that carries a field's MacPorts option name.
// A field without it is an error rather than a guess: the name is what
// a plan's wire form records and what change.fieldNamed inverts, so
// inventing one from the Go identifier would put a vocabulary nobody
// declared into a durable record (rule 6).
const tagKey = "field"

func main() {
	out := flag.String("o", generatedName, "file to write")
	dir := flag.String("dir", ".", "directory holding the package source")
	flag.Parse()

	src, err := generate(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

// leaf is one comparable field: the Field constant to declare for it,
// the MacPorts name it answers to, the Go expression that reads it off a
// Semantic, and whether it is a scalar (which the uniform []string
// representation lifts through scalar()).
type leaf struct {
	Const  string
	Name   string
	Access string
	Scalar bool
}

// structDecl is one struct type declaration as parsed: its fields in
// source order, each with its Go name, its type expression as written,
// and its tag.
type structDecl struct {
	fields []structField
}

type structField struct {
	name string
	typ  string
	tag  string
}

// generate parses the package in dir and returns the formatted source of
// the comparison table.
func generate(dir string) ([]byte, error) {
	types, err := parseStructs(dir)
	if err != nil {
		return nil, err
	}
	root, ok := types[rootType]
	if !ok {
		return nil, fmt.Errorf("no type %s in %s", rootType, dir)
	}

	var leaves []leaf
	// groups is the group struct types encountered, in the order
	// Semantic names them, so the pins below are emitted in a stable
	// order rather than a map's.
	var groups []string
	for _, f := range root.fields {
		name, err := optionName(rootType, f)
		if err != nil {
			return nil, err
		}
		switch f.typ {
		case "string":
			leaves = append(leaves, leaf{Const: "Field" + f.name, Name: name,
				Access: "v." + f.name, Scalar: true})
		case "[]string":
			leaves = append(leaves, leaf{Const: "Field" + f.name, Name: name,
				Access: "v." + f.name})
		default:
			group, ok := types[f.typ]
			if !ok {
				return nil, fmt.Errorf("%s.%s: type %s is neither string, []string, "+
					"nor a struct declared in this package; teach the generator what it means "+
					"before adding it to %s", rootType, f.name, f.typ, rootType)
			}
			groups = append(groups, f.typ)
			for _, g := range group.fields {
				sub, err := optionName(f.typ, g)
				if err != nil {
					return nil, err
				}
				if g.typ != "[]string" {
					return nil, fmt.Errorf("%s.%s: a group's fields must be []string, got %s",
						f.typ, g.name, g.typ)
				}
				leaves = append(leaves, leaf{
					Const:  "Field" + f.name + g.name,
					Name:   name + "_" + sub,
					Access: "v." + f.name + "." + g.name,
				})
			}
		}
	}
	return render(types, root, groups, leaves)
}

// optionName reads a field's MacPorts name out of its tag.
func optionName(owner string, f structField) (string, error) {
	if f.tag == "" {
		return "", fmt.Errorf("%s.%s has no `%s` tag; every compared field must declare "+
			"the MacPorts option name it answers to", owner, f.name, tagKey)
	}
	unquoted, err := strconv.Unquote(f.tag)
	if err != nil {
		return "", fmt.Errorf("%s.%s: malformed tag: %w", owner, f.name, err)
	}
	name, ok := reflect.StructTag(unquoted).Lookup(tagKey)
	if !ok || name == "" {
		return "", fmt.Errorf("%s.%s has no `%s` tag; every compared field must declare "+
			"the MacPorts option name it answers to", owner, f.name, tagKey)
	}
	return name, nil
}

// parseStructs reads every non-test, non-generated Go file in dir and
// returns the struct type declarations it finds, keyed by name.
func parseStructs(dir string) (map[string]structDecl, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]structDecl{}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") ||
			strings.HasSuffix(n, "_test.go") || n == generatedName {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				out[ts.Name.Name] = declOf(st)
			}
		}
	}
	return out, nil
}

// declOf flattens a parsed struct into names, type expressions and tags.
// A grouped declaration (`A, B string`) yields one entry per name, which
// is what the source means.
func declOf(st *ast.StructType) structDecl {
	var d structDecl
	for _, f := range st.Fields.List {
		typ := types(f.Type)
		tag := ""
		if f.Tag != nil {
			tag = f.Tag.Value
		}
		for _, n := range f.Names {
			d.fields = append(d.fields, structField{name: n.Name, typ: typ, tag: tag})
		}
	}
	return d
}

// types renders a type expression the way the source wrote it. Only the
// shapes this generator understands need to round-trip; anything else
// renders to something that will not match a known case and is reported
// by name.
func types(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + types(t.Elt)
		}
	case *ast.StarExpr:
		return "*" + types(t.X)
	case *ast.SelectorExpr:
		return types(t.X) + "." + t.Sel.Name
	}
	return fmt.Sprintf("%T", e)
}

// zeroOf is the zero value literal for a field type, used by the unkeyed
// pins. A group renders as its own empty keyed literal, and the group
// gets a pin of its own so nothing hides inside it.
func zeroOf(typ string) string {
	switch typ {
	case "string":
		return `""`
	case "[]string":
		return "nil"
	default:
		return typ + "{}"
	}
}

func render(types map[string]structDecl, root structDecl, groups []string, leaves []leaf) ([]byte, error) {
	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("// Code generated by internal/macports/info/internal/gen. DO NOT EDIT.\n\n")
	p("package info\n\n")

	p("// Field identifies one field of Semantic. The set is closed, and\n")
	p("// String speaks MacPorts' own option names — these are the canonical\n")
	p("// field identifiers shared by everything that names port metadata,\n")
	p("// and the vocabulary a plan's predicted delta is recorded in.\n")
	p("//\n")
	p("// The order is Semantic's own declaration order, which makes it the\n")
	p("// canonical order of a delta's field changes: two equal deltas\n")
	p("// compare equal deterministically because the table is walked in it.\n")
	p("type Field int\n\n")

	p("const (\n")
	for i, l := range leaves {
		if i == 0 {
			p("\t%s Field = iota\n", l.Const)
			continue
		}
		p("\t%s\n", l.Const)
	}
	p(")\n\n")

	p("// String is the MacPorts option name this field answers to.\n")
	p("func (f Field) String() string {\n\tswitch f {\n")
	for _, l := range leaves {
		p("\tcase %s:\n\t\treturn %q\n", l.Const, l.Name)
	}
	p("\t}\n\treturn \"unknown field\"\n}\n\n")

	p("// Fields is every Field, in canonical order. It is what a caller\n")
	p("// iterating the closed set walks, so that adding a field extends\n")
	p("// the walk instead of needing the walk's bounds edited — and so\n")
	p("// that a caller cannot silently miss a field appended after\n")
	p("// whichever constant it had hard-coded as the end.\n")
	p("func Fields() []Field {\n\treturn []Field{\n")
	for _, l := range leaves {
		p("\t\t%s,\n", l.Const)
	}
	p("\t}\n}\n\n")

	p("// semanticTable is the single source of field extraction: Diff,\n")
	p("// Values equality, and any field-addressed access all read a\n")
	p("// Semantic through it. It is generated from the struct, so a field\n")
	p("// that is not here is a field that is not in Semantic.\n")
	p("var semanticTable = []struct {\n\tfield Field\n\tget   func(Semantic) []string\n}{\n")
	for _, l := range leaves {
		if l.Scalar {
			p("\t{%s, func(v Semantic) []string { return scalar(%s) }},\n", l.Const, l.Access)
			continue
		}
		p("\t{%s, func(v Semantic) []string { return %s }},\n", l.Const, l.Access)
	}
	p("}\n\n")

	p("// The pins below are the enforcement, and they are the reason this\n")
	p("// file is generated at all. An UNKEYED composite literal must give a\n")
	p("// value for every field of its struct, so a field added to one of\n")
	p("// these types stops the package from compiling until the generator\n")
	p("// has run again and given that field a Field constant and a row in\n")
	p("// semanticTable. Go offers no exhaustiveness over struct fields;\n")
	p("// this is the closest thing to it, and it is a build failure rather\n")
	p("// than a review catch or a runtime surprise (D8).\n")
	p("//\n")
	p("// If one of these is failing to compile: run\n")
	p("// `go generate ./internal/macports/info` and commit the result.\n")
	pinFor(p, rootType, root)
	for _, g := range groups {
		pinFor(p, g, types[g])
	}
	return format.Source(b.Bytes())
}

func pinFor(p func(string, ...any), name string, d structDecl) {
	p("var _ = %s{", name)
	for i, f := range d.fields {
		if i > 0 {
			p(", ")
		}
		p("%s", zeroOf(f.typ))
	}
	p("}\n")
}
