package record

import (
	"time"

	"github.com/herbygillot/dockhand/internal/model"
)

// The shared vocabulary lives in model; these aliases keep record's callers
// compiling until each moves to model directly.
type (
	Source   = model.Source
	Target   = model.Target
	Platform = model.Platform
)

// CompareTargets orders targets by their complete selection.
func CompareTargets(a, b Target) int { return model.CompareTargets(a, b) }

// Revision binds a change to an immutable Source. Later edits or rebases produce
// another revision while earlier jobs and evidence retain their original inputs.
type Revision struct {
	Scope    *ReleaseScope `json:",omitempty"`
	ID       RevisionID
	ChangeID ChangeID
	// Previous links to the preceding revision, or is empty for the first.
	Previous  RevisionID
	Source    Source
	CreatedAt time.Time
	// Shared lists the files under _resources this revision changes beside
	// its port, each with the Portfiles and PortGroups that load it, so
	// what verification of the one port does not cover is on record.
	Shared []SharedFile `json:",omitempty"`
}

// SharedFile is a file under _resources a contribution changes, and the
// paths that load it at the contribution's tree: Portfiles for the ports
// and group files for the PortGroups. A file that is not a PortGroup, a
// livecheck or fetch definition for instance, is read by every port's
// evaluation and lists no loaders.
type SharedFile struct {
	Path    string
	Loaders []string `json:",omitempty"`
}

// Artifact identifies externally stored build output, input, or logs.
// The state store retains this reference rather than the artifact's contents.
type Artifact struct {
	// Name identifies the artifact within its producer's outputs.
	Name string
	// Digest identifies the artifact's content for verification and reuse.
	Digest string
	// Location is the provider-supplied address used to retrieve the content.
	Location  string
	MediaType string
}

// Checkout describes where a captured verification tree came from. Head is
// provenance, not a claim that its commit contains the captured edits.
type Checkout struct {
	Branch        string
	Head          ObjectID
	ModifiedFiles int
}
