package record

import "time"

// Release identifies the upstream source selected for a version bump.
// The forge instance, repository, requested spelling, Portfile version, tag,
// and resolved commit remain distinct.
type Release struct {
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
