package newport

import "github.com/herbygillot/dockhand/internal/project"

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
// [package], or pyproject.toml's [project], as project reads them. A field
// it doesn't give as a string, such as one a Cargo workspace inherits or a
// PEP 621 license table, is left out; so is all of a manifest that can't
// be read.
func Declare(files map[string][]byte, build Build) Declared {
	declared := Declared{File: build.Evidence}
	switch build.Evidence {
	case "Cargo.toml":
		manifest, err := project.ReadCargoManifest(files["Cargo.toml"])
		if err != nil || manifest.Package == nil {
			return Declared{}
		}
		declared.License, declared.Description = manifest.Package.License, manifest.Package.Description
	case "pyproject.toml":
		manifest, err := project.ReadPyproject(files["pyproject.toml"])
		if err != nil || manifest.Project == nil {
			return Declared{}
		}
		declared.License, declared.Description = manifest.Project.License, manifest.Project.Description
	default:
		return Declared{}
	}
	return declared
}

// Binaries are the programs a Rust or Go project builds, as its manifest
// names them, for its destroot to install: Cargo.toml's [[bin]] targets,
// else its package, which Cargo builds as its one binary; or the one `go
// build` makes at go.mod's module (project.GoModule.Binary). None where
// the manifest doesn't say, as a Cargo workspace's root doesn't.
func Binaries(files map[string][]byte, build Build) []string {
	switch build.System {
	case "cargo":
		manifest, err := project.ReadCargoManifest(files["Cargo.toml"])
		if err != nil {
			return nil
		}
		if len(manifest.Bins) == 0 && manifest.Package != nil && manifest.Package.Name != "" {
			return []string{manifest.Package.Name}
		}
		return manifest.Bins
	case "go":
		if module, err := project.ReadGoMod(files["go.mod"]); err == nil {
			if binary, ok := module.Binary(); ok {
				return []string{binary}
			}
		}
	}
	return nil
}
