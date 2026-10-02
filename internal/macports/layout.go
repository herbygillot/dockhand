package macports

import (
	"io/fs"
	"slices"
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

// ValidCategory reports whether a name can be a category for a new port:
// one directory, in a port name's characters, that isn't one the tree
// keeps for itself (_resources, a dotfile directory; IsCategory).
func ValidCategory(name string) bool {
	return ValidName(name) && IsCategory(name)
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

// ChangedPort is the port directory a changed path changes by MacPorts
// CI's rule: its Portfile, or anything under its files/ (macports-ports
// .github/workflows/main.yml). A path elsewhere in a port's directory
// changes no port CI builds.
func ChangedPort(path string) (string, bool) {
	directory, ok := PortDirectoryOf(path)
	rest := strings.TrimPrefix(path, directory+"/")
	return directory, ok && (rest == "Portfile" || strings.HasPrefix(rest, "files/"))
}

// Scope is what a set of changed paths touches, by CI's rule.
type Scope struct {
	// Ports are the changed port directories, sorted.
	Ports []string
	// Resources reports a change under _resources, which CI builds nothing
	// for and which may affect every port that loads it.
	Resources bool
}

// ScopeOf applies CI's rule to changed paths: the port directories they
// change (ChangedPort), and whether _resources changed. It was engine's,
// a MacPorts fact kept as a workflow's helper (the architecture review's
// smaller items, batch 32).
func ScopeOf(paths []string) Scope {
	var s Scope
	for _, path := range paths {
		if strings.HasPrefix(path, ResourcesDirectory+"/") {
			s.Resources = true
			continue
		}
		directory, ok := ChangedPort(path)
		if !ok {
			continue
		}
		if !slices.Contains(s.Ports, directory) {
			s.Ports = append(s.Ports, directory)
		}
	}
	slices.Sort(s.Ports)
	return s
}

// Changed names what the scope changes, as a person reads it: the ports'
// directory names, and _resources when it changed.
func (s Scope) Changed() []string {
	names := s.PortNames()
	if s.Resources {
		names = append(names, ResourcesDirectory)
	}
	return names
}

// PortNames are the ports' directory names: the last part of each
// directory.
func (s Scope) PortNames() []string {
	names := make([]string, len(s.Ports))
	for i, directory := range s.Ports {
		names[i] = directory[strings.LastIndexByte(directory, '/')+1:]
	}
	return names
}
