package macports

import (
	"regexp"
	"slices"
	"strings"
)

// pythonSubport is a port of the python PortGroup's naming: its subport
// for one Python version, py313-textual-fastdatatable.
var pythonSubport = regexp.MustCompile(`^py([0-9])([0-9]+)-(.+)$`)

// PythonPackage is the Python package a port of the python PortGroup's
// naming provides: py313-textual-fastdatatable provides
// textual-fastdatatable, as the PortGroup names each Python version's
// subport py<version>-<name>. The name is as MacPorts writes it, which a
// comparison should normalize as Python compares names. Not every port
// is named for its package, as py313-yaml provides PyYAML, so a package
// no port's name matches may still be one MacPorts provides. A port of
// another name, or the py- stub, which installs nothing, names none.
func PythonPackage(port string) (string, bool) {
	m := pythonSubport.FindStringSubmatch(port)
	if m == nil {
		return "", false
	}
	return m[3], true
}

// PythonProjects are the Python packages a port of the python PortGroup
// may provide, as PyPI names them: python.rootname, which the PortGroup
// takes the name from by default and a port sets where it differs, as
// py313-yaml's is PyYAML, else the name's (PythonPackage); and the
// project its forge setup names, where it fetches from one, as
// py313-protobuf3 fetches google/protobuf, PyPI's protobuf. Names are as
// MacPorts writes them, for a comparison to normalize as Python compares
// names. None for a port of another name.
func (p PortInfo) PythonProjects() []string {
	name, ok := PythonPackage(p.Name)
	if !ok {
		return nil
	}
	if rootname, set, err := p.option("python.rootname"); err == nil && set && rootname != "" {
		name = rootname
	}
	projects := []string{name}
	for _, option := range []string{"github.project", "gitlab.project"} {
		if project, set, err := p.option(option); err == nil && set && project != "" && !slices.Contains(projects, project) {
			projects = append(projects, project)
		}
	}
	return projects
}

// CounterpartIn is the port of a directory's other version, ports, that a
// port is compared with: itself, where they have it; else, for a Python
// subport they don't have, their newest subport of the same package, whose
// source it shares, so py311-coremltools, added beside py310-coremltools,
// is compared with it rather than taken for a new port (the Vx port's
// field testing, 2026-10-04: its license and NOTICE findings read as
// py310's alone). The name itself where neither is there.
func CounterpartIn(ports []PortInfo, name string) string {
	if slices.ContainsFunc(ports, func(port PortInfo) bool { return port.Name == name }) {
		return name
	}
	pkg, ok := PythonPackage(name)
	if !ok {
		return name
	}
	counterpart := name
	for _, port := range ports {
		if other, ok := PythonPackage(port.Name); ok && other == pkg && (counterpart == name || naturalCompare(port.Name, counterpart) > 0) {
			counterpart = port.Name
		}
	}
	return counterpart
}

// LikelyPortNames are the names MacPorts may give the port of a project
// named so, by the language PortGroups' naming: py-, p5-, rb-, and R-
// before it, a python- or perl- prefix taken as theirs, and each in lower
// case, for a name no port has, such as sdnotify, whose port is
// py-sdnotify (field testing's batch 14). The name itself is left out.
func LikelyPortNames(name string) []string {
	var names []string
	add := func(candidate string) {
		if candidate != name && candidate != "" && !slices.Contains(names, candidate) {
			names = append(names, candidate)
		}
	}
	base := name
	for prefix, lang := range map[string]string{"python-": "py-", "perl-": "p5-", "ruby-": "rb-"} {
		if rest, ok := strings.CutPrefix(strings.ToLower(name), prefix); ok {
			add(lang + rest)
			base = rest
		}
	}
	for _, prefix := range []string{"py-", "p5-", "rb-", "R-"} {
		add(prefix + base)
		add(prefix + strings.ToLower(base))
	}
	add(strings.ToLower(name))
	return names
}

// PythonVersion is the Python version a port of the python PortGroup's
// naming is built for, as Python writes it: py313-requests is 3.13, and
// py27-requests 2.7.
func PythonVersion(port string) (string, bool) {
	m := pythonSubport.FindStringSubmatch(port)
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2], true
}

// pythonRelease is a Python version as the python PortGroup writes it, 313,
// as Python writes it, 3.13.
var pythonRelease = regexp.MustCompile(`^([0-9])([0-9]+)$`)

// Pythons are the Python versions a port of the python PortGroup builds
// for, as Python writes them: a subport's own, py313-requests' 3.13; else
// each its python.versions names, as the py- stub's; else python.version's,
// as an application's port pins it with python.default_version. None for a
// port of another kind, or one evaluated before these were read.
func (p PortInfo) Pythons() []string {
	if python, ok := PythonVersion(p.Name); ok {
		return []string{python}
	}
	versions, _, err := p.optionList("python.versions")
	if err != nil || len(versions) == 0 {
		versions = nil
		if version, _, err := p.option("python.version"); err == nil && version != "" {
			versions = []string{version}
		}
	}
	var pythons []string
	for _, version := range versions {
		if python, ok := release(version); ok {
			pythons = append(pythons, python)
		}
	}
	return pythons
}

// release is a Python version as the python PortGroup writes it, as
// Python writes it.
func release(version string) (string, bool) {
	m := pythonRelease.FindStringSubmatch(version)
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2], true
}

// PythonPinned is the Python a port of the python PortGroup pins with
// python.default_version, and the one the PortGroup would give it were it
// not pinned, its own default (python_get_default_version), each as Python
// writes it; false for a port without the PortGroup, or one evaluated
// before these were read. sshuttle pins 3.13, where the default is 3.14.
// A py- port's python.versions are its own, so the default the PortGroup
// computes in the port says, capped at the newest it builds for; any other
// port's follow its pin (python_set_default_version), so the default is
// the one the PortGroup computes for a port that names none.
func (p PortInfo) PythonPinned() (pinned, standard string, ok bool) {
	pin, _, err := p.option("python.default_version")
	if err != nil {
		return "", "", false
	}
	computed := "dockhand.python_group_default"
	if strings.HasPrefix(p.Name, "py-") || pythonSubport.MatchString(p.Name) {
		computed = "dockhand.python_default"
	}
	def, _, err := p.option(computed)
	if err != nil {
		return "", "", false
	}
	pinned, pinnedOK := release(pin)
	standard, standardOK := release(def)
	return pinned, standard, pinnedOK && standardOK
}

// PythonPort is the port of a Python version, as Python writes it:
// python313 for 3.13.
func PythonPort(python string) string {
	return "python" + strings.ReplaceAll(python, ".", "")
}
