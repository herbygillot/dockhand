package project

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Requirement is a Python dependency a manifest requires: its name, as PEP
// 503 compares names, its PEP 440 version specifier, which Admits reads,
// and its PEP 508 marker, where it applies, which Evaluate reads; empty
// for one that applies everywhere.
type Requirement struct {
	Name, Specifier, Marker string
}

// OnMacOS is whether a requirement applies to a MacPorts build, with the
// Python version the build uses where it's known.
func (r Requirement) OnMacOS(pythonVersion string) (Applies, error) {
	if r.Marker == "" {
		return Yes, nil
	}
	return Evaluate(r.Marker, MacOS(pythonVersion))
}

// Declaration is one requirement as a manifest declares it: the
// requirement, and what follows its name as written, extras and
// specifier, without the marker.
type Declaration struct {
	Requirement
	Written string
}

// requirement is a Python requirement as PEP 508 writes one: its name, its
// extras and version specifier, and its environment marker after a ";".
var requirement = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*([^;]*)(?:;(.*))?$`)

// extras are a requirement's extras, "[socks]", before its specifier.
var extras = regexp.MustCompile(`^\[[^\]]*\]`)

// ParseRequirement reads a PEP 508 requirement: its name, as Python
// compares names, so a renaming such as textual_fastdatatable to
// Textual-FastDataTable moves nothing, its version specifier apart from any
// extras, and its marker, where it has one (the helper-ownership review's
// finding 1). False where the text isn't one.
func ParseRequirement(text string) (Declaration, bool) {
	m := requirement.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return Declaration{}, false
	}
	written := strings.TrimSpace(m[2])
	specifier := strings.TrimSpace(extras.ReplaceAllString(written, ""))
	if inner, ok := strings.CutPrefix(specifier, "("); ok {
		specifier = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
	}
	marker := strings.Join(strings.Fields(m[3]), " ")
	return Declaration{Requirement: Requirement{Name: NormalizeName(m[1]), Specifier: specifier, Marker: marker}, Written: written}, true
}

// Requirements is what a requirements.txt declares: its requirements, one
// a line, and the other files it includes or constrains by, which it
// names and doesn't hold.
type Requirements struct {
	Declarations []Declaration
	Includes     []string
}

// ReadRequirements reads a requirements.txt. A line that isn't a
// requirement, such as an option, is passed over.
func ReadRequirements(data []byte) Requirements {
	var found Requirements
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		option, file, _ := strings.Cut(line, " ")
		switch option {
		case "-r", "--requirement", "-c", "--constraint":
			found.Includes = append(found.Includes, strings.TrimSpace(file))
			continue
		}
		if declaration, ok := ParseRequirement(line); ok {
			found.Declarations = append(found.Declarations, declaration)
		}
	}
	return found
}

// Pyproject is what a pyproject.toml declares: PEP 621's [project], its
// PEP 518 [build-system], and Poetry's dependency table.
type Pyproject struct {
	// Project is nil where the file has no [project] table.
	Project *PythonProject
	// BuildRequires and BuildBackend are the build system's: what building
	// requires, and the backend that builds.
	BuildRequires []Declaration
	BuildBackend  string
	// Unparsed are the entries of the build system's requirements and the
	// optional groups that aren't requirements, which the reading keeps
	// rather than fails on, as it does for the project's own.
	Unparsed []string
	// Poetry are Poetry's dependencies, by name as Python compares names,
	// each with its constraint as Poetry writes it, a version, "git <url>",
	// or "path <dir>"; not python, which is RequiresPython's kin.
	Poetry map[string]string
}

// PythonProject is PEP 621's [project] table.
type PythonProject struct {
	Name string
	// License and Description are empty where they aren't strings, as a
	// PEP 621 license table isn't.
	License, Description string
	Dependencies         []Declaration
	// Optional are the optional dependencies, by their group.
	Optional map[string][]Declaration
	// RequiresPython is the Python versions the project supports, as a
	// PEP 440 specifier.
	RequiresPython string
	// Dynamic are the fields another file provides, as setuptools reads
	// dependencies from requirements.txt.
	Dynamic []string
}

// ReadPyproject reads a pyproject.toml. One of the project's own
// requirements that isn't one, or a Poetry dependency it can't read, fails
// the reading; one of the build system's or an optional group's is kept
// as unparsed.
func ReadPyproject(data []byte) (Pyproject, error) {
	var manifest struct {
		Project *struct {
			Name                 string              `toml:"name"`
			License              any                 `toml:"license"`
			Description          any                 `toml:"description"`
			Dependencies         []string            `toml:"dependencies"`
			OptionalDependencies map[string][]string `toml:"optional-dependencies"`
			RequiresPython       string              `toml:"requires-python"`
			Dynamic              []string            `toml:"dynamic"`
		} `toml:"project"`
		BuildSystem struct {
			Requires     []string `toml:"requires"`
			BuildBackend string   `toml:"build-backend"`
		} `toml:"build-system"`
		Tool struct {
			Poetry struct {
				Dependencies map[string]any `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return Pyproject{}, err
	}
	declare := func(entries []string) ([]Declaration, error) {
		var declarations []Declaration
		for _, entry := range entries {
			declaration, ok := ParseRequirement(entry)
			if !ok {
				return nil, fmt.Errorf("%q isn't a requirement", entry)
			}
			declarations = append(declarations, declaration)
		}
		return declarations, nil
	}
	found := Pyproject{BuildBackend: manifest.BuildSystem.BuildBackend}
	lenient := func(entries []string) []Declaration {
		var declarations []Declaration
		for _, entry := range entries {
			if declaration, ok := ParseRequirement(entry); ok {
				declarations = append(declarations, declaration)
			} else {
				found.Unparsed = append(found.Unparsed, entry)
			}
		}
		return declarations
	}
	if project := manifest.Project; project != nil {
		found.Project = &PythonProject{Name: project.Name, RequiresPython: project.RequiresPython, Dynamic: project.Dynamic}
		found.Project.License, _ = project.License.(string)
		found.Project.Description, _ = project.Description.(string)
		var err error
		if found.Project.Dependencies, err = declare(project.Dependencies); err != nil {
			return Pyproject{}, err
		}
		for _, group := range slices.Sorted(maps.Keys(project.OptionalDependencies)) {
			if found.Project.Optional == nil {
				found.Project.Optional = map[string][]Declaration{}
			}
			found.Project.Optional[group] = lenient(project.OptionalDependencies[group])
		}
	}
	found.BuildRequires = lenient(manifest.BuildSystem.Requires)
	for _, name := range slices.Sorted(maps.Keys(manifest.Tool.Poetry.Dependencies)) {
		if name == "python" {
			continue
		}
		constraint, err := poetryConstraint(manifest.Tool.Poetry.Dependencies[name])
		if err != nil {
			return Pyproject{}, fmt.Errorf("%s: %w", name, err)
		}
		if found.Poetry == nil {
			found.Poetry = map[string]string{}
		}
		found.Poetry[NormalizeName(name)] = constraint
	}
	return found, nil
}

// poetryConstraint is a Poetry dependency's constraint: a string, or a
// table's version, Git source, or path.
func poetryConstraint(value any) (string, error) {
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
