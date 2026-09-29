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
}

// memberLimit is the most of one file the comparison reads.
const memberLimit = 1 << 20

var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|copyright|notice|unlicense)([._-].*)?$`)

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
func Compare(ctx context.Context, older, newer string) ([]Change, error) {
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
		case licenseName.MatchString(base):
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
	return changes, nil
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
		wanted := licenseName.MatchString(base) && depth <= 1 ||
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
		switch delta.how {
		case "adds":
			changes = append(changes, Change{Kind: "dependency", Path: file, Hold: true, Message: strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, delta.name, delta.now))})
		case "drops":
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s drops %s", file, delta.name)})
		default:
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s moves %s from %s to %s", file, delta.name, spelled(delta.old, delta.wasIndirect), spelled(delta.now, delta.isIndirect))})
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
// declared dependencies it gained, lost, and moved, "upstream: go.mod: 2
// added, 4 moved", holding nothing (D9).
func dependencyCount(file string, before, after reading) []Change {
	counts := map[string]int{}
	for _, delta := range dependencyDeltas(before, after) {
		counts[delta.how]++
	}
	var parts []string
	for _, part := range [][2]string{{"adds", "added"}, {"drops", "dropped"}, {"moves", "moved"}} {
		if n := counts[part[0]]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, part[1]))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []Change{{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s: %s", file, strings.Join(parts, ", "))}}
}

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
