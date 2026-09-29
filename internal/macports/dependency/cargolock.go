package dependency

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/semver"
)

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
// they can declare. It refuses a lock format it doesn't know, a package
// whose name or version isn't Cargo's, a crates.io crate without a valid
// checksum or pinned twice, and a source it can't place.
func ReadCargoLock(data []byte) ([]CargoPackage, error) {
	var lock struct {
		Version int
		Package []struct{ Name, Version, Source, Checksum string }
	}
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, fmt.Errorf("dependency: Cargo.lock: %w", err)
	}
	if lock.Version < 1 || lock.Version > 4 || len(lock.Package) == 0 {
		return nil, fmt.Errorf("dependency: unsupported or empty Cargo.lock")
	}
	var packages []CargoPackage
	pinned := map[string]bool{}
	for _, pkg := range lock.Package {
		if !crateName(pkg.Name) || !semver.IsValid("v"+pkg.Version) {
			return nil, fmt.Errorf("dependency: invalid Cargo.lock package")
		}
		found := CargoPackage{Name: pkg.Name, Version: pkg.Version, Origin: pkg.Source, Checksum: pkg.Checksum}
		switch {
		case pkg.Source == "":
			found.Source = FromLocal
		case slices.Contains(cratesIO, pkg.Source):
			found.Source = FromCratesIO
			if !sha256Value(pkg.Checksum) {
				return nil, fmt.Errorf("dependency: registry crate %s has no valid checksum", pkg.Name)
			}
			key := pkg.Name + " " + pkg.Version
			if pinned[key] {
				return nil, fmt.Errorf("dependency: duplicate registry crate %s", key)
			}
			pinned[key] = true
		case strings.HasPrefix(pkg.Source, "registry+"), strings.HasPrefix(pkg.Source, "sparse+"):
			found.Source = FromRegistry
		case strings.HasPrefix(pkg.Source, "git+"):
			found.Source = FromGit
		default:
			return nil, fmt.Errorf("dependency: crate %s comes from a source Cargo.lock doesn't define: %s", pkg.Name, pkg.Source)
		}
		packages = append(packages, found)
	}
	return packages, nil
}
