package record

import "time"

// Disposition describes whether a contribution is still being pursued.
// It is independent of the outcomes of individual jobs.
type Disposition string

const (
	// ChangeOpen marks a contribution that can receive further work.
	ChangeOpen Disposition = "open"
	// ChangeMerged marks a contribution incorporated upstream.
	ChangeMerged Disposition = "merged"
	// ChangeClosed marks a contribution closed without being merged.
	ChangeClosed Disposition = "closed"
	// ChangeAbandoned marks a contribution the user is no longer pursuing.
	ChangeAbandoned Disposition = "abandoned"
)

// Change tracks one contribution across revisions, jobs, and pull-request updates.
// Its identity survives branch renames and commit rewrites.
type Change struct {
	ID      ChangeID
	Branch  string
	Targets []Target
	// CurrentRevision identifies the current local revision.
	CurrentRevision RevisionID
	// PublishedRevision identifies the last confirmed published revision.
	// It may differ from CurrentRevision or be empty before publication.
	PublishedRevision RevisionID
	PullRequestID     PullRequestID
	Disposition       Disposition
	CreatedAt         time.Time
}
