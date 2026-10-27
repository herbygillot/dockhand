package assess

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// Go's linker ignores -X for a symbol that isn't there, so a port's version
// string goes silently wrong where upstream moves or drops the variable:
// termshot, dblab, atlas, and mage built, linked, and passed lint
// reporting the wrong version, and mp4ff moved its version variable to
// another package (the docs dig's I18). Nothing is run: the package is
// parsed (go/parser).

// goLinkTargets are the new version's -X targets its package doesn't
// declare, each held, and those whose package can't be found or read,
// coverage, said and holding nothing.
func (a *assessment) goLinkTargets() {
	targets := a.input.Port.GoLinkTargets()
	if len(targets) == 0 {
		return
	}
	for _, pair := range a.input.Pairs {
		reading := pair.After
		module, ok := reading.Files[path.Join(reading.Root, "go.mod")]
		if !ok || module.Truncated {
			continue
		}
		parsed, err := project.ReadGoModLax(module.Data)
		if err != nil || parsed.Path == "" {
			continue
		}
		for _, target := range targets {
			dir, inModule := packageDir(parsed.Path, target.Package)
			if !inModule {
				continue
			}
			dir = path.Join(reading.Root, dir)
			declared, problem := declaresVariable(reading, dir, target)
			switch {
			case problem != "":
				a.cover(model.Coverage{Path: dir, System: "go", Relevance: "used", Treatment: "set-apart", Policy: GoLinkTargetRule, Reason: problem})
			case !declared:
				a.add(model.UpstreamChange{Kind: "go-ldflags", Path: dir, Rule: GoLinkTargetRule, Subject: target.Package + "." + target.Name, Class: model.Introduced, Hold: true,
					Message: fmt.Sprintf("upstream: package %s declares no variable %s, so the build's %s sets nothing, and the version it reports may be wrong", target.Package, target.Name, target.Argument)})
			}
		}
		return
	}
}

// packageDir is a package's directory below its module's, false where the
// import path isn't the module's; package main is the module's own.
func packageDir(module, imported string) (string, bool) {
	switch {
	case imported == "main" || imported == module:
		return "", true
	case strings.HasPrefix(imported, module+"/"):
		return strings.TrimPrefix(imported, module+"/"), true
	}
	return "", false
}

// declaresVariable reports whether the Go files of a directory, the
// package, declare a package-level variable of the target's name, package
// main's being the module's own main files; problem says why that couldn't
// be read: no file there, one cut short, or one that doesn't parse.
func declaresVariable(reading project.Reading, dir string, target macports.GoLinkTarget) (bool, string) {
	files := token.NewFileSet()
	read := 0
	for name, file := range reading.Files {
		if path.Dir(name) != dir || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if file.Truncated {
			return false, name + " is larger than the comparison reads"
		}
		parsed, err := parser.ParseFile(files, name, file.Data, parser.SkipObjectResolution)
		if err != nil {
			return false, fmt.Sprintf("%s couldn't be parsed: %v", name, err)
		}
		if target.Package == "main" && parsed.Name.Name != "main" {
			continue
		}
		read++
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if name.Name == target.Name {
						return true, ""
					}
				}
			}
		}
	}
	if read == 0 {
		return false, fmt.Sprintf("the package %s for %s wasn't found in the archive", target.Package, target.Argument)
	}
	return false, ""
}
