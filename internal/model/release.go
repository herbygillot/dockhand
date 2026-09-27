package model

import "time"

// UpdateAction is what an update of a port does.
type UpdateAction string

const (
	// Bump updates a port's version and associated source metadata.
	Bump UpdateAction = "bump"
	// BumpRevision increments a port's MacPorts revision without changing
	// its version.
	BumpRevision UpdateAction = "bump-revision"
	// RefreshChecksums updates distfile checksums for the selected source.
	RefreshChecksums UpdateAction = "refresh-checksums"
)

// EditIntent is what a person asked of an update beyond the version: the
// choices that shape the commit. It is set once from the command line and
// travels unchanged to the editor, so an option is one field rather than
// one copied through every type on the way.
type EditIntent struct {
	// SharedRelease authorizes moving every subport that shares the
	// selected port's release source.
	SharedRelease bool `json:",omitempty"`
	// Stub names the port a person selected when the edit targets its
	// newest versioned subport instead: the branch and commit carry this
	// name, and the editor honors the redirect.
	Stub string `json:",omitempty"`
	// KeepOldChecksums refreshes a legacy checksum group's values in place,
	// md5 and sha1 included, instead of rewriting it as rmd160, sha256, and
	// size.
	KeepOldChecksums bool `json:",omitempty"`
}

// ReleaseSelection records how a release was chosen, as distinct from what
// it is. Requested is the spelling a person typed, empty for automatic
// selection; CurrentVersion is what the selection was compared against,
// and NoUpdate says the port is already current. Stability classifies the
// selected version and LeavesStable marks a move from a stable current
// version to a prerelease; an explicit version is honored either way, and
// these only inform reporting. Embedded in Release, so its shape is flat.
type ReleaseSelection struct {
	Requested      string
	CurrentVersion string `json:",omitempty"`
	NoUpdate       bool   `json:",omitempty"`
	Stability      string `json:",omitempty"`
	LeavesStable   bool   `json:",omitempty"`
}

// Release identifies the upstream source selected for a version bump: the
// frozen fact of forge, repository, tag, and commit, or an archive listing,
// together with the selection that produced it.
type Release struct {
	// Archive selects a Portfile version whose source is established by
	// evaluated download locations and prepared checksums, without a forge
	// tag.
	Archive bool            `json:",omitempty"`
	Listing *ReleaseListing `json:",omitempty"`
	ReleaseSelection
	Version string
	// SourceVersion is the version as the source spells it when the
	// Portfile derives its own from that spelling, such as a perl5 module
	// version that MacPorts normalizes into the port version; empty when
	// the two agree. It is what the version input is edited to; Version
	// is always the evaluated result.
	SourceVersion string `json:",omitempty"`
	Forge         string
	Instance      string
	Repository    string
	Tag           string
	Commit        string
	ObservedAt    time.Time
}

// ReleaseListing retains compact discovery evidence; source archives are
// bound separately by their evaluated locations and prepared checksums.
type ReleaseListing struct {
	URL          string
	ETag         string `json:",omitempty"`
	LastModified string `json:",omitempty"`
	SHA256       string
}

// SourceSpelling is the version the source names: SourceVersion when the
// Portfile derives its version from it, otherwise Version itself.
func (r Release) SourceSpelling() string {
	if r.SourceVersion != "" {
		return r.SourceVersion
	}
	return r.Version
}

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
	Target        Target
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
