package record

import "time"

// Selection records how a release was chosen, as distinct from what it is.
// Requested is the spelling a person typed, empty for automatic selection;
// CurrentVersion is what the selection was compared against, and NoUpdate
// says the port is already current. Stability classifies the selected
// version and LeavesStable marks a move from a stable current version to a
// prerelease; an explicit version is honored either way, and these only
// inform reporting. Embedded in Release, so the stored shape is flat.
type Selection struct {
	Requested      string
	CurrentVersion string `json:",omitempty"`
	NoUpdate       bool   `json:",omitempty"`
	Stability      string `json:",omitempty"`
	LeavesStable   bool   `json:",omitempty"`
}

// Release identifies the upstream source selected for a version bump: the
// frozen fact of forge, repository, tag, and commit, or an archive listing,
// together with the Selection that produced it.
type Release struct {
	// Archive selects a Portfile version whose source is established by
	// evaluated download locations and prepared checksums, without a forge tag.
	Archive bool            `json:",omitempty"`
	Listing *ReleaseListing `json:",omitempty"`
	Selection
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

// ReleaseListing retains compact discovery evidence; source archives are bound
// separately by their evaluated locations and prepared checksums.
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
