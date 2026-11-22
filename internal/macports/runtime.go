package macports

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Runtime describes the installation observed by this evaluator session.
// Startup checks do not certify individual PortGroups or fetch customizations.
type Runtime struct {
	// Platform is what the interpreter describes to a Portfile: the host's
	// own on a Mac, and a modeled macOS elsewhere.
	Platform model.Platform
	// Host is the platform MacPorts itself runs on when it differs from
	// Platform, and zero when the interpreter describes its own host.
	Host        model.Platform
	BaseVersion string
	TclVersion  string
	// DarwinVersion is the kernel's full version, 25.1.0, and Xcode and
	// Tools are xcodeversion and xcodecltversion, as Base had read them at
	// the session's start: ToolsNotRead where it hadn't, ToolsNone where it
	// read nothing, and empty, which a scope takes as unknown, where
	// neither can be told (#194).
	DarwinVersion, Xcode, Tools string
}

// Provenance is the runtime as a record keeps it (model.EvaluatedUnder).
func (r Runtime) Provenance() model.EvaluatedUnder {
	return model.EvaluatedUnder{BaseVersion: r.BaseVersion, TclVersion: r.TclVersion, Platform: r.Platform, Host: r.Host, DarwinVersion: r.DarwinVersion, Xcode: r.Xcode, Tools: r.Tools}
}

// ToolsNotRead is a developer tool's version Base hasn't read, which a
// Portfile that never asks leaves so: a known value, equal to itself.
// ToolsNone is one Base read and found empty, as xcodeversion is with no
// Xcode. A part neither read nor pending is empty, and unknown (#194).
const (
	ToolsNotRead = "(not read)"
	ToolsNone    = "(none)"
)

// PatchScope is the scope a port's patch list was read in: the runtime's
// provenance, the subport, and the variants, each part Base or the caller
// named; a part it didn't name is unknown, so the list compares with none
// (model.ScopesComparable, #194). No variants is "-", a known value.
func (r Runtime) PatchScope(subport string, variants []string) string {
	selected := "-"
	if len(variants) > 0 {
		selected = strings.Join(variants, " ")
	}
	platform := func(p model.Platform) string {
		if p == (model.Platform{}) {
			return "-"
		}
		return p.OS + " " + p.Version + " " + p.Architecture
	}
	return model.PatchScope(r.BaseVersion, r.TclVersion, platform(r.Platform), platform(r.Host), r.DarwinVersion, model.ScopeTools(r.Xcode, r.Tools), subport, selected)
}

// Modeled reports whether every context of this runtime is a model: MacPorts
// runs on a host that is not the platform it describes, so what a Portfile
// reads from the host is not what a Mac would answer, and nothing it builds
// would be evidence.
func (r Runtime) Modeled() bool { return r.Host != (model.Platform{}) }
