// Package sourcecompare compares two versions of a project's upstream
// source, as project read them, for what a reviewer would ask about before
// an update is submitted (Design v3 §6.12): a license file, a build file,
// or a declared dependency. What it couldn't read it says, so an empty
// reading never stands for one it couldn't make.
package sourcecompare

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// Change is one difference between two versions' source that a reviewer
// would ask about, as a fact: what changed and how. What it means for a
// port, and whether it holds a submission, is macports/assess's to say.
type Change struct {
	// Kind is license, build, dependency, or unread: a file the comparison
	// couldn't read, or read only in part, or an archive whose project
	// wasn't found.
	Kind string
	// How is what happened to it:
	//   - a license or build file "added", "removed", or "changed", or
	//     "years" where only a license's copyright years moved, and
	//     "version" where a build file changed only the project's version
	//     it declares;
	//   - a dependency "adds", "drops", or "moves", or "native" for a crate
	//     new to a Cargo.lock that links a native library, and "unlinked"
	//     for one gone from it;
	//   - an unread file "truncated", "unreadable", or "unfollowed", for
	//     another file a manifest includes, and "ambiguous" for an archive
	//     whose project wasn't found.
	How string
	// Side is the version that couldn't be read, "old" or "new", of an
	// unread change.
	Side string
	// Path is the file, relative to the archive's top directory.
	Path    string
	Message string
	// System is the build system the file belongs to, a manifest's
	// language or a build file's tool; empty for a license file.
	System project.System
	// Name is a dependency's name, and Old and Now its version or
	// constraint in each version, as the manifest writes it.
	Name, Old, Now string
	// Requirements and Before are a Python dependency's declarations in
	// the new version and the old, each with its specifier and marker;
	// none for every other change.
	Requirements, Before []project.Requirement
}

// A copyright line names its holder and years: "Copyright (c) 2016-2026
// Kenneth Shaw". Its years are one, or a range or list of them.
var (
	copyrightLine  = regexp.MustCompile(`(?i)copyright|\(c\)|©`)
	copyrightYears = regexp.MustCompile(`\b(19|20)\d\d(\s*(-|–|,|and)\s*(19|20)\d\d)*\b`)
)

// yearsOnly reports whether two versions of a license file differ only in
// the years of their copyright lines, as a new year moves them, and gives
// the first such line as it now reads. A line added, removed, or changed in
// anything but its years is a change to the license file, which is read no
// further: a new holder, or a license's own text, could need the Portfile's
// license line to follow.
func yearsOnly(old, now []byte) (string, bool) {
	before, after := strings.Split(string(old), "\n"), strings.Split(string(now), "\n")
	if len(before) != len(after) {
		return "", false
	}
	first := ""
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		if !copyrightLine.MatchString(before[i]) || !copyrightLine.MatchString(after[i]) ||
			copyrightYears.ReplaceAllString(before[i], "") != copyrightYears.ReplaceAllString(after[i], "") {
			return "", false
		}
		if first == "" {
			first = quotable(after[i])
		}
	}
	return first, first != ""
}

// Versions are the release an update moves from and to, which a build file
// that changes only the version it names spells.
type Versions struct{ Old, New string }

