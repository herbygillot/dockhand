package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
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
	// Author is the login of the pull request's author, for an adopted
	// one, whose Portfiles the trust rule names as theirs (Then's item
	// 12); empty for one adopted before it was kept.
	Author string `json:",omitempty"`
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
	State  PullRequestState
	Draft  bool `json:",omitempty"`
	Review PullRequestReview
	// ChangesRequestedBy are the reviewers whose latest review asks for
	// changes.
	ChangesRequestedBy []string `json:",omitempty"`
	// ReviewedAt is when the review that sets Review was made; zero where
	// none does, or for an observation from before it was kept.
	ReviewedAt time.Time `json:",omitzero"`
	// Failing names the checks that concluded unsuccessfully.
	Checks  PullRequestChecks
	Failing []string `json:",omitempty"`
	// Head is the commit the pull request was at.
	Head ObjectID
	At   time.Time
	// Conflicts is a pull request GitHub says doesn't merge cleanly into
	// master, which a rebase onto master settles; false where it merges
	// or GitHub hasn't computed it yet (the docs dig's I2).
	Conflicts bool `json:",omitempty"`
}

// CIFailure is one of a pull request's failed CI jobs, classified once
// from its log: the head it ran at, its run, attempt, and job, its check's
// name, and what made it fail, where a reading found it (schema 37).
type CIFailure struct {
	Branch BranchID
	Head   ObjectID
	Run    int64
	// Attempt is the run's attempt the job was; a re-run is another.
	Attempt int
	Job     int64
	Check   string
	Cause   CICause
	// File, URL, and Status are the archive a fetch failed, the last site
	// tried, and the HTTP status it answered (0 for none), for CIFetch;
	// Reason is the site's reason, or, for CIUnclassified, why the log
	// said nothing a reading knows.
	File, URL string
	Status    int
	Reason    string
	// Error is the log's first error line, read as a local log's is, where
	// the job wasn't a fetch's; Ports are each port the job built, as
	// MacPorts' workflow marked it, in name order (schema 44, Then's item
	// 7). Empty for a job read before them.
	Error string
	Ports []CIPort `json:",omitempty"`
	At    time.Time
}

// CIPort is one port a CI job built, and how: passed, or failed at a
// phase.
type CIPort struct {
	Port    string  `json:"port"`
	Outcome Outcome `json:"outcome"`
	Phase   Phase   `json:"phase,omitempty"`
}

// CICause is what made a CI job fail, as its log was read.
type CICause string

const (
	// CIFetch is an archive MacPorts' fetch couldn't get, which another
	// run, its mirrors answering, may get.
	CIFetch CICause = "fetch"
	// CIUnclassified is a failure no reading named: the log was gone,
	// withheld, or held nothing a reading knows.
	CIUnclassified CICause = "unclassified"
)

// Validate checks that a CI failure says which job, and what it was.
func (f CIFailure) Validate() error {
	switch {
	case f.Branch == "" || f.Head == "" || f.Run <= 0 || f.Job <= 0 || f.Attempt <= 0 || f.At.IsZero():
		return invalid("CI failure of job %d is incomplete", f.Job)
	case f.Cause != CIFetch && f.Cause != CIUnclassified:
		return invalid("CI failure of job %d has an unknown cause %q", f.Job, f.Cause)
	case f.Cause == CIFetch && f.File == "":
		return invalid("CI failure of job %d fetched no archive it names", f.Job)
	}
	return nil
}

// PullRequestState is a pull request's state as last observed.
type PullRequestState string

const (
	PullRequestOpen   PullRequestState = "open"
	PullRequestClosed PullRequestState = "closed"
	PullRequestMerged PullRequestState = "merged"
)

// PullRequestReview is what a pull request's reviews ask, as last
// observed: the latest review of each reviewer, changes requested by any
// of them deciding it.
type PullRequestReview string

const (
	ReviewNone             PullRequestReview = "none"
	ReviewApproved         PullRequestReview = "approved"
	ReviewChangesRequested PullRequestReview = "changes-requested"
)

// PullRequestChecks is how a pull request's checks stand at its head, as
// last observed: failing where any failed, pending where any hasn't
// finished, passing where all passed, and none where it has none.
type PullRequestChecks string

const (
	ChecksNone    PullRequestChecks = "none"
	ChecksPending PullRequestChecks = "pending"
	ChecksPassing PullRequestChecks = "passing"
	ChecksFailing PullRequestChecks = "failing"
)

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
	// Ports are the ports it was started for, by name: start --port's, or
	// the port an authoring command started it for. -p, status --port,
	// and its ports read them beside what it changes, so a branch started
	// for jq is jq's before it changes jq (the command-line UX second
	// look's §1).
	Ports []string
	// Closes are the Trac tickets update, ship, or bump's --closes named,
	// by number, which tidy writes as Closes: trailers with each one's URL
	// (the docs dig's I41).
	Closes []string
	// EndedAt is when it left open, merged, closed, or archived, which
	// cleanup times what it keeps of its checks from (D6); zero while it's
	// open. The store records the moment it leaves open where it's zero.
	EndedAt time.Time
	// CleanKept says what clean kept of the branch, as "fork branch kept:
	// moved after merge", which status --all says; empty where it kept
	// nothing, or hasn't cleaned it.
	CleanKept string
	// ForgottenAt is when clean --delete forgot the archived branch, which
	// nothing then reads as a branch: its record stays only for what
	// other branches' results name of its checks. Zero for one remembered.
	ForgottenAt time.Time
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
	for _, port := range b.Ports {
		if port == "" || strings.ContainsFunc(port, unicode.IsSpace) {
			return invalid("branch %s was started for an unusable port name %q", b.ID, port)
		}
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
