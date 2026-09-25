package model

import "time"

// RevisionID identifies one immutable candidate source of a branch.
type RevisionID string

// RevisionKind says whether a revision is committed source or captured
// working files.
type RevisionKind string

const (
	// RevisionCommit is a commit on the branch.
	RevisionCommit RevisionKind = "commit"
	// RevisionSnapshot is the working files captured by a check, which has a
	// tree but no commit (Design v3 §7).
	RevisionSnapshot RevisionKind = "snapshot"
)

// Revision is an immutable candidate: a source tree and its base. The changed
// scope derives from it, never from the branch's commits.
type Revision struct {
	ID     RevisionID
	Branch BranchID
	Kind   RevisionKind
	// Snapshot numbers a branch's snapshots from 1; zero for a commit.
	Snapshot int
	Source   Source
	// Head is the commit checked out when a snapshot was captured. It is
	// provenance, not a claim that the commit contains the captured edits.
	Head      ObjectID
	CreatedAt time.Time
}

// Validate checks the rules every stored revision keeps.
func (r Revision) Validate() error {
	switch {
	case r.ID == "" || r.Branch == "":
		return invalid("revision %q has no ID or branch", r.ID)
	case r.Source.Tree == "":
		return invalid("revision %s has no tree", r.ID)
	case r.Source.Base == "":
		return invalid("revision %s has no base", r.ID)
	case r.CreatedAt.IsZero():
		return invalid("revision %s has no creation time", r.ID)
	}
	switch r.Kind {
	case RevisionCommit:
		if r.Source.Commit == "" || r.Snapshot != 0 {
			return invalid("commit revision %s needs a commit and no snapshot number", r.ID)
		}
	case RevisionSnapshot:
		if r.Source.Commit != "" || r.Snapshot <= 0 {
			return invalid("snapshot revision %s needs a snapshot number and no commit", r.ID)
		}
	default:
		return invalid("revision %s has unknown kind %q", r.ID, r.Kind)
	}
	return nil
}

// SameTree reports whether two revisions hold identical files, which is
// what lets a result for a snapshot apply to the commit tidy makes of it.
func (r Revision) SameTree(other Revision) bool {
	return r.Source.Tree != "" && r.Source.Tree == other.Source.Tree && r.Source.Base == other.Source.Base
}
