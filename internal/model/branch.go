package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalid reports a record that breaks one of this package's rules.
var ErrInvalid = errors.New("invalid record")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// BranchID identifies a branch's record independently of its Git ref name,
// which a person may rename (Design v3 §15, point 8).
type BranchID string

// BranchState is where a branch is in its life.
type BranchState string

const (
	BranchOpen     BranchState = "open"
	BranchMerged   BranchState = "merged"
	BranchClosed   BranchState = "closed"
	BranchArchived BranchState = "archived"
)

// Valid reports whether the state is one this package defines.
func (s BranchState) Valid() bool {
	switch s {
	case BranchOpen, BranchMerged, BranchClosed, BranchArchived:
		return true
	}
	return false
}

// CanBecome reports whether a branch in state s may move to next. A merge is
// final. A closed pull request can be reopened, and archived work can be
// taken up again, so both return to open.
func (s BranchState) CanBecome(next BranchState) bool {
	switch s {
	case BranchOpen:
		return next == BranchMerged || next == BranchClosed || next == BranchArchived
	case BranchClosed, BranchArchived:
		return next == BranchOpen
	}
	return false
}

// PullRequest is the one pull request a branch became.
type PullRequest struct {
	// Repository is the base repository, such as macports/macports-ports.
	Repository string
	Number     int
	// Head is the head repository and branch, such as ada/macports-ports:dockhand/jq-4k2p.
	Head string
	// Pushed is the commit dockhand last pushed to the head branch. A push
	// that finds anything else there stops, since someone else pushed.
	Pushed ObjectID
	// Body is the description dockhand last wrote, so a person's edits to
	// it are recognized and kept.
	Body  string
	Draft bool
	// Adopted is someone else's pull request, adopted with adopt --pr: its
	// Portfile isn't the person's, so nothing evaluates it for a change
	// record until the trust rule is in code.
	Adopted bool
	// Observed is what the forge last reported about it; nil until then.
	Observed *PullRequestObservation `json:",omitempty"`
}

// HeadParts are the pull request's head repository and branch, as Head
// joins them; empty where Head names none.
func (p PullRequest) HeadParts() (repository, branch string) {
	repository, branch, _ = strings.Cut(p.Head, ":")
	return repository, branch
}

// PullRequestObservation is a pull request as the forge last reported it.
type PullRequestObservation struct {
	// State is open, closed, or merged.
	State string
	Draft bool `json:",omitempty"`
	// Review is approved, changes-requested, or none.
	Review string
	// ChangesRequestedBy are the reviewers whose latest review asks for
	// changes.
	ChangesRequestedBy []string `json:",omitempty"`
	// ReviewedAt is when the review that sets Review was made; zero where
	// none does, or for an observation from before it was kept.
	ReviewedAt time.Time `json:",omitzero"`
	// Checks is passing, failing, pending, or none; Failing names the
	// checks that concluded unsuccessfully.
	Checks  string
	Failing []string `json:",omitempty"`
	// Head is the commit the pull request was at.
	Head ObjectID
	At   time.Time
}

// Branch is the unit of work.
type Branch struct {
	ID         BranchID
	Repository RepositoryID
	// Name is the Git branch name, such as dockhand/jq-4k2p.
	Name string
	// Base is the master commit the branch started from, or was last rebased onto.
	Base ObjectID
	// Worktree is the directory the branch is checked out in: a managed sparse
	// worktree, or the person's own checkout when Managed is false.
	Worktree string
	Managed  bool
	// Title is kept from any step that set one; empty until then.
	Title       string
	State       BranchState
	PullRequest *PullRequest
	CreatedAt   time.Time
	// Origin is who started it: a person, or serve for your outdated
	// ports. serve submits only its own (Design v3 §11).
	Origin Origin
	// Note is a person's note for the pull request, which its description
	// gives under Description each time dockhand writes it (submit
	// --note); empty for none. dockhand writes the whole description, so
	// a person had no way to say in it why rust's tests failed (the rust
	// run, #35084). It is the branch's, not the pull request's, so a note
	// given before one opens is kept for it.
	Note string
	// EndedAt is when it left open, merged, closed, or archived, which
	// cleanup times what it keeps of its checks from (D6); zero while it's
	// open. The store records the moment it leaves open where it's zero.
	EndedAt time.Time
}

// Validate checks the rules every stored branch keeps.
func (b Branch) Validate() error {
	switch {
	case b.ID == "":
		return invalid("branch has no ID")
	case b.Repository == "":
		return invalid("branch %s has no repository", b.ID)
	case !validRefName(b.Name):
		return invalid("branch %s has an unusable name %q", b.ID, b.Name)
	case b.Base == "":
		return invalid("branch %s has no base", b.ID)
	case !b.State.Valid():
		return invalid("branch %s has unknown state %q", b.ID, b.State)
	case b.Managed && b.Worktree == "":
		return invalid("managed branch %s has no worktree", b.ID)
	case b.CreatedAt.IsZero():
		return invalid("branch %s has no creation time", b.ID)
	}
	if pr := b.PullRequest; pr != nil && (pr.Repository == "" || pr.Number <= 0 || pr.Head == "") {
		return invalid("branch %s has an incomplete pull request", b.ID)
	}
	return nil
}

// ShortName is the name people type: the Git name without dockhand's prefix.
func (b Branch) ShortName() string { return strings.TrimPrefix(b.Name, BranchPrefix) }

// BranchPrefix begins the name of every branch dockhand creates.
const BranchPrefix = "dockhand/"

// validRefName applies the parts of git check-ref-format that a branch name
// dockhand stores can break, as git.ValidBranchName does, which a test
// holds them to; Git remains the authority when it creates one. Each
// component is held to them: dockhand/a.lock/b passed here where Git
// refuses it (the architecture review's H8).
func validRefName(name string) bool {
	if name == "" || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "@{") || name == "@" {
		return false
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f || strings.ContainsRune(" ~^:?*[\\", c) {
			return false
		}
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
