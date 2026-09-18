// Package version describes the running dockhand build from the information
// the Go toolchain embeds: the module version when built from a tagged
// module, otherwise the VCS revision, marked when the tree was modified.
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

// Current reads the embedded build information.
func Current() Info {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{Version: "unknown"}
	}
	current := Info{Version: info.Main.Version}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			current.Revision = setting.Value
		case "vcs.modified":
			current.Modified = setting.Value == "true"
		}
	}
	if len(current.Revision) > 12 {
		current.Revision = current.Revision[:12]
	}
	if current.Version == "" || current.Version == "(devel)" {
		current.Version = "devel"
	}
	return current
}

// String is the form `dockhand --version` prints: "v0.3.0 (1a2b3c4d5e6f)"
// or "devel (1a2b3c4d5e6f, modified)".
func (i Info) String() string {
	text := i.Version
	if i.Revision != "" {
		text += " (" + i.Revision
		if i.Modified {
			text += ", modified"
		}
		text += ")"
	}
	return strings.TrimSpace(text)
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
