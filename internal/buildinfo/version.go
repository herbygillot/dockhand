package buildinfo

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// ProjectURL is where dockhand lives.
const ProjectURL = "https://github.com/herbygillot/dockhand"

// Info is what the build knows about itself.
type Info struct {
	// Version is the module version, or "devel" when untagged.
	Version string
	// Revision is the VCS revision, shortened to twelve characters, if known.
	Revision string
	// FullRevision is the VCS revision as the toolchain recorded it, all of
	// it: what a forge is asked for, where Revision is what Tag and String
	// show of it.
	FullRevision string
	Modified     bool
}

// Version is the version a packager names at link time when no version
// control data is available, as when building from a release tarball:
//
//	go build -ldflags "-X github.com/herbygillot/dockhand/internal/buildinfo.Version=v0.9.0" ./cmd/dockhand
//
// A version the toolchain stamped from a tag wins over it, so a build from a
// checkout is never mislabeled by a stale build variable.
var Version string

// Current reads the embedded build information.
func Current() Info {
	info, ok := debug.ReadBuildInfo()
	return current(info, ok, Version)
}

func current(info *debug.BuildInfo, ok bool, named string) Info {
	current := Info{Version: "unknown"}
	if ok {
		current.Version = info.Main.Version
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				current.Revision, current.FullRevision = setting.Value, setting.Value
			case "vcs.modified":
				current.Modified = setting.Value == "true"
			}
		}
	}
	if len(current.Revision) > 12 {
		current.Revision = current.Revision[:12]
	}
	if current.Version == "" || current.Version == "(devel)" {
		current.Version = "devel"
	}
	if named = strings.TrimSpace(named); named != "" && (current.Version == "devel" || current.Version == "unknown") {
		if !strings.HasPrefix(named, "v") {
			named = "v" + named
		}
		current.Version = named
	}
	return current
}

// String is the form `dockhand --version` prints: "v0.3.0 (1a2b3c4d5e6f)"
// or "devel (1a2b3c4d5e6f, modified)". A version that already ends in its own
// revision, which a pseudo-version does, does not repeat it.
func (i Info) String() string {
	text := i.Version
	switch {
	case i.Revision != "" && !namesRevision(i.Version, i.Revision):
		text += " (" + i.Revision
		if i.Modified {
			text += ", modified"
		}
		text += ")"
	case i.Modified:
		text += " (modified)"
	}
	return strings.TrimSpace(text)
}

// namesRevision reports whether a version ends with this revision. A Go
// pseudo-version does: its last hyphenated component is the short commit,
// after any +dirty the toolchain appends for a modified tree.
func namesRevision(version, revision string) bool {
	base, _, _ := strings.Cut(version, "+")
	index := strings.LastIndex(base, "-")
	return index >= 0 && base[index+1:] == revision
}

// TagModified reports whether a build's tag, as Tag gives it and a
// Generated-By trailer names it, is of uncommitted source: the "+dirty"
// the toolchain appends to a pseudo-version, or an untagged build's
// ".modified". Nobody else can find such a build.
func TagModified(tag string) bool {
	return strings.Contains(tag, "+dirty") || strings.HasSuffix(tag, ".modified")
}

// Tag is the one-token form for trailers and user agents: the module
// version when tagged, otherwise "devel+<revision>", with ".modified" when
// the tree had uncommitted changes, so a commit names the exact code that
// made it.
func (i Info) Tag() string {
	if i.Version != "devel" && i.Version != "unknown" {
		return i.Version
	}
	if i.Revision == "" {
		return i.Version
	}
	tag := i.Version + "+" + i.Revision
	if i.Modified {
		tag += ".modified"
	}
	return tag
}

// Source is what a build's tag, as Tag gives it and a Generated-By trailer
// names it, gives anybody to find the build's source by in the project's
// repository: the commit it was built from, or the tag of a release.
// Neither, where the build recorded no revision (devel or unknown), or
// was of uncommitted source, which nothing finds.
type Source struct {
	// Commit is the commit, in full where the tag is this build's own and
	// the toolchain recorded it, else the twelve characters a tag
	// abbreviates it to.
	Commit string
	// Release is a tagged build's tag, where no commit is known.
	Release string
}

// SourceOf is what a build's tag gives anybody to find its source by.
func SourceOf(tag string) Source {
	return sourceOf(tag, Current())
}

func sourceOf(tag string, running Info) Source {
	if TagModified(tag) {
		return Source{}
	}
	var source Source
	switch rest, devel := strings.CutPrefix(tag, "devel+"); {
	case devel:
		source.Commit = rest
	case module.IsPseudoVersion(tag):
		source.Commit, _ = module.PseudoVersionRev(tag)
	case tag != "devel" && tag != "unknown" && tag != "":
		source.Release = tag
	}
	// This build's own tag names the commit it was built from in full,
	// which needs no forge to expand it: a tag's twelve characters are a
	// prefix, and a release tag can move.
	if tag == running.Tag() && running.FullRevision != "" && strings.HasPrefix(running.FullRevision, source.Commit) {
		return Source{Commit: running.FullRevision}
	}
	return source
}

// Released is the release a build's version names, as String gives it,
// "v0.4.0" of "v0.4.0 (5897a828e1c2)": a tag with no pre-release or build
// part, as the port's builds are. False for an rc, a pseudo-version, a
// modified build, devel, or unknown.
func Released(version string) (string, bool) {
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return "", false
	}
	tag := fields[0]
	if !semver.IsValid(tag) || semver.Prerelease(tag) != "" || semver.Build(tag) != "" || strings.Contains(version, "modified") {
		return "", false
	}
	return tag, true
}

// Way is how to reach the build a version names, as String gives it: a
// release's is the port's upgrade, a pseudo-version's or a devel build's
// its commit; empty where none can be found.
func Way(version string) string {
	if release, ok := Released(version); ok {
		return "dockhand " + release + " comes with the port's upgrade, sudo port upgrade dockhand"
	}
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return ""
	}
	tag := fields[0]
	if module.IsPseudoVersion(tag) {
		if commit, err := module.PseudoVersionRev(tag); err == nil {
			return "it was built from commit " + commit + " of " + ProjectURL
		}
	}
	if len(fields) > 1 {
		if commit := strings.Trim(strings.Split(fields[1], ",")[0], "()"); commit != "" {
			return "it was built from commit " + commit + " of " + ProjectURL
		}
	}
	return ""
}
