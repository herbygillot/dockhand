package archive

import (
	"maps"
	"path"
	"slices"
	"strings"
)

// ResourceFork reports a member macOS's archiver adds beside a file, its
// resource fork or Finder data: anything under __MACOSX/, or a file whose
// name begins with "._". It says nothing of what a project holds.
func ResourceFork(name string) bool {
	first, _, _ := strings.Cut(name, "/")
	return first == "__MACOSX" || strings.HasPrefix(path.Base(name), "._")
}

// Tops finds the one directory an archive holds its files in, the rule
// every reader of an archive's layout follows (Tree, Extract,
// project.Read, and project's inventory): by the paths of its regular
// files alone, a resource fork's left out. A directory or link member, a
// tar's pax_global_header, or a zip's directory entry doesn't count, so an
// archive made by git archive, or by the Finder, still has one top.
type Tops struct {
	names map[string]bool
	flat  bool
}

// Add counts a regular member by its cleaned path (Member.Clean).
func (t *Tops) Add(name string) {
	if ResourceFork(name) {
		return
	}
	first, _, nested := strings.Cut(name, "/")
	if !nested {
		t.flat = true
		return
	}
	if t.names == nil {
		t.names = map[string]bool{}
	}
	t.names[first] = true
}

// One is the one directory every counted file is under, false where a
// file is at the root, files are under several directories, or there were
// none.
func (t Tops) One() (string, bool) {
	if t.flat || len(t.names) != 1 {
		return "", false
	}
	for name := range t.names {
		return name, true
	}
	return "", false
}

// Flat reports a file at the root, or no file at all.
func (t Tops) Flat() bool { return t.flat || len(t.names) == 0 }

// Names are the directories counted files are under, in name order.
func (t Tops) Names() []string { return slices.Sorted(maps.Keys(t.names)) }
