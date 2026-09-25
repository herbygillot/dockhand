package engine

import (
	"regexp"
	"slices"
	"strings"
)

// portChange is MacPorts CI's rule for which directories a pull request
// changes: a Portfile, or anything under files/, in <category>/<port>
// (macports-ports .github/workflows/main.yml). Directories beginning with
// "." or "_" are not ports.
var portChange = regexp.MustCompile(`^[^._/][^/]*/[^/]+/(Portfile$|files/)`)

// Scope is what a set of changed paths touches.
type Scope struct {
	// Ports are the changed port directories, sorted.
	Ports []string
	// Resources reports a change under _resources, which CI builds nothing
	// for and which may affect every port that loads it.
	Resources bool
}

// ScopeOf applies CI's rule to changed paths.
func ScopeOf(paths []string) Scope {
	var s Scope
	for _, path := range paths {
		if strings.HasPrefix(path, "_resources/") {
			s.Resources = true
			continue
		}
		if !portChange.MatchString(path) {
			continue
		}
		parts := strings.SplitN(path, "/", 3)
		directory := parts[0] + "/" + parts[1]
		if !slices.Contains(s.Ports, directory) {
			s.Ports = append(s.Ports, directory)
		}
	}
	slices.Sort(s.Ports)
	return s
}

// PortNames are the ports' names: the last part of each directory.
func (s Scope) PortNames() []string {
	names := make([]string, len(s.Ports))
	for i, directory := range s.Ports {
		names[i] = directory[strings.LastIndexByte(directory, '/')+1:]
	}
	return names
}
