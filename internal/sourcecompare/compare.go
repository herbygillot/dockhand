// Package sourcecompare compares two versions of a project's upstream
// source, as their release archives hold it, for what a reviewer would ask
// about before an update is submitted (Design v3 §6.12): a license file, a
// build file, or a declared dependency. It reads the archives through
// archive's traversal, and owns what their files mean: which files are
// license and build files, and how each manifest declares dependencies.
// What it couldn't read it says, so an empty reading never stands for one
// it couldn't make.
package sourcecompare

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
)

// Change is one difference between two versions' source archives that a
// reviewer would ask about. Hold marks what a person should look at before
// the update is submitted; the rest is information.
type Change struct {
	// Kind is license, build, dependency, or unread: a file the comparison
	// couldn't read, or read only in part, which holds as a change would.
	Kind string
	// Path is the file, relative to the archive's top directory.
	Path    string
	Message string
	Hold    bool
	// System is the build system the file belongs to, a manifest's
	// language or a build file's tool, for a caller that knows which the
	// port uses; empty for a license file.
	System macports.BuildSystem
	// Requirements are a Python requirement the new version adds or
	// moves, each declaration of it, for what it asks of the port that
	// provides it; none for every other change.
	Requirements []Requirement
}

// Requirement is a Python dependency a manifest requires: its name, as PEP
// 503 compares names, its PEP 440 version specifier, which Admits reads,
// and its PEP 508 marker, where it applies, which Evaluate reads; empty
// for one that applies everywhere.
type Requirement struct {
	Name, Specifier, Marker string
}

// OnMacOS is whether a requirement applies to a MacPorts build, with the
// Python version the build uses where it's known.
func (r Requirement) OnMacOS(pythonVersion string) (Applies, error) {
	if r.Marker == "" {
		return Yes, nil
	}
	return Evaluate(r.Marker, MacOS(pythonVersion))
}

// memberLimit is the most of one file the comparison reads.
const memberLimit = 1 << 20

