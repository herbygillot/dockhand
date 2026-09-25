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
	Results(execution model.ExecutionID) ([]model.TargetResult, error)

	Session(id model.SessionID) (model.Session, error)
	// Sessions lists sessions that have not ended.
	Sessions() ([]model.Session, error)
	Lease(resource string) (model.Lease, error)
	// Events lists events after a sequence number, oldest first.
	Events(after int64, limit int) ([]model.Event, error)

	// Edits lists a branch's authoring records, oldest first.
	Edits(branch model.BranchID) ([]model.Edit, error)
	Checkpoint(number int) (model.Checkpoint, error)
	// Checkpoints lists a branch's checkpoints, oldest first.
	Checkpoints(branch model.BranchID) ([]model.Checkpoint, error)
	// Acceptances lists what was accepted for one commit of a branch.
	Acceptances(branch model.BranchID, commit model.ObjectID) ([]model.Acceptance, error)
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
	// complete verdict (model.TargetResult.ReplacedBy).
	RecordResult(result model.TargetResult) error

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

	AddEdit(edit model.Edit) error
	// AddCheckpoint records a checkpoint; its Number must be
	// NextCheckpointNumber's.
	AddCheckpoint(checkpoint model.Checkpoint) error
	NextCheckpointNumber() (int, error)
	// MarkRestored records a checkpoint's restore, once.
	MarkRestored(checkpoint model.Checkpoint) error
	// AddAcceptance records an acceptance; repeating one changes nothing.
	AddAcceptance(acceptance model.Acceptance) error
}

// NewID returns a random identifier with a readable prefix, such as
// br_k3z9…, for records whose identity must not depend on their names.
func NewID(prefix string) string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
