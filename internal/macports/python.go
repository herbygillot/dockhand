package macports

import "regexp"

// pythonSubport is a port of the python PortGroup's naming: its subport
// for one Python version, py313-textual-fastdatatable.
var pythonSubport = regexp.MustCompile(`^py[0-9]+-(.+)$`)

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
	return m[1], true
}
