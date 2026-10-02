package project

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/semver"
)

// CargoManifest is what a Cargo.toml declares: its package, the binaries
// it names, its dependencies, and the workspace it's the root of, read by
// the Cargo Book's rules.
type CargoManifest struct {
	// Package is nil where the file has none, as a workspace's root may
	// not. A field it inherits from the workspace, as license.workspace =
	// true, is the workspace's where this file is the root, which is the
	// only place [workspace.package] can be; a member's is resolved by
	// Inherit.
	Package *CargoPackageInfo
	// Workspace is the workspace this file is the root of; nil where it
	// isn't one.
	Workspace *CargoWorkspace
	// Bins are its [[bin]] targets' names.
	Bins []string
	// Dependencies are in the order a name declared in several places is
	// read: the package's own, then for building, then for tests, then
	// each target's in that order, then the workspace's.
	Dependencies []CargoDependency
}

// CargoPackageInfo is a Cargo.toml's [package], or a workspace's
// [workspace.package].
type CargoPackageInfo struct {
	Name, License, Description string
	// Inherited are the fields it takes from its workspace, by name, which
	// stay empty until they're resolved.
	Inherited []string
}

// CargoWorkspace is a workspace's root: its members, as globs of their
// directories, those excluded, as paths, and the package fields and
// dependencies its members may inherit (The Cargo Book, "Workspaces").
type CargoWorkspace struct {
	Members, Exclude []string
	Package          CargoPackageInfo
}

// Member reports whether a directory below the workspace's root is a
// member: matched by a members glob, and not inside an excluded path. A
// path dependency inside the workspace is a member too, which a manifest
// alone doesn't say.
func (w CargoWorkspace) Member(directory string) bool {
	directory = strings.Trim(path.Clean("/"+directory), "/")
	for _, excluded := range w.Exclude {
		excluded = strings.Trim(path.Clean("/"+excluded), "/")
		if directory == excluded || strings.HasPrefix(directory, excluded+"/") {
			return false
		}
	}
	for _, pattern := range w.Members {
		pattern = strings.Trim(path.Clean("/"+pattern), "/")
		if pattern != "" && glob(strings.Split(pattern, "/"), strings.Split(directory, "/")) {
			return true
		}
	}
	return false
}

// Inherit resolves what a member's manifest takes from its workspace's
// root: each package field it inherits, and each dependency declared with
// workspace = true, which takes the root's [workspace.dependencies] entry
// of its name, its own optional mark kept. uv's crates declare license =
// { workspace = true }, which reads as the workspace's license rather
// than as none.
func (m CargoManifest) Inherit(root CargoManifest) CargoManifest {
	if root.Workspace == nil {
		return m
	}
	if m.Package != nil {
		resolved := *m.Package
		resolved.inherit(root.Workspace.Package)
		m.Package = &resolved
	}
	m.Dependencies = slices.Clone(m.Dependencies)
	for i, dependency := range m.Dependencies {
		if !dependency.Workspace || strings.HasPrefix(dependency.Table, "workspace.") {
			continue
		}
		j := slices.IndexFunc(root.Dependencies, func(d CargoDependency) bool { return d.Table == "workspace.dependencies" && d.Name == dependency.Name })
		if j < 0 {
			continue
		}
		inherited := root.Dependencies[j]
		inherited.Name, inherited.Table, inherited.Workspace = dependency.Name, dependency.Table, true
		inherited.Optional = inherited.Optional || dependency.Optional
		m.Dependencies[i] = inherited
	}
	return m
}

// inherit fills the fields a package inherits from its workspace's.
func (p *CargoPackageInfo) inherit(workspace CargoPackageInfo) {
	for _, field := range p.Inherited {
		switch field {
		case "license":
			p.License = workspace.License
		case "description":
			p.Description = workspace.Description
		}
	}
}

// CargoDependency is one dependency a Cargo.toml declares, in one table:
// its version requirement, its Git source and what pins it there, or its
// path, or that it's the workspace's.
type CargoDependency struct {
	Name string
	// Table is where it's declared: "dependencies", "build-dependencies",
	// or "dev-dependencies", "target.<cfg>." before one of those, or
	// "workspace.dependencies".
	Table   string
	Version string
	Git     string
	// Pin is how a Git source is pinned, "rev", "tag", or "branch", and At
	// what it's pinned to.
	Pin, At   string
	Path      string
	Workspace bool
	// Optional is a dependency a feature turns on.
	Optional bool
	// Package is the crate a dependency renamed with package = is, where
	// Name is what the project calls it.
	Package string
}

