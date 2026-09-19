package record

import (
	"strings"
	"time"
)

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
	// InitiatingTarget is the named port used to continue this contribution.
	InitiatingTarget string `json:",omitempty"`
	ID               ChangeID
	Branch           string
	Targets          []Target
	// GeneratedCommit is the original fully generated commit, if known. Rewrites
	// retain this identity so a trailer cannot masquerade as generation evidence.
	GeneratedCommit ObjectID `json:",omitempty"`
	// CurrentRevision identifies the current local revision.
	CurrentRevision RevisionID
	// PublishedRevision identifies the last confirmed published revision.
	// It may differ from CurrentRevision or be empty before publication.
	PublishedRevision RevisionID
	PullRequestID     PullRequestID
	Disposition       Disposition
	// Cleanup is the housekeeping a merged contribution owes, recorded
	// with the merged disposition and settled afterwards; nil before the
	// merge and on contributions retired before it was recorded.
	Cleanup   *BranchCleanup `json:",omitempty"`
	CreatedAt time.Time
}

// Names reports whether name selects this contribution. The initiating port
// names it, and so does any target it carries: a shared release is initiated
// by its stub -- rb-mustache -- while the target that is prepared and built is
// a subport of it, rb33-mustache, and status names that subport when it tells
// a reader what to run next. Both spellings reach the same contribution.
func (c Change) Names(name string) bool {
	if name == "" {
		return false
	}
	if strings.EqualFold(c.InitiatingTarget, name) {
		return true
	}
	for _, target := range c.Targets {
		if strings.EqualFold(target.Name, name) {
			return true
		}
	}
	return false
}

// CleanupState is where one side of a merged contribution's cleanup stands.
type CleanupState string

const (
	// CleanupPending marks a deletion still owed: not yet attempted, or
	// attempted and to be retried after RetryAt.
	CleanupPending CleanupState = "pending"
	// CleanupComplete marks a branch deleted, or already gone.
	CleanupComplete CleanupState = "complete"
	// CleanupKept marks a branch deliberately left in place, for the reason
	// in Detail: it moved past the published commit, or no remote can reach it.
	CleanupKept CleanupState = "kept"
)

// BranchCleanup is what a merged contribution leaves behind: its local
// branch and the fork branch its pull request was published from. Both are
// deleted only while they still hold Published, so nothing unpublished is
// lost, and each side settles on its own, so a process exit or a failed
// remote call leaves an obligation the next cycle or refresh takes up
// rather than a branch nothing revisits.
type BranchCleanup struct {
	// Published is the commit both branches are expected to hold.
	Published ObjectID
	Local     CleanupOutcome
	Fork      CleanupOutcome
}

// CleanupOutcome is one side of a BranchCleanup.
type CleanupOutcome struct {
	// Name is the local branch, or the fork branch as owner/repo:branch;
	// empty when there was nothing to delete.
	Name  string `json:",omitempty"`
	State CleanupState
	// Detail says what happened on the last attempt.
	Detail string `json:",omitempty"`
	// ConsecutiveFailures and RetryAt are the retry bookkeeping a Lease
	// keeps: failures back the next attempt off, and nothing runs before
	// RetryAt except an explicit refresh.
	ConsecutiveFailures uint32     `json:",omitempty"`
	RetryAt             *time.Time `json:",omitempty"`
}

// Settled reports whether every side of the cleanup has a final outcome.
func (c *BranchCleanup) Settled() bool {
	return c != nil && c.Local.State != CleanupPending && c.Fork.State != CleanupPending
}

// Due reports whether a pending side may be attempted at now.
func (o CleanupOutcome) Due(now time.Time) bool {
	return o.State == CleanupPending && (o.RetryAt == nil || !o.RetryAt.After(now))
}
