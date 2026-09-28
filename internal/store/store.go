// Package store is the contract for v3's durable records (docs/design-v3.md
// §3 and §11): branches, revisions, plans, runs, guest executions, target
// results, and the sessions, leases, and events that coordinate processes.
//
// A transaction is bound to one registered repository. Its callback runs
// once, synchronously, changes only records, and never calls a provider,
// the forge, Git, or the network; external work happens between
// transactions. Methods are specific to their records; there is no generic
// key-value or whole-database access. The rules each record keeps are
// model's; the store checks them again before writing, along with the rules
// that need other rows: a state change a record may make, a checkpoint that
// may replace another, a lease generation that is still current.
package store

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
)

var (
	// ErrNotFound reports a record that does not exist in the repository.
	ErrNotFound = errors.New("not found")
	// ErrConflict reports a write that another row forbids: a duplicate, a
	// state change the record may not make, a checkpoint that is final.
	ErrConflict = errors.New("conflict")
	// ErrStale reports a lease generation that is no longer current: the
	// writer lost its claim and must not act on it.
	ErrStale = errors.New("stale lease")
	// ErrSchema reports a database this build cannot use.
	ErrSchema = errors.New("unusable database")
	// ErrUnavailable reports storage that could not be read or written.
	ErrUnavailable = errors.New("storage unavailable")
	// ErrUncertain reports a commit whose outcome is unknown.
	ErrUncertain = errors.New("commit outcome unknown")
)

// Store opens transactions on one database.
type Store interface {
	// Register records the repository whose Git common directory is given,
	// or returns the one already registered for it.
	Register(ctx context.Context, commonDir string) (model.RepositoryID, error)
	View(ctx context.Context, repository model.RepositoryID, fn func(Reader) error) error
	Update(ctx context.Context, repository model.RepositoryID, fn func(Tx) error) error
	Close() error
}

// BranchFilter selects branches; empty fields select all.
type BranchFilter struct {
	States []model.BranchState
}

// RunFilter selects runs, newest first; empty fields select all.
type RunFilter struct {
	Branch model.BranchID
	States []model.RunState
	Limit  int
}

// Reader reads records within a transaction.
type Reader interface {
	Branch(id model.BranchID) (model.Branch, error)
	// BranchNamed finds the branch with a Git name that is not merged.
	BranchNamed(name string) (model.Branch, error)
	Branches(filter BranchFilter) ([]model.Branch, error)

	Revision(id model.RevisionID) (model.Revision, error)
	// Revisions lists a branch's revisions, oldest first.
	Revisions(branch model.BranchID) ([]model.Revision, error)

	Plan(id model.PlanID) (model.Plan, error)

	Run(id model.RunID) (model.Run, error)
	RunNumbered(number int) (model.Run, error)
	Runs(filter RunFilter) ([]model.Run, error)
	Executions(run model.RunID) ([]model.GuestExecution, error)
	// Execution reads one provider run by its ID.
	Execution(id model.ExecutionID) (model.GuestExecution, error)
	// ExecutionsReferred are the provider runs whose provider knows them by
	// ref, such as a workflow run's URL, oldest first.
	ExecutionsReferred(ref string) ([]model.GuestExecution, error)
	Results(execution model.ExecutionID) ([]model.TargetResult, error)
	// Inputs reads what a build read by its key (model.TargetInputs.Key);
	// ErrNotFound when none was recorded.
	Inputs(key string) (model.TargetInputs, error)
	// Archive is a kept archive, by its digest; ErrNotFound when it isn't
	// kept (decision 28).
	Archive(digest string) (model.Archive, error)
	// Archives are every kept archive, by digest.
	Archives() ([]model.Archive, error)
	// Reusable are a target's passed results in an environment that keep
	// what their builds read, from the builds themselves rather than a
	// reuse of them, newest first, at most limit (decision 28).
	Reusable(target model.TargetID, environment model.Environment, limit int) ([]model.TargetResult, error)

	Session(id model.SessionID) (model.Session, error)
	Lease(resource string) (model.Lease, error)
	// Events lists events after a sequence number, oldest first.
	Events(after int64, limit int) ([]model.Event, error)
	// RunEvents are one run's events after a sequence, oldest first, at
	// most limit; LastEvent is the journal's newest sequence, 0 when it has
	// none.
	RunEvents(run model.RunID, after int64, limit int) ([]model.Event, error)
	LastEvent() (int64, error)
	// CountEvents counts the events of a kind journaled at or after a time.
	CountEvents(kind string, since time.Time) (int, error)

	// Edits lists a branch's authoring records, oldest first.
	Edits(branch model.BranchID) ([]model.Edit, error)
	Checkpoint(number int) (model.Checkpoint, error)
	// Checkpoints lists a branch's checkpoints, oldest first.
	Checkpoints(branch model.BranchID) ([]model.Checkpoint, error)
	// Acceptances lists what was accepted for one commit of a branch.
	Acceptances(branch model.BranchID, commit model.ObjectID) ([]model.Acceptance, error)
	// LastReview is the newest review of a pull request; ErrNotFound when
	// there is none.
	LastReview(repository string, number int) (model.Review, error)
}