// OnMacOS says whether a dependency applies to a macOS build: one of a
// target table, by its key (CargoTargetOnMacOS); any other always does.
func (d CargoDependency) OnMacOS() (Applies, error) {
	key, ok := strings.CutPrefix(d.Table, "target.")
	if !ok {
		return Yes, nil
	}
	for _, table := range cargoTables {
		if trimmed, ok := strings.CutSuffix(key, "."+table); ok {
			return CargoTargetOnMacOS(trimmed)
		}
	}
	return Unknown, fmt.Errorf("%s isn't a target's dependency table", d.Table)
}

// Crate is the crate a dependency is: its name, or what it renames.
func (d CargoDependency) Crate() string {
	if d.Package != "" {
		return d.Package
	}
	return d.Name
}

// cargoTables are where a Cargo.toml declares dependencies, in the order a
// name declared in several is read.
var cargoTables = []string{"dependencies", "build-dependencies", "dev-dependencies"}

// ReadCargoManifest reads a Cargo.toml. A dependency table that isn't one,
// or a dependency that names none of a version, a Git source, a path, or
// the workspace, fails the reading.
func ReadCargoManifest(data []byte) (CargoManifest, error) {
	var manifest map[string]any
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return CargoManifest{}, err
	}
	var found CargoManifest
	if pkg, ok := manifest["package"].(map[string]any); ok {
		found.Package = &CargoPackageInfo{}
		found.Package.Name, _ = pkg["name"].(string)
		for field, value := range map[string]*string{"license": &found.Package.License, "description": &found.Package.Description} {
			switch given := pkg[field].(type) {
			case string:
				*value = given
			case map[string]any:
				if given["workspace"] == true {
					found.Package.Inherited = append(found.Package.Inherited, field)
				}
			}
		}
		slices.Sort(found.Package.Inherited)
	}
	if workspace, ok := manifest["workspace"].(map[string]any); ok {
		found.Workspace = &CargoWorkspace{Members: cargoStrings(workspace["members"]), Exclude: cargoStrings(workspace["exclude"])}
		if pkg, ok := workspace["package"].(map[string]any); ok {
			found.Workspace.Package.License, _ = pkg["license"].(string)
			found.Workspace.Package.Description, _ = pkg["description"].(string)
		}
		if found.Package != nil {
			found.Package.inherit(found.Workspace.Package)
		}
	}
	if bins, ok := manifest["bin"].([]map[string]any); ok {
		for _, bin := range bins {
			if name, ok := bin["name"].(string); ok && name != "" {
				found.Bins = append(found.Bins, name)
			}
		}
	}
	read := func(prefix string, tables map[string]any, names []string) error {
		for _, table := range names {
			entries, ok := tables[table].(map[string]any)
			if tables[table] != nil && !ok {
				return fmt.Errorf("[%s%s] isn't a table", prefix, table)
			}
			for _, name := range slices.Sorted(maps.Keys(entries)) {
				dependency, err := cargoDependency(entries[name])
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				dependency.Name, dependency.Table = name, prefix+table
				found.Dependencies = append(found.Dependencies, dependency)
			}
		}
		return nil
	}
	if err := read("", manifest, cargoTables); err != nil {
		return CargoManifest{}, err
	}
	if targets, ok := manifest["target"].(map[string]any); ok {
		for _, target := range slices.Sorted(maps.Keys(targets)) {
			tables, ok := targets[target].(map[string]any)
			if !ok {
				return CargoManifest{}, fmt.Errorf("[target.%s] isn't a table", target)
			}
			if err := read("target."+target+".", tables, cargoTables); err != nil {
				return CargoManifest{}, err
			}
		}
	}
	if workspace, ok := manifest["workspace"].(map[string]any); ok {
		if err := read("workspace.", workspace, []string{"dependencies"}); err != nil {
			return CargoManifest{}, err
		}
		// The root's own package inherits from its own workspace: cargo's
		// [dependencies] serde = { workspace = true } is the version its
		// [workspace.dependencies] gives, which reading it as "workspace",
		// first, kept that version moving from being seen.
		found = found.Inherit(found)
	}
	return found, nil
}

// cargoDependency reads a dependency as Cargo.toml gives it: a version, or
// a table.
func cargoDependency(value any) (CargoDependency, error) {
	switch value := value.(type) {
	case string:
		return CargoDependency{Version: value}, nil
	case map[string]any:
		var found CargoDependency
		found.Version, _ = value["version"].(string)
		found.Git, _ = value["git"].(string)
		if found.Git != "" {
			for _, pin := range []string{"rev", "tag", "branch"} {
				if at, ok := value[pin].(string); ok {
					found.Pin, found.At = pin, at
					break
				}
			}
		}
		found.Path, _ = value["path"].(string)
		found.Package, _ = value["package"].(string)
		found.Workspace = value["workspace"] == true
		found.Optional = value["optional"] == true
		if found.Version == "" && found.Git == "" && found.Path == "" && !found.Workspace {
			return CargoDependency{}, errors.New("neither a version, a Git source, a path, nor the workspace's")
		}
		return found, nil
	}
	return CargoDependency{}, fmt.Errorf("a %T isn't a requirement", value)
}

