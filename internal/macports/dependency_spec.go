package macports

import (
	"fmt"
	"regexp"
)

// Base's patterns for a depends_* entry, as portdepends'
// validate_depends_options checks each one it's given: lib:, bin:, or
// path:, with what to look for and the port that provides it, or port:
// with the port alone or after fields Base reserves and ignores, "colon-
// separated junk that we do not understand yet", as mise's
// port:bin/cmake:cmake has.
var (
	fileDependency = regexp.MustCompile(`^(lib|bin|path):[-A-Za-z0-9_/.${}^?+()|\\]+:([-._A-Za-z0-9]+)$`)
	portDependency = regexp.MustCompile(`^port(:.+)?:([-._A-Za-z0-9]+)$`)
)

// ParseDependency reads one entry of a port's depends_<phase> as MacPorts
// Base reads it: whatever Base's own patterns admit, with the port named by
// the last field, as every reader in Base but restore takes it. A lib:,
// bin:, or path: entry is met by its file where one is found, and by the
// port otherwise; which it is, Base decides where it installs. "." and ".."
// are refused as names, as ValidName refuses them.
func ParseDependency(phase, spec string) (Dependency, error) {
	match := fileDependency.FindStringSubmatch(spec)
	if match == nil {
		match = portDependency.FindStringSubmatch(spec)
	}
	if match == nil || !ValidName(match[2]) {
		return Dependency{}, fmt.Errorf("macports: invalid dependency %q", spec)
	}
	return Dependency{Port: match[2], Phase: phase, Spec: spec}, nil
}
