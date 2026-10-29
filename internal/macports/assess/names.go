package assess

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// projectRenamed is upstream's project calling itself another name in the
// new version than in the old: its Go module's last element, its Cargo
// package, or its Python distribution, each read from the root's own
// manifest. goplus's gop became xgo, its module with it. It holds nothing
// itself; it makes a repository's rename under the same owner a port's
// where the port carries the old name (the person's rule, 2026-10-29).
func (a *assessment) projectRenamed() (model.UpstreamChange, bool) {
	for _, manifest := range []struct {
		file string
		name func([]byte) (string, bool)
	}{{"go.mod", goModuleName}, {"Cargo.toml", cargoPackageName}, {"pyproject.toml", pythonDistributionName}} {
		was, ok := rootName(a.input.Pairs, before, manifest.file, manifest.name)
		if !ok {
			continue
		}
		now, ok := rootName(a.input.Pairs, after, manifest.file, manifest.name)
		if !ok || strings.EqualFold(now, was) {
			continue
		}
		return model.UpstreamChange{Kind: "name", Path: manifest.file, Rule: ProjectRenamed, Subject: now, Class: model.Introduced,
			Message: fmt.Sprintf("upstream renamed its project from %s to %s, as %s names it", was, now, manifest.file)}, true
	}
	return model.UpstreamChange{}, false
}

// rootName is the name a version's root manifest gives its project, the
// first archive's to hold one that reads.
func rootName(pairs []Pair, side func(Pair) project.Reading, file string, name func([]byte) (string, bool)) (string, bool) {
	for _, pair := range pairs {
		found, ok := side(pair).RootFile(file)
		if !ok || found.Truncated {
			continue
		}
		if named, ok := name(found.Data); ok {
			return named, true
		}
	}
	return "", false
}

// goModuleName is a module path's last element, its major version's
// suffix set aside: github.com/goplus/xgo/v2 is xgo.
func goModuleName(data []byte) (string, bool) {
	module, err := project.ReadGoModLax(data)
	if err != nil || module.Path == "" {
		return "", false
	}
	return path.Base(majorSuffix.ReplaceAllString(module.Path, "")), true
}

// majorSuffix is a Go module path's major version element, /v2 and on.
var majorSuffix = regexp.MustCompile(`/v[0-9]+$`)

func cargoPackageName(data []byte) (string, bool) {
	manifest, err := project.ReadCargoManifest(data)
	if err != nil || manifest.Package == nil || manifest.Package.Name == "" {
		return "", false
	}
	return manifest.Package.Name, true
}

// pythonDistributionName is [project]'s name as PEP 503 compares names.
func pythonDistributionName(data []byte) (string, bool) {
	manifest, err := project.ReadPyproject(data)
	if err != nil || manifest.Project == nil || manifest.Project.Name == "" {
		return "", false
	}
	return project.NormalizeName(manifest.Project.Name), true
}
