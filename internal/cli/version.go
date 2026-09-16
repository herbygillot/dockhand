package cli

import (
	"runtime/debug"
	"strings"
)

// buildVersion describes the executable from its embedded build information:
// the module version when built from a tagged module, otherwise the VCS
// revision, marked when the tree was modified.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	version := info.Main.Version
	var revision string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	if revision != "" {
		version += " (" + revision
		if modified {
			version += ", modified"
		}
		version += ")"
	}
	return strings.TrimSpace(version)
}
