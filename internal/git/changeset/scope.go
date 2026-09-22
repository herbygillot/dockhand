package changeset

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// ErrScope means a contribution's changed paths do not describe one port.
var ErrScope = errors.New("changeset: contribution scope")

// Resources is the directory of the shared PortGroups and definitions every
// Portfile may load.
const Resources = "_resources"

// Scope is what a contribution changes: one port directory, and whether
// files under _resources changed beside it.
type Scope struct {
	// Directory is the port directory as category/port.
	Directory string
	// Resources reports a change under _resources, a PortGroup or another
	// shared definition, which the port's update may need.
	Resources bool
}

// Portfile is the port's Portfile path.
func (s Scope) Portfile() string { return path.Join(s.Directory, "Portfile") }

// Within reports whether a changed path belongs to the scope: the port
// directory or _resources.
func (s Scope) Within(name string) bool {
	return strings.HasPrefix(name, s.Directory+"/") || strings.HasPrefix(name, Resources+"/")
}

// ScopeOf reads the one port directory a set of changed paths belongs to.
// Files under _resources may change alongside it, since a port's update
// sometimes needs the group it loads to change too; a change under
// _resources alone names no port and is refused, as is a second port
// directory or a path outside any port directory. The errors read as
// predicates of the contribution, "changes devel/a and devel/b", so a
// caller can name the subject.
func ScopeOf(paths []string) (Scope, error) {
	var scope Scope
	for _, name := range paths {
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\x00") {
			return Scope{}, fmt.Errorf("%w: changes %s, which is not a valid path", ErrScope, name)
		}
		if strings.HasPrefix(name, Resources+"/") {
			scope.Resources = true
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) < 3 || strings.HasPrefix(parts[0], ".") || strings.HasPrefix(parts[0], "_") {
			return Scope{}, fmt.Errorf("%w: changes %s, which is outside a port directory", ErrScope, name)
		}
		current := path.Join(parts[0], parts[1])
		if scope.Directory != "" && scope.Directory != current {
			return Scope{}, fmt.Errorf("%w: changes %s and %s; one port directory is supported", ErrScope, scope.Directory, current)
		}
		scope.Directory = current
	}
	switch {
	case scope.Directory == "" && scope.Resources:
		return Scope{}, fmt.Errorf("%w: changes only shared files under %s, which names no port to prepare or verify", ErrScope, Resources)
	case scope.Directory == "":
		return Scope{}, fmt.Errorf("%w: changes nothing", ErrScope)
	}
	return scope, nil
}
