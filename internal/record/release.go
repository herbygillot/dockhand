package record

import "time"

// Release identifies the upstream source selected for a version bump.
// The forge instance, repository, requested spelling, Portfile version, tag,
// and resolved commit remain distinct.
type Release struct {
	// Archive selects a Portfile version whose source is established by
	// evaluated download locations and prepared checksums, without a forge tag.
	Archive bool            `json:",omitempty"`
	Listing *ReleaseListing `json:",omitempty"`
	// Automatic selections retain the evaluated version and whether an update is needed.
	// Requested is empty for those jobs; explicit selections never set NoUpdate.
	CurrentVersion string `json:",omitempty"`
	NoUpdate       bool   `json:",omitempty"`
	Requested      string
	Version        string
	Forge          string
	Instance       string
	Repository     string
	Tag            string
	Commit         string
	ObservedAt     time.Time
}

// ReleaseListing retains compact discovery evidence; source archives are bound
// separately by their evaluated locations and prepared checksums.
type ReleaseListing struct {
	URL          string
	ETag         string `json:",omitempty"`
	LastModified string `json:",omitempty"`
	SHA256       string
}
