package version

import (
	"runtime/debug"
	"strings"
)

// ProjectURL is where dockhand lives.
const ProjectURL = "https://github.com/herbygillot/dockhand"

// Info is what the build knows about itself.
type Info struct {
	// Version is the module version, or "devel" when untagged.
	Version string
	// Revision is the VCS revision, shortened to twelve characters, if known.
	Revision string
	Modified bool
}

// Version is the version a packager names at link time when no version
// control data is available, as when building from a release tarball:
//
//	go build -ldflags "-X github.com/herbygillot/dockhand/internal/version.Version=v0.9.0" ./cmd/dockhand
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
				current.Revision = setting.Value
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
