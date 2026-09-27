package macports

import "github.com/herbygillot/dockhand/internal/model"

// ReleaseScope records the complete effect of one source-bound version
// input: the ports its release moves, and those it must not.
type ReleaseScope struct {
	Input     ReleaseInput
	Affected  []ReleaseMember
	Protected []ReleaseMember
}

// ReleaseInput locates the literal whose native evaluation established the
// scope.
type ReleaseInput struct {
	Portfile      string
	Offset        int
	Before, After string
}

// ReleaseMember retains source identity as well as the evaluated package
// version.
type ReleaseMember struct {
	NeedsXcode    bool
	Target        model.Target
	Before, After ReleaseState
	MetadataOnly  bool
	// Follower marks an obsolete port replaced by the initiating target that
	// carries its version and builds nothing, such as the kubectl stub
	// replaced by kubectl-1.37. It has no source of its own to get wrong, so
	// it moves with the target without shared-release authorization.
	Follower bool `json:",omitempty"`
}

// NeedsAuthorization reports whether moving this member takes
// shared-release authorization: every member but the initiating target
// does, except an obsolete follower.
func (m ReleaseMember) NeedsAuthorization(initiating string) bool {
	return m.Target.Name != initiating && !m.Follower
}

// ReleaseState is the metadata preserved for scope checks after human
// edits.
type ReleaseState struct {
	MasterSites, Worksrcdir   string
	Epoch                     int
	Version                   string
	Revision                  int
	Tag, Distfiles, Checksums string
}
