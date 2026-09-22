package macports

import "github.com/herbygillot/dockhand/internal/record"

// Runtime describes the installation observed by this evaluator session.
// SourceReviewed records historical inspection, not runtime certification.
// Startup checks do not certify individual PortGroups or fetch customizations.
type Runtime struct {
	// Platform is what the interpreter describes to a Portfile: the host's
	// own on a Mac, and a modeled macOS elsewhere.
	Platform record.Platform
	// Host is the platform MacPorts itself runs on when it differs from
	// Platform, and zero when the interpreter describes its own host.
	Host           record.Platform
	BaseVersion    string
	TclVersion     string
	SourceReviewed bool
}

// Modeled reports whether every context of this runtime is a model: MacPorts
// runs on a host that is not the platform it describes, so what a Portfile
// reads from the host is not what a Mac would answer, and nothing it builds
// would be evidence.
func (r Runtime) Modeled() bool { return r.Host != (record.Platform{}) }
