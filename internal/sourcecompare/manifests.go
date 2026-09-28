package sourcecompare

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
)

// reading is what a manifest declares: each dependency's name and its
// version or constraint, and what of the manifest the reading couldn't
// follow, such as another file it includes. indirect are what it records
// only for its dependencies' sake, as go.mod's indirect requirements: in
// the build already, though not declared.
type reading struct {
	dependencies map[string]string
	indirect     map[string]string
	unread       []string
}

// reader reads one kind of manifest; an error is a manifest it couldn't
// read at all.
type reader func([]byte) (reading, error)

// manifests are the top-level files that declare dependencies, and how to
// read them.
var manifests = map[string]reader{
	"go.mod": goModules, "Cargo.toml": cargoDependencies, "package.json": nodeDependencies,
	"requirements.txt": requirements, "pyproject.toml": pyprojectDependencies,
}

// goModules reads a go.mod's requirements: its direct ones as its own
// declarations, and its indirect ones apart, as modules the build already
// has for its dependencies' sake.
func goModules(data []byte) (reading, error) {
	parsed, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		return reading{}, err
	}
	found := reading{dependencies: map[string]string{}, indirect: map[string]string{}}
	for _, required := range parsed.Require {
		if required.Indirect {
			found.indirect[required.Mod.Path] = required.Mod.Version
		} else {
			found.dependencies[required.Mod.Path] = required.Mod.Version
		}
	}
	return found, nil
}

// cargoTables are where a Cargo.toml declares dependencies, in the order a
// name declared in several is read: its own, then for building, then for
// tests, then the workspace's.
var cargoTables = []string{"dependencies", "build-dependencies", "dev-dependencies"}

// cargoDependencies reads a Cargo.toml's dependency tables: the package's,
// each target's, and the workspace's.
func cargoDependencies(data []byte) (reading, error) {
	var manifest map[string]any
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return reading{}, err
	}
	found := reading{dependencies: map[string]string{}}
	read := func(tables map[string]any, names []string) error {
		for _, table := range names {
			entries, ok := tables[table].(map[string]any)
			if tables[table] != nil && !ok {
				return fmt.Errorf("[%s] isn't a table", table)
			}
			for _, name := range slices.Sorted(maps.Keys(entries)) {
				if _, seen := found.dependencies[name]; seen {
					continue
				}
				version, err := cargoRequirement(entries[name])
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				found.dependencies[name] = version
			}
		}
		return nil
	}
	if err := read(manifest, cargoTables); err != nil {
		return reading{}, err
	}
	if targets, ok := manifest["target"].(map[string]any); ok {
		for _, target := range slices.Sorted(maps.Keys(targets)) {
			tables, ok := targets[target].(map[string]any)
			if !ok {
				return reading{}, fmt.Errorf("[target.%s] isn't a table", target)
			}
			if err := read(tables, cargoTables); err != nil {
				return reading{}, err
			}
		}
	}
	if workspace, ok := manifest["workspace"].(map[string]any); ok {
		if err := read(workspace, []string{"dependencies"}); err != nil {
			return reading{}, err
		}
	}
	return found, nil
}

// cargoRequirement is a dependency's requirement as Cargo.toml gives it: a
// version, or a table's version, Git source, path, or workspace.
func cargoRequirement(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case map[string]any:
		if version, ok := value["version"].(string); ok {
			return version, nil
		}
		if git, ok := value["git"].(string); ok {
			for _, pin := range []string{"rev", "tag", "branch"} {
				if at, ok := value[pin].(string); ok {
					return fmt.Sprintf("git %s %s %s", git, pin, at), nil
				}
			}
			return "git " + git, nil
		}
		if dir, ok := value["path"].(string); ok {
			return "path " + dir, nil
		}
		if value["workspace"] == true {
			return "workspace", nil
		}
		return "", errors.New("neither a version, a Git source, a path, nor the workspace's")
	}
	return "", fmt.Errorf("a %T isn't a requirement", value)
}

// nodeDependencies reads a package.json's dependencies and
// devDependencies.
func nodeDependencies(data []byte) (reading, error) {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return reading{}, err
	}
	found := reading{dependencies: map[string]string{}}
	for _, set := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for name, version := range set {
			found.dependencies[name] = version
		}
	}
	return found, nil
}

// requirement is a Python requirement as PEP 508 writes one: its name, and
// its extras and version specifier, up to any environment marker.
var requirement = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*([^;]*)`)

// requirements reads a requirements.txt, one requirement a line. A line
// that includes or constrains by another file names what isn't read.
func requirements(data []byte) (reading, error) {
	found := reading{dependencies: map[string]string{}}
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		option, file, _ := strings.Cut(line, " ")
		switch option {
		case "-r", "--requirement", "-c", "--constraint":
			found.unread = append(found.unread, fmt.Sprintf("reads %s too", strings.TrimSpace(file)))
			continue
		}
		if m := requirement.FindStringSubmatch(line); m != nil {
			found.dependencies[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return found, nil
}

// pyprojectDependencies reads a pyproject.toml's dependencies: PEP 621's
// [project] array, or Poetry's table. Dependencies declared dynamically,
// from another file, are named as not read.
func pyprojectDependencies(data []byte) (reading, error) {
	var manifest struct {
		Project *struct {
			Dependencies []string `toml:"dependencies"`
			Dynamic      []string `toml:"dynamic"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies map[string]any `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return reading{}, err
	}
	found := reading{dependencies: map[string]string{}}
	if project := manifest.Project; project != nil {
		if slices.Contains(project.Dynamic, "dependencies") {
			found.unread = append(found.unread, "declares its dependencies dynamically, from another file")
		}
		for _, entry := range project.Dependencies {
			m := requirement.FindStringSubmatch(strings.TrimSpace(entry))
			if m == nil {
				return reading{}, fmt.Errorf("%q isn't a requirement", entry)
			}
			found.dependencies[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	for _, name := range slices.Sorted(maps.Keys(manifest.Tool.Poetry.Dependencies)) {
		if name == "python" {
			continue
		}
		version, err := poetryRequirement(manifest.Tool.Poetry.Dependencies[name])
		if err != nil {
			return reading{}, fmt.Errorf("%s: %w", name, err)
		}
		found.dependencies[strings.ToLower(name)] = version
	}
	return found, nil
}

// poetryRequirement is a Poetry dependency's constraint: a string, or a
// table's version, Git source, or path.
func poetryRequirement(value any) (string, error) {
	if table, ok := value.(map[string]any); ok {
		if version, ok := table["version"].(string); ok {
			return version, nil
		}
		if git, ok := table["git"].(string); ok {
			return "git " + git, nil
		}
		if dir, ok := table["path"].(string); ok {
			return "path " + dir, nil
		}
		return "", errors.New("neither a version, a Git source, nor a path")
	}
	if version, ok := value.(string); ok {
		return version, nil
	}
	return "", fmt.Errorf("a %T isn't a constraint", value)
}
