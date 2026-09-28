package engine

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// portChange is the port directory a changed path changes by MacPorts CI's
// rule: its Portfile, or anything under its files/ (macports-ports
// .github/workflows/main.yml).
func portChange(path string) (string, bool) {
	directory, ok := macports.PortDirectoryOf(path)
	rest := strings.TrimPrefix(path, directory+"/")
	return directory, ok && (rest == "Portfile" || strings.HasPrefix(rest, "files/"))
}

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
		if strings.HasPrefix(path, macports.ResourcesDirectory+"/") {
			s.Resources = true
			continue
		}
		directory, ok := portChange(path)
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
// names, and _resources when it changed.
func (s Scope) Changed() []string {
	names := s.PortNames()
	if s.Resources {
		names = append(names, macports.ResourcesDirectory)
	}
	return names
}

// PortNames are the ports' names: the last part of each directory.
func (s Scope) PortNames() []string {
	names := make([]string, len(s.Ports))
	for i, directory := range s.Ports {
		names[i] = directory[strings.LastIndexByte(directory, '/')+1:]
	}
	return names
}