// cargoStrings is a TOML array of strings; nil for anything else.
func cargoStrings(value any) []string {
	items, _ := value.([]any)
	var found []string
	for _, item := range items {
		if text, ok := item.(string); ok {
			found = append(found, text)
		}
	}
	return found
}

// CrateSource is where a package a Cargo.lock pins comes from.
type CrateSource string

const (
	// FromLocal is the project's own package, or a path dependency: the
	// lock names no source.
	FromLocal CrateSource = "local"
	// FromCratesIO is crates.io's index, in either of its protocols.
	FromCratesIO CrateSource = "crates.io"
	// FromRegistry is another registry.
	FromRegistry CrateSource = "registry"
	// FromGit is a Git repository.
	FromGit CrateSource = "git"
)

// CargoPackage is one package a Cargo.lock pins. Origin is its source as
// the lock writes it, such as
// registry+https://github.com/rust-lang/crates.io-index; empty for a local
// one.
type CargoPackage struct {
	Name, Version string
	Source        CrateSource
	Origin        string
	Checksum      string
}

// NativeLibrary is the native library a package links, by Cargo's
// convention for naming such packages: foo-sys links foo ("The *-sys
// Packages", The Cargo Book). Such a package often finds the library
// installed and links it, and builds a copy it bundles otherwise. Empty
// for a package not named so. crates.io treats - and _ in names alike.
func (p CargoPackage) NativeLibrary() string {
	for _, suffix := range []string{"-sys", "_sys"} {
		if library, ok := strings.CutSuffix(p.Name, suffix); ok {
			return library
		}
	}
	return ""
}

// cratesIO is crates.io's index as a lock names it, in each protocol.
var cratesIO = []string{"registry+https://github.com/rust-lang/crates.io-index", "sparse+https://index.crates.io/"}

// ReadCargoLock reads the packages a Cargo.lock pins, each with where it
// comes from, for creating a port and for updating one, which choose what
// they can declare, and for comparing two versions. It refuses a lock
// format it doesn't know, a package whose name or version isn't Cargo's,
// a crates.io crate without a valid checksum or pinned twice, and a source
// it can't place.
func ReadCargoLock(data []byte) ([]CargoPackage, error) {
	var lock struct {
		Version int
		Package []struct{ Name, Version, Source, Checksum string }
	}
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, fmt.Errorf("project: Cargo.lock: %w", err)
	}
	if lock.Version < 1 || lock.Version > 4 || len(lock.Package) == 0 {
		return nil, fmt.Errorf("project: unsupported or empty Cargo.lock")
	}
	var packages []CargoPackage
	pinned := map[string]bool{}
	for _, pkg := range lock.Package {
		if !CrateName(pkg.Name) || !semver.IsValid("v"+pkg.Version) {
			return nil, fmt.Errorf("project: invalid Cargo.lock package")
		}
		found := CargoPackage{Name: pkg.Name, Version: pkg.Version, Origin: pkg.Source, Checksum: pkg.Checksum}
		switch {
		case pkg.Source == "":
			found.Source = FromLocal
		case slices.Contains(cratesIO, pkg.Source):
			found.Source = FromCratesIO
			if sum, err := hex.DecodeString(pkg.Checksum); err != nil || len(sum) != 32 {
				return nil, fmt.Errorf("project: registry crate %s has no valid checksum", pkg.Name)
			}
			key := pkg.Name + " " + pkg.Version
			if pinned[key] {
				return nil, fmt.Errorf("project: duplicate registry crate %s", key)
			}
			pinned[key] = true
		case strings.HasPrefix(pkg.Source, "registry+"), strings.HasPrefix(pkg.Source, "sparse+"):
			found.Source = FromRegistry
		case strings.HasPrefix(pkg.Source, "git+"):
			found.Source = FromGit
		default:
			return nil, fmt.Errorf("project: crate %s comes from a source Cargo.lock doesn't define: %s", pkg.Name, pkg.Source)
		}
		packages = append(packages, found)
	}
	return packages, nil
}

// CrateName reports a name Cargo allows a package: ASCII letters, digits,
// "_", and "-".
func CrateName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
