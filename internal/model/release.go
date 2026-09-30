package model

import (
	"strings"
	"time"
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

// Names reports whether a Git fetch of a source fetches this release: its
// git.branch is the release's tag, or its commit where the Portfile pins
// one, and its git.url is the release's repository on its forge, as a
// forge PortGroup writes it, the repository's address with ".git" after
// it, whose case the forge doesn't read. An archive's release, or one
// found without a repository, names no Git source.
func (r Release) Names(source GitSource) bool {
	if r.Archive || r.Instance == "" || r.Repository == "" || source.Ref == "" {
		return false
	}
	if source.Ref != r.Tag && source.Ref != r.Commit {
		return false
	}
	address := strings.TrimSuffix(strings.TrimRight(source.URL, "/"), ".git")
	return strings.EqualFold(address, strings.TrimRight(r.Instance, "/")+"/"+r.Repository)
}

// SourceSpelling is the version the source names: SourceVersion when the
// Portfile derives its version from it, otherwise Version itself.
func (r Release) SourceSpelling() string {
	if r.SourceVersion != "" {
		return r.SourceVersion
	}
	return r.Version
}
