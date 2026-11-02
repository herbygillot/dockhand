package project

import (
	"slices"
	"strings"
)

// CargoFeatures are the features a cargo build turns on, and the packages
// it builds, as its arguments say them (The Cargo Book, "Features",
// "Command-line feature options"): the zero value is a build of each
// package with its default features, as the cargo PortGroup's is.
type CargoFeatures struct {
	// Features are --features' names, each as given: a package's own
	// feature, "pkg/feature" for a workspace member's, or "dep/feature"
	// for a dependency's.
	Features []string
	// All is --all-features; NoDefault is --no-default-features.
	All, NoDefault bool
	// Packages are -p and --package's names; none builds the workspace's
	// default members.
	Packages []string
}

// Enabled are the features of the package a build turns on, by name, and
// the dependencies they turn on, by the name the manifest declares them
// under: default unless the build turns it off, each feature the build
// names for this package, and through each, the features it names, an
// optional dependency of the name where no feature is, dep:x, and x/y
// (x?/y turns on x's feature only where x is on otherwise, so not x).
// --all-features turns every feature and optional dependency on.
func (m CargoManifest) Enabled(build CargoFeatures) (features, dependencies map[string]bool) {
	features, dependencies = map[string]bool{}, map[string]bool{}
	optional := map[string]bool{}
	for _, dependency := range m.Dependencies {
		if dependency.Optional {
			optional[dependency.Name] = true
		}
	}
	if build.All {
		for name := range m.Features {
			features[name] = true
		}
		for name := range optional {
			dependencies[name] = true
		}
		return features, dependencies
	}
	var turn func(entry string)
	turn = func(entry string) {
		if name, ok := strings.CutPrefix(entry, "dep:"); ok {
			dependencies[name] = true
			return
		}
		if dependency, _, ok := strings.Cut(entry, "/"); ok {
			if !strings.HasSuffix(dependency, "?") {
				dependencies[dependency] = true
				// An optional dependency's implicit feature is on with it.
				if _, declared := m.Features[dependency]; !declared && optional[dependency] {
					features[dependency] = true
				}
			}
			return
		}
		if features[entry] {
			return
		}
		if entries, ok := m.Features[entry]; ok {
			features[entry] = true
			for _, next := range entries {
				turn(next)
			}
			return
		}
		if optional[entry] {
			features[entry] = true
			dependencies[entry] = true
		}
	}
	if !build.NoDefault {
		turn("default")
	}
	name := ""
	if m.Package != nil {
		name = m.Package.Name
	}
	for _, requested := range build.Features {
		if pkg, feature, ok := strings.Cut(requested, "/"); ok && pkg == name {
			turn(feature)
			continue
		} else if ok && !slices.ContainsFunc(m.Dependencies, func(d CargoDependency) bool { return d.Name == pkg }) {
			// Another member's feature.
			continue
		}
		turn(requested)
	}
	return features, dependencies
}
