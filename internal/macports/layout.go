package macports

import (
	"io/fs"
	"strings"
)

// The ports tree's layout, as MacPorts reads it and MacPorts CI builds
// from it: each port in a directory of its own, category/port, with its
// Portfile there, and what ports share in _resources.

// ResourcesDirectory is the ports tree's directory of what its ports
// share: the PortGroups, and what MacPorts itself reads there, such as
// the compiler lists, the fetch and livecheck definitions, and the variant
// descriptions. It is no category.
const ResourcesDirectory = "_resources"

// PortGroupDirectory is where the ports tree keeps its PortGroups, each
// in name-version.tcl.
const PortGroupDirectory = ResourcesDirectory + "/port1.0/group"

// IsCategory reports whether a directory at the top of the ports tree is
// a category, which holds port directories: one whose name begins with
// neither . nor _, as MacPorts CI reads the tree. _resources and .github
// are not.
func IsCategory(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

// PortDirectoryOf is the port directory, category/port, a path in the
// ports tree lies in. ok is false for a path in no port's directory: at
// the top of the tree, in _resources or a dotfile directory, or a file of
// a category's own. A port directory doesn't lie in itself.
func PortDirectoryOf(path string) (directory string, ok bool) {
	category, rest, found := strings.Cut(path, "/")
	if !found || !IsCategory(category) {
		return "", false
	}
	port, _, found := strings.Cut(rest, "/")
	if !found || port == "" {
		return "", false
	}
	return category + "/" + port, true
}

// ValidPortfilePath reports whether a path in the ports tree is a port's
// Portfile, category/port/Portfile, each part a path element of its own.
func ValidPortfilePath(path string) bool {
	directory, ok := PortDirectoryOf(path)
	return ok && path == directory+"/Portfile" && fs.ValidPath(path) && !strings.ContainsAny(path, "\\\x00")
}

// A PortGroup is shared code a Portfile loads by its name and version, as
// PortGroup golang 1.0 does.
type PortGroup struct {
	Name, Version string
}

// Path is where the ports tree keeps a PortGroup, as MacPorts reads it:
// _resources/port1.0/group/golang-1.0.tcl for golang 1.0.
func (g PortGroup) Path() string {
	return PortGroupDirectory + "/" + g.Name + "-" + g.Version + ".tcl"
}

// PortGroupAt is the PortGroup a path in the ports tree holds, its version
// read from after the name's last hyphen and beginning with a digit. ok is
// false for any other path.
func PortGroupAt(path string) (PortGroup, bool) {
	file, found := strings.CutPrefix(path, PortGroupDirectory+"/")
	if !found || strings.Contains(file, "/") {
		return PortGroup{}, false
	}
	stem, found := strings.CutSuffix(file, ".tcl")
	hyphen := strings.LastIndexByte(stem, '-')
	if !found || hyphen <= 0 || hyphen+1 == len(stem) || stem[hyphen+1] < '0' || stem[hyphen+1] > '9' {
		return PortGroup{}, false
	}
	return PortGroup{Name: stem[:hyphen], Version: stem[hyphen+1:]}, true
}
