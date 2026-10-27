package evidence

import (
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// InstallChange is what a target's build installed against an earlier
// build of the port in the same environment, its previous result where
// dockhand checked it (Then's item 9): its own libraries whose install
// name or compatibility version changed, which its dependents link, and
// the files gone or added, a program, a man page, a library, a .pc or
// CMake package file. Notices, holding nothing.
type InstallChange struct {
	Target model.TargetID
	// Environment is where both builds were, and Against the version the
	// earlier one built, "1.7.1_0".
	Environment model.Environment
	Against     string
	Libraries   []LibraryChange
	Removed     []string
	Added       []string
	// Found and Lost are the configure summary's lines this build's has
	// and the earlier's didn't, and the other way about (Then's item 10):
	// duckdb's LINK_CORE_EXTENSIONS dropping two, libxkbcommon's optional
	// ICU.
	Found, Lost []string
}

// LibraryChange is one of a target's libraries whose install name or
// compatibility version moved: its file, as it was and as it is.
type LibraryChange struct {
	Was, Now model.Library
}

// Words say a library's change: "libexiv2.27.dylib is libexiv2.28.dylib
// now" or "libfoo.1.dylib's compatibility version is 2.0.0 now, was
// 1.0.0", by the install names' last part.
func (c LibraryChange) Words() string {
	if c.Was.Name != c.Now.Name {
		return path.Base(c.Was.Name) + " is " + path.Base(c.Now.Name) + " now"
	}
	return path.Base(c.Now.Name) + "'s compatibility version is " + c.Now.Compatibility + " now, was " + c.Was.Compatibility
}

// Empty reports a change that changes nothing.
func (c InstallChange) Empty() bool {
	return len(c.Libraries)+len(c.Removed)+len(c.Added)+len(c.Found)+len(c.Lost) == 0
}

// CompareConfigure diffs a build's configure summary with an earlier
// build's, purely: the lines found now, and those lost; ok is false where
// either wasn't read, which says nothing, unknown and never equal. A line
// whose answer moved, "checking for icu... no" from "... yes", is one lost
// and one found.
func CompareConfigure(before, after model.TargetResult) (found, lost []string, ok bool) {
	if before.Configure == nil || after.Configure == nil {
		return nil, nil, false
	}
	for _, line := range after.Configure {
		if !slices.Contains(before.Configure, line) {
			found = append(found, line)
		}
	}
	for _, line := range before.Configure {
		if !slices.Contains(after.Configure, line) {
			lost = append(lost, line)
		}
	}
	return found, lost, true
}

// CompareInstalled compares a build's installed files and libraries with
// an earlier build's, purely; ok is false where either wasn't read, which
// says nothing: unknown, never equal. A library is matched with the
// earlier one its file's stem names, libexiv2.27.dylib with
// libexiv2.28.dylib, since a new soname is a new file.
func CompareInstalled(before, after model.TargetResult) (InstallChange, bool) {
	if before.Installed == nil || after.Installed == nil || before.Libraries == nil || after.Libraries == nil {
		return InstallChange{}, false
	}
	change := InstallChange{Target: after.Target}
	was := map[string]bool{}
	for _, file := range before.Installed {
		was[file] = true
	}
	now := map[string]bool{}
	for _, file := range after.Installed {
		now[file] = true
		if !was[file] && !versioned(file, before.Installed) {
			change.Added = append(change.Added, file)
		}
	}
	for _, file := range before.Installed {
		if !now[file] && !versioned(file, after.Installed) {
			change.Removed = append(change.Removed, file)
		}
	}
	for _, library := range after.Libraries {
		i := slices.IndexFunc(before.Libraries, func(old model.Library) bool { return stem(old.Name) == stem(library.Name) })
		if i < 0 {
			continue
		}
		old := before.Libraries[i]
		if old.Name != library.Name || old.Compatibility != library.Compatibility && old.Compatibility != "" && library.Compatibility != "" {
			change.Libraries = append(change.Libraries, LibraryChange{Was: old, Now: library})
		}
	}
	slices.Sort(change.Added)
	slices.Sort(change.Removed)
	return change, true
}

// versioned reports a file whose name, its version parts set aside, is
// among others: a library's new soname, libfoo.2.dylib for libfoo.1.dylib,
// or a versioned directory's file, which every new version moves, is the
// same file, not one gone and one added.
func versioned(file string, others []string) bool {
	key := unversioned(file)
	return slices.ContainsFunc(others, func(other string) bool { return other != file && unversioned(other) == key })
}

// unversioned is a path with each run of digits and dots taken for one
// version.
func unversioned(file string) string {
	var b strings.Builder
	digits := false
	for _, r := range file {
		if r >= '0' && r <= '9' || digits && r == '.' {
			if !digits {
				b.WriteByte('#')
			}
			digits = true
			continue
		}
		digits = false
		b.WriteRune(r)
	}
	return b.String()
}

// stem is a library's name without its directory, version, and suffix:
// libexiv2 for /opt/local/lib/libexiv2.28.dylib.
func stem(name string) string {
	base := path.Base(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	return base
}
