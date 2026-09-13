package record

import "time"

// Release identifies the upstream source selected for one explicit version bump.
// The requested spelling, Portfile version, tag, and resolved commit remain distinct.
type Release struct {
	Requested  string
	Version    string
	Repository string
	Tag        string
	Commit     string
	ObservedAt time.Time
}
