package macports

import (
	"regexp"
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
func (p PortInfo) PythonPinned() (pinned, standard string, ok bool) {
	pin, _, err := p.option("python.default_version")
	if err != nil {
		return "", "", false
	}
	def, _, err := p.option("dockhand.python_default")
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
