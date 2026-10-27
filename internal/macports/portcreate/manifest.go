package portcreate

import (
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// CargoFiles are the files beside the top-level ones a Cargo project's
// programs are read from, given its Cargo.toml: whether it has
// src/main.rs, and each workspace member's Cargo.toml and src/main.rs, a
// member named by its path; a glob, as crates/*, can't be listed file by
// file, so its members aren't read. Their presence is what Binaries
// reads.
func CargoFiles(root []byte) []string {
	files := []string{"src/main.rs"}
	manifest, err := project.ReadCargoManifest(root)
	if err != nil || manifest.Workspace == nil {
		return files
	}
	for _, member := range manifest.Workspace.Members {
		member = strings.Trim(path.Clean("/"+member), "/")
		if member == "" || strings.ContainsAny(member, "*?[") || manifest.Workspace.Exclude != nil && !manifest.Workspace.Member(member) {
			continue
		}
		files = append(files, member+"/Cargo.toml", member+"/src/main.rs")
	}
	return files
}

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

// Binaries are the programs a Rust or Go project builds, as its manifests
// name them, for its destroot to install: each Cargo package's, its
// workspace members' among them, its [[bin]] targets and the one of its
// name where it has src/main.rs (CargoFiles says what to read); or the
// one `go build` makes at go.mod's module (project.GoModule.Binary). None
// where the manifests don't say.
func Binaries(files map[string][]byte, build Build) []string {
	switch build.System {
	case "cargo":
		// Each package's programs, the root's and its members', as a
		// comparison reads them, a package building one of its name where
		// it has src/main.rs (project.CargoPrograms, CX-7).
		return project.CargoPrograms(files, func(dir string) bool {
			_, ok := files[path.Join(dir, "src/main.rs")]
			return ok
		})
	case "go":
		if module, err := project.ReadGoMod(files["go.mod"]); err == nil {
			if binary, ok := module.Binary(); ok {
				return []string{binary}
			}
		}
	}
	return nil
}
