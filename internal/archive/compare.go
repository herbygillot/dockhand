package archive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Change is one difference between two versions' source archives that a
// reviewer would ask about (Design v3 §6.12): a license file, a build
// file, or a declared dependency. Hold marks what a person should look at
// before the update is submitted; the rest is information.
type Change struct {
	// Kind is license, build, or dependency.
	Kind string
	// Path is the file, relative to the archive's top directory.
	Path    string
	Message string
	Hold    bool
}

func (c Change) String() string {
	mark := "·"
	if c.Hold {
		mark = "!"
	}
	return mark + " " + c.Message
}

// memberLimit is the most of one file the comparison reads.
const memberLimit = 1 << 20

var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|copyright|notice|unlicense)([._-].*)?$`)

// buildNames are the top-level files that say how software builds.
var buildNames = []string{"CMakeLists.txt", "configure.ac", "configure.in", "meson.build", "meson_options.txt", "Makefile.am", "Makefile.PL",
	"setup.py", "setup.cfg", "build.gradle", "pom.xml", "SConstruct", "build.zig", "Package.swift", "Gemfile", "cpanfile", "DESCRIPTION"}

// manifests are the top-level files that declare dependencies, and how to
// read them: each gives a dependency's name and its version or constraint.
var manifests = map[string]func([]byte) map[string]string{
	"go.mod": goModules, "Cargo.toml": cargoDependencies, "package.json": nodeDependencies,
	"requirements.txt": requirements, "pyproject.toml": pyprojectDependencies,
}

// Compare reads two versions' archives, the old and the new, and reports
// the license files, build files, and declared dependencies that differ.
// Each archive's single top directory, which names its version, is set
// aside so the same file compares across versions.
func Compare(ctx context.Context, older, newer string) ([]Change, error) {
	before, err := interesting(ctx, older)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path.Base(older), err)
	}
	after, err := interesting(ctx, newer)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path.Base(newer), err)
	}
	var names []string
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []Change
	for _, name := range names {
		old, hadOld := before[name]
		now, hasNow := after[name]
		if hadOld && hasNow && bytes.Equal(old, now) {
			continue
		}
		base := path.Base(name)
		switch {
		case manifests[name] != nil:
			changes = append(changes, dependencyChanges(name, manifests[name](old), manifests[name](now))...)
		case licenseName.MatchString(base):
			what := "changed"
			switch {
			case !hadOld:
				what = "was added"
			case !hasNow:
				what = "was removed"
			}
			changes = append(changes, Change{Kind: "license", Path: name, Hold: true,
				Message: fmt.Sprintf("upstream's %s %s; the Portfile's license line may need to follow", name, what)})
		default:
			what := "changed"
			switch {
			case !hadOld:
				what = "is new"
			case !hasNow:
				what = "was removed"
			}
			changes = append(changes, Change{Kind: "build", Path: name, Hold: true,
				Message: fmt.Sprintf("upstream's %s %s; the build may need the Portfile to follow", name, what)})
		}
	}
	return changes, nil
}

// interesting reads an archive's license files, top-level build files, and
// dependency manifests, by their path below its top directory.
func interesting(ctx context.Context, filename string) (map[string][]byte, error) {
	files := map[string][]byte{}
	top := ""
	err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular {
			return nil
		}
		first, rest, nested := strings.Cut(name, "/")
		if !nested {
			return nil
		}
		if top == "" {
			top = first
		}
		if first != top {
			return nil
		}
		depth := strings.Count(rest, "/")
		base := path.Base(rest)
		wanted := licenseName.MatchString(base) && depth <= 1 ||
			depth == 0 && (slices.Contains(buildNames, base) || manifests[base] != nil)
		if !wanted {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, memberLimit))
		if err != nil {
			return err
		}
		files[rest] = data
		return nil
	})
	return files, err
}

func dependencyChanges(file string, before, after map[string]string) []Change {
	var names []string
	for name := range after {
		names = append(names, name)
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []Change
	for _, name := range names {
		old, had := before[name]
		now, has := after[name]
		switch {
		case !had:
			changes = append(changes, Change{Kind: "dependency", Path: file, Hold: true, Message: strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, name, now))})
		case !has:
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s drops %s", file, name)})
		case old != now:
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s moves %s from %s to %s", file, name, old, now)})
		}
	}
	// What holds the update for a look comes first.
	slices.SortStableFunc(changes, func(a, b Change) int {
		switch {
		case a.Hold == b.Hold:
			return 0
		case a.Hold:
			return -1
		}
		return 1
	})
	return changes
}

var goRequire = regexp.MustCompile(`^\s*([^\s()]+)\s+(v\S+)`)

// goModules reads a go.mod's direct requirements; indirect ones are left
// out, as they are not the module's own declarations.
func goModules(data []byte) map[string]string {
	modules := map[string]string{}
	block := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			block = true
			continue
		case block && trimmed == ")":
			block = false
			continue
		case strings.HasPrefix(trimmed, "require "):
			line = strings.TrimPrefix(trimmed, "require ")
		case !block:
			continue
		}
		if strings.Contains(line, "// indirect") {
			continue
		}
		if m := goRequire.FindStringSubmatch(line); m != nil {
			modules[m[1]] = m[2]
		}
	}
	return modules
}

var tomlKey = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)\s*=\s*(.*)$`)

// cargoDependencies reads the keys of a Cargo.toml's dependency tables.
func cargoDependencies(data []byte) map[string]string {
	dependencies := map[string]string{}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = strings.Trim(trimmed, "[] ")
			continue
		}
		if !strings.HasSuffix(section, "dependencies") {
			continue
		}
		if m := tomlKey.FindStringSubmatch(trimmed); m != nil {
			dependencies[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `"`)
		}
	}
	return dependencies
}

// nodeDependencies reads a package.json's dependencies and
// devDependencies.
func nodeDependencies(data []byte) map[string]string {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	dependencies := map[string]string{}
	if json.Unmarshal(data, &manifest) != nil {
		return dependencies
	}
	for _, set := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for name, version := range set {
			dependencies[name] = version
		}
	}
	return dependencies
}

var requirementLine = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*(.*)$`)

// requirements reads a requirements.txt, one requirement a line.
func requirements(data []byte) map[string]string {
	found := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if m := requirementLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			found[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return found
}

var pyprojectEntry = regexp.MustCompile(`"([A-Za-z0-9][A-Za-z0-9._-]*)\s*([^";]*)[^"]*"`)

// pyprojectDependencies reads a pyproject.toml's [project] dependencies
// array.
func pyprojectDependencies(data []byte) map[string]string {
	found := map[string]string{}
	text := string(data)
	start := regexp.MustCompile(`(?m)^\s*dependencies\s*=\s*\[`).FindStringIndex(text)
	if start == nil {
		return found
	}
	end := strings.Index(text[start[1]:], "]")
	if end < 0 {
		return found
	}
	for _, m := range pyprojectEntry.FindAllStringSubmatch(text[start[1]:start[1]+end], -1) {
		found[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
	}
	return found
}
