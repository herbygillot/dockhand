package macports

import "github.com/herbygillot/dockhand/internal/record"

// Runtime describes the installation observed by this evaluator session.
// SourceReviewed records historical inspection, not runtime certification.
// Startup checks do not certify individual PortGroups or fetch customizations.
type Runtime struct {
	Platform       record.Platform
	BaseVersion    string
	TclVersion     string
	SourceReviewed bool
}