var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|copyright|notice|unlicense)([._-].*)?$`)

// sourceExtensions are a program's source, never a license file, whatever
// the file is named: usql's text/license.go is Go.
var sourceExtensions = []string{".c", ".cc", ".cpp", ".cs", ".go", ".h", ".java", ".js", ".kt", ".lua", ".m", ".php", ".pl", ".py", ".rb", ".rs", ".scala", ".sh", ".swift", ".tcl", ".ts"}

// licenseFile reports a license file by its name: LICENSE, COPYING, NOTICE,
// and their kin, such as LICENSE-MIT or COPYING.LESSER, but not a program's
// source named for one.
func licenseFile(base string) bool {
	return licenseName.MatchString(base) && !slices.Contains(sourceExtensions, strings.ToLower(path.Ext(base)))
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

// systems are the build system each top-level build file and manifest
// belongs to.
var systems = map[string]macports.BuildSystem{
	"CMakeLists.txt": macports.CMake, "configure.ac": macports.Autotools, "configure.in": macports.Autotools, "Makefile.am": macports.Autotools,
	"meson.build": macports.Meson, "meson_options.txt": macports.Meson, "Makefile.PL": macports.Perl, "cpanfile": macports.Perl,
	"setup.py": macports.Python, "setup.cfg": macports.Python, "requirements.txt": macports.Python, "pyproject.toml": macports.Python,
	"build.gradle": macports.Java, "pom.xml": macports.Java, "SConstruct": macports.SCons, "build.zig": macports.Zig,
	"Package.swift": macports.Swift, "Gemfile": macports.Ruby, "DESCRIPTION": macports.R,
	"go.mod": macports.Go, "Cargo.toml": macports.Cargo, cargoLock: macports.Cargo, "package.json": macports.Node,
}

// Versions are the release an update moves from and to, which a build file
// that changes only the version it names spells.
type Versions struct{ Old, New string }

// buildNames are the top-level files that say how software builds.
var buildNames = []string{"CMakeLists.txt", "configure.ac", "configure.in", "meson.build", "meson_options.txt", "Makefile.am", "Makefile.PL",
	"setup.py", "setup.cfg", "build.gradle", "pom.xml", "SConstruct", "build.zig", "Package.swift", "Gemfile", "cpanfile", "DESCRIPTION"}

// proven are the manifests whose dependencies a check proves. A Go module
// or a Rust crate is compiled into what the port builds, and a check builds
// with only what the port declares, so one needing a library the port
// doesn't declare fails it. What they change is counted, and holds nothing,
// nor does what the comparison couldn't read of them (D9). A Python or Node
// dependency is another port, found when the software runs, which a build
// doesn't prove.
var proven = map[string]bool{"go.mod": true, "Cargo.toml": true, cargoLock: true}

// cargoLock is read for the crates new to it that link a native library.
const cargoLock = "Cargo.lock"

// file is a member as the comparison read it: its first memberLimit bytes,
// and whether there was more.
type file struct {
	data      []byte
	truncated bool
}

// Compare reads two versions' archives, the old and the new, and reports
// the license files, build files, and declared dependencies that differ,
// and what it couldn't read of them. Each archive's single top directory,
// which names its version, is set aside so the same file compares across
// versions.
func Compare(ctx context.Context, older, newer string, versions Versions) ([]Change, error) {
	before, err := interesting(ctx, older)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path.Base(older), err)
	}
	after, err := interesting(ctx, newer)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path.Base(newer), err)
	}
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
		if old.truncated || now.truncated {
			changes = append(changes, Change{Kind: "unread", Path: name, Hold: !proven[name],
				Message: fmt.Sprintf("upstream's %s is larger than the %d KiB the comparison reads, so it wasn't compared", name, memberLimit>>10)})
			continue
		}
		if hadOld && hasNow && bytes.Equal(old.data, now.data) {
			continue
		}
		base := path.Base(name)
		switch {
		case name == cargoLock:
			changes = append(changes, nativeLinks(old, hadOld, now, hasNow)...)
		case manifests[name] != nil:
			changes = append(changes, manifestChanges(name, manifests[name], old, hadOld, now, hasNow)...)
		case licenseFile(base) && hadOld && hasNow:
			if line, ok := yearsOnly(old.data, now.data); ok {
				changes = append(changes, Change{Kind: "license", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only its copyright years: %q", name, line)})
				continue
			}
			fallthrough
		case licenseFile(base):
			what := "changed"
			switch {
			case !hadOld:
				what = "was added"
			case !hasNow:
				what = "was removed"
			}
			changes = append(changes, Change{Kind: "license", Path: name, Hold: true,
				Message: fmt.Sprintf("upstream's %s %s; the Portfile's license line may need to follow", name, what)})
		default:
			if line, ok := versionOnly(old.data, now.data, versions); hadOld && hasNow && ok {
				changes = append(changes, Change{Kind: "build", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only the version it names: %q", name, line)})
				continue
			}
			what := "changed"
			switch {
			case !hadOld:
				what = "is new"
			case !hasNow:
				what = "was removed"
			}
			changes = append(changes, Change{Kind: "build", Path: name, Hold: true,
				Message: fmt.Sprintf("upstream's %s %s; the build may need the Portfile to follow", name, what)})
		}
	}
	for i := range changes {
		changes[i].System = systems[path.Base(changes[i].Path)]
	}
	return changes, nil
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
// the release they name, as nuspell's CMakeLists.txt changed only
// project(nuspell VERSION 5.1.9), and gives the first such line as it now
// reads: each line that differs reads as the new one once the old version
// in it is the new one. A line added or removed, or changed in anything
// else, is a change to the build.
func versionOnly(old, now []byte, versions Versions) (string, bool) {
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
		if strings.ReplaceAll(before[i], versions.Old, versions.New) != after[i] {
			return "", false
		}
		if first == "" {
			first = quotable(after[i])
		}
	}
	return first, first != ""
}

// interesting reads an archive's license files, top-level build files, and
// dependency manifests, by their path below its top directory.
func interesting(ctx context.Context, filename string) (map[string]file, error) {
	files := map[string]file{}
	top := ""
	err := archive.Walk(ctx, filename, func(member archive.Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular {
			return nil
		}
		first, rest, nested := strings.Cut(name, "/")
		if !nested {
			return nil
		}
		if top == "" {
			top = first
		}
		if first != top {
			return nil
		}
		depth := strings.Count(rest, "/")
		base := path.Base(rest)
		wanted := licenseFile(base) && depth <= 1 ||
			depth == 0 && (slices.Contains(buildNames, base) || manifests[base] != nil || base == cargoLock)
		if !wanted {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, memberLimit+1))
		if err != nil {
			return err
		}
		if len(data) > memberLimit {
			files[rest] = file{data: data[:memberLimit], truncated: true}
			return nil
		}
		files[rest] = file{data: data}
		return nil
	})
	return files, err
}

// manifestChanges compares what a manifest declares in each version, and
// says what it couldn't read of either.
func manifestChanges(name string, read reader, old file, hadOld bool, now file, hasNow bool) []Change {
	unread := func(what string) Change {
		return Change{Kind: "unread", Path: name, Hold: !proven[name], Message: fmt.Sprintf("upstream's %s %s", name, what)}
	}
	var readings [2]reading
	for i, side := range []struct {
		version string
		file    file
		present bool
	}{{"old", old, hadOld}, {"new", now, hasNow}} {
		if !side.present {
			continue
		}
		found, err := read(side.file.data)
		if err != nil {
			return []Change{unread(fmt.Sprintf("couldn't be read in the %s version, so its dependencies weren't compared: %v", side.version, err))}
		}
		readings[i] = found
	}
	// What it couldn't follow holds, so it comes first: in either version,
	// since a gap in the old one leaves what changed as unknown as one in
	// the new. A gap both share is said once.
	var changes []Change
	for _, gap := range readings[1].unread {
		changes = append(changes, unread(gap+", which the comparison doesn't follow"))
	}
	for _, gap := range readings[0].unread {
		if !slices.Contains(readings[1].unread, gap) {
			changes = append(changes, unread("in the old version "+gap+", which the comparison doesn't follow"))
		}
	}
	if proven[name] {
		return append(changes, dependencyCount(name, readings[0], readings[1])...)
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

// elsewhere reports declarations of a requirement that all apply only
// elsewhere than macOS, by their markers; none, or one that may apply, is
// not.
func elsewhere(declarations []Requirement) bool {
	for _, declaration := range declarations {
		if applies, err := declaration.OnMacOS(""); err != nil || applies != No {
			return false
		}
	}
	return len(declarations) > 0
}

// dependencyChanges says what a manifest's declared dependencies gained,
// lost, and moved, one line each. What's gained holds, as another port the
// Portfile may need to declare.
func dependencyChanges(file string, before, after reading) []Change {
	spelled := func(version string, indirect bool) string {
		if indirect {
			return version + " (indirect)"
		}
		return version
	}
	var changes []Change
	for _, delta := range dependencyDeltas(before, after) {
		required := after.requirements[delta.name]
		switch delta.how {
		case "adds":
			// One that applies only elsewhere, as a Windows-only one, is
			// said and holds nothing; one that may apply here holds.
			changes = append(changes, Change{Kind: "dependency", Path: file, Hold: !elsewhere(required), Requirements: required,
				Message: strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, delta.name, delta.now))})
		case "drops":
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s drops %s", file, delta.name)})
		default:
			// One that applied only elsewhere and now may apply here, as
			// a Windows-only requirement made macOS's, is as good as added,
			// and holds as one (the helper-ownership review's finding 1).
			message := fmt.Sprintf("upstream: %s moves %s from %s to %s", file, delta.name, spelled(delta.old, delta.wasIndirect), spelled(delta.now, delta.isIndirect))
			arrives := required != nil && elsewhere(before.requirements[delta.name]) && !elsewhere(required)
			if arrives {
				message += ", which now may apply to macOS"
			}
			changes = append(changes, Change{Kind: "dependency", Path: file, Hold: arrives, Requirements: required, Message: message})
		}
	}
	// What holds the update for a look comes first.
	slices.SortStableFunc(changes, func(a, b Change) int {
		switch {
		case a.Hold == b.Hold:
			return 0
		case a.Hold:
			return -1
		}
		return 1
	})
	return changes
}

// dependencyCount says in one line how many of a proven manifest's
// declared dependencies it gained, lost, and moved, holding nothing (D9),
// naming them where there are few of a kind: "upstream: Cargo.toml: 1
// added (inferno), 1 moved (open)", and "upstream: go.mod: 2 added, 14
// moved" (the txt run's finding 6).
func dependencyCount(file string, before, after reading) []Change {
	names := map[string][]string{}
	for _, delta := range dependencyDeltas(before, after) {
		names[delta.how] = append(names[delta.how], delta.name)
	}
	var parts []string
	for _, part := range [][2]string{{"adds", "added"}, {"drops", "dropped"}, {"moves", "moved"}} {
		some := names[part[0]]
		switch {
		case len(some) == 0:
		case len(some) <= namedDependencies:
			parts = append(parts, fmt.Sprintf("%d %s (%s)", len(some), part[1], strings.Join(some, ", ")))
		default:
			parts = append(parts, fmt.Sprintf("%d %s", len(some), part[1]))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []Change{{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s: %s", file, strings.Join(parts, ", "))}}
}

// namedDependencies is how many of a kind the count names.
const namedDependencies = 3

// nativeLinks lists the crates new to a Cargo.lock that link a native
// library, as Cargo's -sys crates do. Such a crate often links a copy of
// the library it finds installed, and builds one it bundles otherwise,
// which a clean check can't tell apart: where MacPorts has the library,
// the Portfile may want to declare it. They're for the person's attention,
// and hold nothing (D9).
func nativeLinks(old file, hadOld bool, now file, hasNow bool) []Change {
	if !hasNow {
		return nil
	}
	unread := func(version string, err error) []Change {
		return []Change{{Kind: "unread", Path: cargoLock, Message: fmt.Sprintf("upstream's Cargo.lock couldn't be read in the %s version, so the crates new to it that link a native library weren't looked for: %v", version, err)}}
	}
	packages, err := dependency.ReadCargoLock(now.data)
	if err != nil {
		return unread("new", err)
	}
	had := map[string]bool{}
	if hadOld {
		earlier, err := dependency.ReadCargoLock(old.data)
		if err != nil {
			return unread("old", err)
		}
		for _, pkg := range earlier {
			had[pkg.Name] = true
		}
	}
	var changes []Change
	for _, pkg := range packages {
		library := pkg.NativeLibrary()
		if library == "" || had[pkg.Name] {
			continue
		}
		// Once, whichever versions the lock pins.
		had[pkg.Name] = true
		changes = append(changes, Change{Kind: "dependency", Path: cargoLock,
			Message: fmt.Sprintf("upstream: Cargo.lock adds %s %s, which links the native library %s: MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds", pkg.Name, pkg.Version, library)})
	}
	return changes
}