// Compare reports the license files, build files, and declared
// dependencies that differ between two versions' readings, the old and the
// new, and what it couldn't read of them. Each is by its path below its
// archive's top, whose name, the version's, is set aside so the same file
// compares across versions. A version whose project couldn't be found is
// said.
func Compare(older, newer project.Reading, versions Versions) []Change {
	var lost []Change
	for _, side := range []struct {
		version string
		reading project.Reading
	}{{"old", older}, {"new", newer}} {
		if side.reading.Layout == project.Ambiguous {
			lost = append(lost, Change{Kind: "unread", How: "ambiguous", Side: side.version,
				Message: fmt.Sprintf("upstream's %s archive holds %s and no file beside them, so which is the project wasn't found, and it wasn't compared", side.version, strings.Join(side.reading.Tops, ", "))})
		}
	}
	if len(lost) > 0 {
		return lost
	}
	before, after := older.Files, newer.Files
	var names []string
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []Change
	for _, name := range names {
		old, hadOld := before[name]
		now, hasNow := after[name]
		base := path.Base(name)
		if old.Truncated || now.Truncated {
			changes = append(changes, Change{Kind: "unread", How: "truncated", Path: name,
				Message: fmt.Sprintf("upstream's %s is larger than the %d KiB the comparison reads, so it wasn't compared", name, project.FileLimit>>10)})
			continue
		}
		if hadOld && hasNow && bytes.Equal(old.Data, now.Data) {
			continue
		}
		switch {
		case base == project.CargoLock:
			changes = append(changes, nativeLinks(name, old, hadOld, now, hasNow)...)
		case project.Manifest(base):
			changes = append(changes, manifestChanges(name, old, hadOld, now, hasNow)...)
		case project.LicenseFile(base) && hadOld && hasNow:
			if line, ok := yearsOnly(old.Data, now.Data); ok {
				changes = append(changes, Change{Kind: "license", How: "years", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only its copyright years: %q", name, line)})
				continue
			}
			fallthrough
		case project.LicenseFile(base):
			how, what := "changed", "changed"
			switch {
			case !hadOld:
				how, what = "added", "was added"
			case !hasNow:
				how, what = "removed", "was removed"
			}
			changes = append(changes, Change{Kind: "license", How: how, Path: name,
				Message: fmt.Sprintf("upstream's %s %s", name, what)})
		default:
			if line, ok := versionOnly(name, old.Data, now.Data, versions); hadOld && hasNow && ok {
				changes = append(changes, Change{Kind: "build", How: "version", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only the version it names: %q", name, line)})
				continue
			}
			how, what := "changed", "changed"
			switch {
			case !hadOld:
				how, what = "added", "is new"
			case !hasNow:
				how, what = "removed", "was removed"
			}
			changes = append(changes, Change{Kind: "build", How: how, Path: name,
				Message: fmt.Sprintf("upstream's %s %s; the build may need the Portfile to follow", name, what)})
		}
	}
	for i := range changes {
		changes[i].System = project.SystemOf(changes[i].Path)
	}
	return changes
}

// quotable is a line as a message quotes it: trimmed, and cut short past
// 120 characters.
func quotable(line string) string {
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > 120 {
		return string(runes[:119]) + "…"
	}
	return line
}

// versionOnly reports whether a build file's two versions differ only in
// the project's version they declare, as nuspell's CMakeLists.txt changed
// only project(nuspell VERSION 5.1.9), and gives the first such line as it
// now reads: each line that differs reads as the new one once the old
// version in it is the new one, and declares the project's version as its
// build system does. A line added or removed, or changed in anything else,
// is a change to the build, as find_package(SomeLibrary 1.0) moving with
// the project's version is (the update-workflow review's finding 2).
func versionOnly(name string, old, now []byte, versions Versions) (string, bool) {
	if versions.Old == "" || versions.New == "" || versions.Old == versions.New {
		return "", false
	}
	before, after := strings.Split(string(old), "\n"), strings.Split(string(now), "\n")
	if len(before) != len(after) {
		return "", false
	}
	first := ""
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		if strings.ReplaceAll(before[i], versions.Old, versions.New) != after[i] || !project.DeclaresVersion(name, after[i], versions.New) {
			return "", false
		}
		if first == "" {
			first = quotable(after[i])
		}
	}
	return first, first != ""
}

// manifestChanges compares what a manifest declares in each version, and
// says what it couldn't read of either.
func manifestChanges(name string, old project.File, hadOld bool, now project.File, hasNow bool) []Change {
	base := path.Base(name)
	unread := func(how, side, what string) Change {
		return Change{Kind: "unread", How: how, Side: side, Path: name, Message: fmt.Sprintf("upstream's %s %s", name, what)}
	}
	var readings [2]reading
	for i, side := range []struct {
		version string
		file    project.File
		present bool
	}{{"old", old, hadOld}, {"new", now, hasNow}} {
		if !side.present {
			continue
		}
		found, err := readManifest(base, side.file.Data)
		if err != nil {
			return []Change{unread("unreadable", side.version, fmt.Sprintf("couldn't be read in the %s version, so its dependencies weren't compared: %v", side.version, err))}
		}
		readings[i] = found
	}
	// What it couldn't follow comes first: in either version, since a gap
	// in the old one leaves what changed as unknown as one in the new. A
	// gap both share is said once.
	var changes []Change
	for _, gap := range readings[1].unread {
		changes = append(changes, unread("unfollowed", "new", gap+", which the comparison doesn't follow"))
	}
	for _, gap := range readings[0].unread {
		if !slices.Contains(readings[1].unread, gap) {
			changes = append(changes, unread("unfollowed", "old", "in the old version "+gap+", which the comparison doesn't follow"))
		}
	}
	return append(changes, dependencyChanges(name, readings[0], readings[1])...)
}

// dependencyDelta is one declared dependency that differs between the
// versions: how, "adds", "drops", or "moves", and its version on each side,
// marked indirect where the build had or keeps it only for another's sake.
type dependencyDelta struct {
	how, name, old, now     string
	wasIndirect, isIndirect bool
}

// dependencyDeltas are what a manifest's declared dependencies gained,
// lost, and moved, by name. A dependency the build had or keeps for
// another's sake, as a Go module required indirectly, is neither gained
// nor lost: it moves only where its version does.
func dependencyDeltas(before, after reading) []dependencyDelta {
	var names []string
	for name := range after.dependencies {
		names = append(names, name)
	}
	for name := range before.dependencies {
		if _, ok := after.dependencies[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var deltas []dependencyDelta
	for _, name := range names {
		old, had := before.dependencies[name]
		now, has := after.dependencies[name]
		delta := dependencyDelta{name: name}
		if !had {
			old, delta.wasIndirect = before.indirect[name]
		}
		if !has {
			now, delta.isIndirect = after.indirect[name]
		}
		delta.old, delta.now = old, now
		switch {
		case !had && !delta.wasIndirect:
			delta.how = "adds"
		case !has && !delta.isIndirect:
			delta.how = "drops"
		case old != now:
			delta.how = "moves"
		default:
			continue
		}
		deltas = append(deltas, delta)
	}
	return deltas
}

// dependencyChanges are what a manifest's declared dependencies gained,
// lost, and moved, one change each, in name order.
func dependencyChanges(file string, before, after reading) []Change {
	spelled := func(version string, indirect bool) string {
		if indirect {
			return version + " (indirect)"
		}
		return version
	}
	var changes []Change
	for _, delta := range dependencyDeltas(before, after) {
		change := Change{Kind: "dependency", How: delta.how, Path: file, Name: delta.name, Old: delta.old, Now: delta.now,
			Requirements: after.requirements[delta.name], Before: before.requirements[delta.name]}
		switch delta.how {
		case "adds":
			change.Message = strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, delta.name, delta.now))
		case "drops":
			change.Message = fmt.Sprintf("upstream: %s drops %s", file, delta.name)
		default:
			change.Message = fmt.Sprintf("upstream: %s moves %s from %s to %s", file, delta.name, spelled(delta.old, delta.wasIndirect), spelled(delta.now, delta.isIndirect))
		}
		changes = append(changes, change)
	}
	return changes
}

// nativeLinks lists the crates new to a Cargo.lock that link a native
// library, as Cargo's -sys crates do. Such a crate often links a copy of
// the library it finds installed, and builds one it bundles otherwise,
// which a clean check can't tell apart: where MacPorts has the library,
// the Portfile may want to declare it, which is assess's to say. It lists
// those gone from it too,
// "unlinked": what the Portfile declared for the library may be left.
func nativeLinks(name string, old project.File, hadOld bool, now project.File, hasNow bool) []Change {
	if !hasNow {
		return nil
	}
	unread := func(version string, err error) []Change {
		return []Change{{Kind: "unread", How: "unreadable", Side: version, Path: name, Message: fmt.Sprintf("upstream's Cargo.lock couldn't be read in the %s version, so the crates new to it that link a native library weren't looked for: %v", version, err)}}
	}
	packages, err := project.ReadCargoLock(now.Data)
	if err != nil {
		return unread("new", err)
	}
	had, has := map[string]bool{}, map[string]bool{}
	var earlier []project.CargoPackage
	if hadOld {
		earlier, err = project.ReadCargoLock(old.Data)
		if err != nil {
			return unread("old", err)
		}
		for _, pkg := range earlier {
			had[pkg.Name] = true
		}
	}
	for _, pkg := range packages {
		has[pkg.Name] = true
	}
	var changes []Change
	for _, pkg := range packages {
		library := pkg.NativeLibrary()
		if library == "" || had[pkg.Name] {
			continue
		}
		// Once, whichever versions the lock pins.
		had[pkg.Name] = true
		changes = append(changes, Change{Kind: "dependency", How: "native", Path: name, Name: pkg.Name, Now: pkg.Version,
			Message: fmt.Sprintf("upstream: Cargo.lock adds %s %s, which links the native library %s", pkg.Name, pkg.Version, library)})
	}
	for _, pkg := range earlier {
		library := pkg.NativeLibrary()
		if library == "" || has[pkg.Name] {
			continue
		}
		has[pkg.Name] = true
		changes = append(changes, Change{Kind: "dependency", How: "unlinked", Path: name, Name: pkg.Name, Old: pkg.Version,
			Message: fmt.Sprintf("upstream: Cargo.lock drops %s, which linked the native library %s", pkg.Name, library)})
	}
	return changes
}
