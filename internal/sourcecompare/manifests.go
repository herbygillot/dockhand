package sourcecompare

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// reading is what a manifest declares, as the comparison compares it: each
// dependency's name and its version or constraint, and what of the
// manifest the reading couldn't follow, such as another file it includes.
// indirect are what it records only for its dependencies' sake, as go.mod's
// indirect requirements: in the build already, though not declared.
type reading struct {
	dependencies map[string]string
	indirect     map[string]string
	unread       []string
	// requirements are a Python manifest's PEP 508 requirements, by the
	// dependency's name as dependencies has it: every declaration of it,
	// each with its version specifier and its marker, so one declared
	// twice, under two conditions, keeps both. Not Poetry's constraints,
	// which are another syntax.
	requirements map[string][]project.Requirement
}

// readManifest reads one kind of manifest, by its name, through project's
// reader for it; an error is a manifest it couldn't read at all.
func readManifest(name string, data []byte) (reading, error) {
	found := reading{dependencies: map[string]string{}}
	switch name {
	case "go.mod":
		// Its direct requirements are its own declarations, and its
		// indirect ones apart, as modules the build already has for its
		// dependencies' sake.
		module, err := project.ReadGoModLax(data)
		if err != nil {
			return reading{}, err
		}
		found.indirect = map[string]string{}
		for _, required := range module.Requires {
			if required.Indirect {
				found.indirect[required.Path] = required.Version
			} else {
				found.dependencies[required.Path] = required.Version
			}
		}
	case "Cargo.toml":
		manifest, err := project.ReadCargoManifest(data)
		if err != nil {
			return reading{}, err
		}
		// A crate declared in several tables is read where it's first, by
		// the crate it is, whatever the project renames it; one only a
		// target other than macOS's has, as cfg(windows)'s, isn't the
		// build's, and a target table whose key can't be read applies as
		// far as can be told.
		for _, dependency := range manifest.Dependencies {
			if applies, err := dependency.OnMacOS(); err == nil && applies == project.No {
				continue
			}
			if _, seen := found.dependencies[dependency.Crate()]; !seen {
				found.dependencies[dependency.Crate()] = cargoConstraint(dependency)
			}
		}
	case "package.json":
		manifest, err := project.ReadPackageJSON(data)
		if err != nil {
			return reading{}, err
		}
		for _, set := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
			maps.Copy(found.dependencies, set)
		}
	case "requirements.txt":
		// A line that includes or constrains by another file names what
		// isn't read.
		requirements := project.ReadRequirements(data)
		for _, file := range requirements.Includes {
			found.unread = append(found.unread, fmt.Sprintf("reads %s too", file))
		}
		for _, declaration := range requirements.Declarations {
			found.require(declaration)
		}
	case "pyproject.toml":
		// PEP 621's [project] array, or Poetry's table. Dependencies
		// declared dynamically, from another file, are named as not read.
		manifest, err := project.ReadPyproject(data)
		if err != nil {
			return reading{}, err
		}
		if manifest.Project != nil {
			if slices.Contains(manifest.Project.Dynamic, "dependencies") {
				found.unread = append(found.unread, "declares its dependencies dynamically, from another file")
			}
			for _, declaration := range manifest.Project.Dependencies {
				found.require(declaration)
			}
		}
		maps.Copy(found.dependencies, manifest.Poetry)
	default:
		return reading{}, fmt.Errorf("%s isn't a manifest the comparison reads", name)
	}
	return found, nil
}

// cargoConstraint is a Cargo dependency as the comparison compares it: its
// version, with its Git source where it has both, since its revision
// moving under the same version is a change too (the helper-ownership
// review's finding 1); and marked optional, since one a feature turns on
// becoming one every build has is a change too (the txt run's finding 6).
func cargoConstraint(dependency project.CargoDependency) string {
	source := ""
	if dependency.Git != "" {
		source = "git " + dependency.Git
		if dependency.Pin != "" {
			source = fmt.Sprintf("git %s %s %s", dependency.Git, dependency.Pin, dependency.At)
		}
	}
	optional := ""
	if dependency.Optional {
		optional = " (optional)"
	}
	switch {
	case dependency.Version != "" && source != "":
		return dependency.Version + " (" + source + ")" + optional
	case dependency.Version != "":
		return dependency.Version + optional
	case source != "":
		return source
	case dependency.Path != "":
		return "path " + dependency.Path
	}
	return "workspace"
}

// require records a PEP 508 requirement, as written, under its marker
// where it has one; a name declared twice keeps both.
func (r *reading) require(declaration project.Declaration) {
	declared := declaration.Written
	if declaration.Marker != "" {
		declared = strings.TrimSpace(declaration.Written + "; " + declaration.Marker)
	}
	if prior, ok := r.dependencies[declaration.Name]; ok {
		declared = prior + " | " + declared
	}
	r.dependencies[declaration.Name] = declared
	if r.requirements == nil {
		r.requirements = map[string][]project.Requirement{}
	}
	r.requirements[declaration.Name] = append(r.requirements[declaration.Name], declaration.Requirement)
}
