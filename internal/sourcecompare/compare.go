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
			changes = append(changes, Change{Kind: "unread", Path: name, Hold: true,
				Message: fmt.Sprintf("upstream's %s is larger than the %d KiB the comparison reads, so it wasn't compared", name, memberLimit>>10)})
			continue
		}
		if hadOld && hasNow && bytes.Equal(old.data, now.data) {
			continue
		}
		base := path.Base(name)
		switch {
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
			depth == 0 && (slices.Contains(buildNames, base) || manifests[base] != nil)
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
		return Change{Kind: "unread", Path: name, Hold: true, Message: fmt.Sprintf("upstream's %s %s", name, what)}
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
	// What it couldn't follow holds, so it comes first.
	var changes []Change
	for _, gap := range readings[1].unread {
		changes = append(changes, unread(gap+", which the comparison doesn't follow"))
	}
	return append(changes, dependencyChanges(name, readings[0].dependencies, readings[1].dependencies)...)
}

func dependencyChanges(file string, before, after map[string]string) []Change {
	var names []string
	for name := range after {
		names = append(names, name)
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []Change
	for _, name := range names {
		old, had := before[name]
		now, has := after[name]
		switch {
		case !had:
			changes = append(changes, Change{Kind: "dependency", Path: file, Hold: true, Message: strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, name, now))})
		case !has:
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s drops %s", file, name)})
		case old != now:
			changes = append(changes, Change{Kind: "dependency", Path: file, Message: fmt.Sprintf("upstream: %s moves %s from %s to %s", file, name, old, now)})
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