// Tx reads and writes records within a transaction.
type Tx interface {
	Reader

	// AddBranch records a new branch.
	AddBranch(branch model.Branch) error
	// UpdateBranch replaces a branch's fields; a state change must be one
	// model.BranchState allows.
	UpdateBranch(branch model.Branch) error

	// AddRevision records a revision. A snapshot's number must be the
	// branch's next one; NextSnapshot says which that is.
	AddRevision(revision model.Revision) error
	NextSnapshot(branch model.BranchID) (int, error)

	AddPlan(plan model.Plan) error

	// AddRun records a queued run; its Number must be NextRunNumber's.
	AddRun(run model.Run) error
	NextRunNumber() (int, error)
	UpdateRun(run model.Run) error

	AddExecution(execution model.GuestExecution) error
	UpdateExecution(execution model.GuestExecution) error
	// RecordResult writes a target's checkpoint, refusing to replace a
	// complete verdict (model.TargetResult.ReplacedBy). Inputs it names
	// must be recorded first.
	RecordResult(result model.TargetResult) error
	// RecordInputs keeps what a build read, once for every build that
	// read the same, and returns its key.
	RecordInputs(inputs model.TargetInputs) (string, error)
	// KeepArchive records an archive as kept, once its file is whole and
	// checked (decision 44). Keeping one already kept changes nothing.
	KeepArchive(archive model.Archive) error
	// PruneArchives forgets the archives kept before a time that no live
	// result names: none in a check of an open branch, and none recorded
	// at or after it (decisions 36 and 44). It returns what it forgot,
	// whose files go next.
	PruneArchives(before time.Time) ([]model.Archive, error)

	AddSession(session model.Session) error
	UpdateSession(session model.Session) error

	// AcquireLease gives the resource to the session under a new
	// generation, whoever held it; callers decide whether the holder may be
	// displaced before calling it.
	AcquireLease(resource string, session model.SessionID) (model.Lease, error)
	// ReleaseLease frees the resource if the lease is still current.
	ReleaseLease(lease model.Lease) error
	// CheckLease reports ErrStale unless the lease is still current: the
	// fence every write made under a claim passes first.
	CheckLease(lease model.Lease) error

	// AppendEvent adds an event to the journal and returns its sequence.
	AppendEvent(event model.Event) (int64, error)
	// PruneJournal removes the events recorded before a time, and the
	// sessions that ended, or last showed a heartbeat, before it, but for
	// one a lease still names; it says how many of each.
	PruneJournal(before time.Time) (events, sessions int, err error)

	AddEdit(edit model.Edit) error
	// AddCheckpoint records a checkpoint; its Number must be
	// NextCheckpointNumber's.
	AddCheckpoint(checkpoint model.Checkpoint) error
	NextCheckpointNumber() (int, error)
	// SettleCheckpoint records a prepared checkpoint as applied or
	// abandoned, once.
	SettleCheckpoint(checkpoint model.Checkpoint) error
	// MarkRestored records an applied checkpoint's restore, once.
	MarkRestored(checkpoint model.Checkpoint) error
	// AddAcceptance records an acceptance; repeating one changes nothing.
	AddAcceptance(acceptance model.Acceptance) error
	// AddReview records a review of a pull request.
	AddReview(review model.Review) error
}

// NewID returns a random identifier with a readable prefix, such as
// br_k3z9…, for records whose identity must not depend on their names.
func NewID(prefix string) string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
