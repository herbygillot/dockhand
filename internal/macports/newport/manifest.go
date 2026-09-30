package newport

import (
	"github.com/BurntSushi/toml"

	"github.com/herbygillot/dockhand/internal/macports/dependency"
)

// Declared is what a project's own manifest says of it: its license, as
// an SPDX expression, and its one-line description, each empty where the
// manifest leaves it out, and the manifest's file. A project's own words
// for these are nearer MacPorts' than a forge's: a forge detects a
// license from files, and says NOASSERTION of two, while Cargo.toml
// declares "MIT OR Apache-2.0"; and a crate's description is its
// one line, where a repository's is often a pitch.
type Declared struct {
	License, Description string
	File                 string
}

// Declare reads the manifest the build was detected from: Cargo.toml's
// [package], or pyproject.toml's [project]. A field it doesn't give as a
// string, such as one a Cargo workspace inherits or a PEP 621 license
// table, is left out; so is all of a manifest that isn't TOML.
func Declare(files map[string][]byte, build Build) Declared {
	// The fields as the manifest writes them, strings or not.
	type fields struct {
		License     any `toml:"license"`
		Description any `toml:"description"`
	}
	var found *fields
	switch build.Evidence {
	case "Cargo.toml":
		var manifest struct {
			Package *fields `toml:"package"`
		}
		if _, err := toml.Decode(string(files["Cargo.toml"]), &manifest); err == nil {
			found = manifest.Package
		}
	case "pyproject.toml":
		var manifest struct {
			Project *fields `toml:"project"`
		}
		if _, err := toml.Decode(string(files["pyproject.toml"]), &manifest); err == nil {
			found = manifest.Project
		}
	}
	if found == nil {
		return Declared{}
	}
	declared := Declared{File: build.Evidence}
	declared.License, _ = found.License.(string)
	declared.Description, _ = found.Description.(string)
	return declared
}

// Binaries are the programs a Rust or Go project builds, as its manifest
// names them, for its destroot to install: Cargo.toml's [[bin]] targets,
// else its package, which Cargo builds as its one binary; or the one `go
// build` makes at go.mod's module (dependency.GoBinary). None where the
// manifest doesn't say, as a Cargo workspace's root doesn't.
func Binaries(files map[string][]byte, build Build) []string {
	switch build.System {
	case "cargo":
		var manifest struct {
			Package *struct {
				Name string `toml:"name"`
			} `toml:"package"`
			Bin []struct {
				Name string `toml:"name"`
			} `toml:"bin"`
		}
		if _, err := toml.Decode(string(files["Cargo.toml"]), &manifest); err != nil {
			return nil
		}
		var names []string
		for _, bin := range manifest.Bin {
			if bin.Name != "" {
				names = append(names, bin.Name)
			}
		}
		if len(names) == 0 && manifest.Package != nil && manifest.Package.Name != "" {
			names = []string{manifest.Package.Name}
		}
		return names
	case "go":
		if binary, err := dependency.GoBinary(files["go.mod"]); err == nil {
			return []string{binary}
		}
	}
	return nil
}
