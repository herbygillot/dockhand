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
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

var (
	// ErrNotFound reports a record that does not exist in the repository.
	ErrNotFound = errors.New("not found")
	// ErrConflict reports a write that another row forbids: a duplicate, a
	// state change the record may not make, a checkpoint that is final.
	// It's the store keeping its own contract, which dockhand's code keeps
	// first: no caller recovers from one, and it says a rule was broken,
	// not that another try may succeed (the code-organization review's
	// finding 41).
	ErrConflict = errors.New("conflict")
	// ErrStale reports a lease generation that is no longer current: the
	// writer lost its claim and must not act on it.
	ErrStale = errors.New("stale lease")
	// ErrSchema reports a database this build cannot use.
	ErrSchema = errors.New("unusable database")
	// ErrUnavailable reports storage that could not be read or written,
	// for any reason the driver gives but a conflict: the disk, the file,
	// or a database still locked once the busy timeout has queued the
	// writer behind another. No caller tells one cause from another; each
	// fails what it was doing, and says why.
	ErrUnavailable = errors.New("storage unavailable")
	// ErrLiveUnderMigration refuses a migration while dockhand processes
	// that read the schema it would replace are working: after it, none
	// of them could record what it does (LiveUnderMigrationError).
	ErrLiveUnderMigration error = refusal.New(refusal.Busy, "dockhand processes are working on the database's schema")
	// ErrReleaseUnderMigration refuses a development build's migration of
	// a database a release last opened, which a person answers.
	ErrReleaseUnderMigration error = refusal.New(refusal.Attention, "a release last opened the database")
	// ErrUncertain reports a commit whose outcome is unknown: the write
	// may have landed or not, as when a cancel arrives while the commit
	// syncs. A writer that would undo work outside the store on a failed
	// write reads the record back first, and never undoes on this alone.
	ErrUncertain = errors.New("commit outcome unknown")
)

// Recorded runs a write, and where its commit's outcome is unknown
// (ErrUncertain), reads it back: witness reports whether what it wrote is
// there, which then answers for it. A write that landed is no failure; one
// that didn't, and any other error, is returned. The witness reads outside
// the caller's cancel, which is what can leave the outcome unknown. A
// writer that would undo, or leave unrecorded, work done outside the store
// writes through it (the code-organization review's finding 24).
func Recorded(ctx context.Context, s Store, repository model.RepositoryID, write func(Tx) error, witness func(Reader) bool) error {
	err := s.Update(ctx, repository, write)
	if !errors.Is(err, ErrUncertain) {
		return err
	}
	landed := false
	_ = s.View(context.WithoutCancel(ctx), repository, func(r Reader) error {
		landed = witness(r)
		return nil
	})
	if landed {
		return nil
	}
	return err
}

// Store opens transactions on one database.
type Store interface {
	// Register records the repository whose Git common directory is given,
	// or returns the one already registered for it.
	Register(ctx context.Context, commonDir string) (model.RepositoryID, error)
	// Repositories are the Git common directories registered, in the order
	// they were, for a command run outside any checkout to find the one
	// it means.
	Repositories(ctx context.Context) ([]string, error)
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
	// Tree selects the runs of revisions with this tree (SQL-O1).
	Tree model.ObjectID
	// NoBaselines leaves baselines out. They're in by default, since serve
	// runs them from the queue like any check.
	NoBaselines bool
	Limit       int
}

// AssessmentFilter selects assessments, newest first; empty fields select
// all. A branch, tree, and base select one revision's.
type AssessmentFilter struct {
	Branch     model.BranchID
	Tree, Base model.ObjectID
}

// Build is a target's result with the execution that built it.
type Build struct {
	Result    model.TargetResult
	Execution model.GuestExecution
}

// Reader reads records within a transaction.
type Reader interface {
	Branch(id model.BranchID) (model.Branch, error)
	// BranchNamed finds the branch with a Git name that is not merged.
	BranchNamed(name string) (model.Branch, error)
	// MergedBranchNamed finds the newest merged branch with a Git name,
	// whose record clean keeps; the name may have been used again since.
	MergedBranchNamed(name string) (model.Branch, error)
	Branches(filter BranchFilter) ([]model.Branch, error)

	Revision(id model.RevisionID) (model.Revision, error)
	// Revisions lists a branch's revisions, oldest first.
	Revisions(branch model.BranchID) ([]model.Revision, error)

	Plan(id model.PlanID) (model.Plan, error)

	Run(id model.RunID) (model.Run, error)
	RunNumbered(number int) (model.Run, error)
	Runs(filter RunFilter) ([]model.Run, error)
	Executions(run model.RunID) ([]model.GuestExecution, error)
	// Check is a check's records, read together (the SQL study's O2).
	Check(run model.RunID) (Check, error)
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
	// DependencyArchives are the kept archives guests in an environment
	// installed the ports named from, the newest few of each port first.
	DependencyArchives(environment model.Environment, ports []string) ([]model.DependencyArchive, error)
	// Reusable are a target's passed results in an environment that keep
	// what their builds read, from the builds themselves rather than a
	// reuse of them, each with the execution that built it, newest first,
	// at most limit (decision 28).
	Reusable(target model.TargetID, environment model.Environment, limit int) ([]Build, error)

	Session(id model.SessionID) (model.Session, error)
	// OpenSessions are the sessions that haven't ended, oldest first.
	OpenSessions() ([]model.Session, error)
	Lease(resource string) (model.Lease, error)
	// Events lists events after a sequence number, oldest first.
	// Events are the events after a sequence, at least as important as
	// level, every level where it is empty (SQL-O5).
	Events(after int64, limit int, level model.EventLevel) ([]model.Event, error)
	// RunEvents are one run's events after a sequence, oldest first, at
	// most limit; LastEvent is the journal's newest sequence, 0 when it has
	// none.
	RunEvents(run model.RunID, after int64, limit int) ([]model.Event, error)
	// LatestRunEvent is a run's newest event of any of the kinds; found is
	// false where it has none.
	LatestRunEvent(run model.RunID, kinds ...model.EventKind) (event model.Event, found bool, err error)
	LastEvent() (int64, error)
	// CountEvents counts the events of a kind journaled at or after a time.
	CountEvents(kind model.EventKind, since time.Time) (int, error)

	// Edits lists a branch's authoring records, oldest first.
	Edits(branch model.BranchID) ([]model.Edit, error)
	// Assessments lists what was recorded of what upstream's change means
	// for the ports revisions change, newest first.
	Assessments(filter AssessmentFilter) ([]model.Assessment, error)
	// ChangeRecords lists what revisions changed in their port
	// directories, selected as assessments are, newest first.
	ChangeRecords(filter AssessmentFilter) ([]model.ChangeRecord, error)
	Checkpoint(number int) (model.Checkpoint, error)
	// Checkpoints lists a branch's checkpoints, oldest first.
	Checkpoints(branch model.BranchID) ([]model.Checkpoint, error)
	// Decisions lists a branch's decisions on a kind of finding that rest
	// on one piece of evidence, by subject.
	Decisions(branch model.BranchID, finding model.FindingKind, evidence string) ([]model.Decision, error)
	// StandingDecisions lists the decisions on a kind of finding that
	// belong to no branch, by subject.
	StandingDecisions(finding model.FindingKind) ([]model.Decision, error)
	// BranchDecisions are a branch's decisions on a kind of finding,
	// whatever their evidence, by subject, oldest first: a hand edit's,
	// whose attempt is the evidence (model.FindingHandEdit).
	BranchDecisions(branch model.BranchID, finding model.FindingKind) ([]model.Decision, error)
	// CIFailures lists a branch's classified CI failures at a head, by
	// check and job.
	CIFailures(branch model.BranchID, head model.ObjectID) ([]model.CIFailure, error)
	// NetworkHolds are what held the branch's last submission plan of a
	// tree that took the network to find, and when that plan was made;
	// none, and the zero time, where no plan of it was.
	NetworkHolds(branch model.BranchID, tree model.ObjectID) ([]model.Concern, time.Time, error)
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
	// result names (decisions 36 and 44, D6): the newest passed build of
	// each target in each environment that reuse may choose, and an open
	// branch's newest passed result of each target in each environment.
	// The newest archive a guest installed each port from, in each
	// environment, kept since dependenciesSince, stays too. It returns
	// what it forgot, whose files go next.
	PruneArchives(before, dependenciesSince time.Time) ([]model.Archive, error)
	// KeepDependencyArchive records a kept archive as one a guest in an
	// environment installed a port from; the archive must be kept.
	KeepDependencyArchive(archive model.DependencyArchive) error
	// ForgetDependencyArchive drops that record, as for an archive whose
	// file is gone; the archive's own record stays.
	ForgetDependencyArchive(archive model.DependencyArchive) error
	// PruneHistory removes what up to limit branches that ended before a
	// time recorded of their checks, taken in ID order past after, sparing
	// what reuse may still choose and each branch's newest run (D6). It
	// says how much, and the last branch it took, for the next call's
	// after, or "" once none is left, when it also removes the inputs no
	// result names. Each call is a chunk, so a large backlog commits a
	// chunk at a time.
	PruneHistory(before time.Time, after model.BranchID, limit int) (Pruned, model.BranchID, error)
	// ForgetBranch forgets an archived branch, keeping of its checks only
	// what reuse may choose or another result names, and its record,
	// marked forgotten, which no list of branches returns.
	ForgetBranch(id model.BranchID, at time.Time) (Pruned, error)
	// PruneAssessments removes the assessments and change records of trees
	// an open branch moved past before a time, and says how many.
	PruneAssessments(before time.Time) (int, error)

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
	// RecordAssessment records a revision's assessment of a port,
	// replacing one recorded for the same revision, base, and port.
	RecordAssessment(assessment model.Assessment) error
	// RecordChange records what a revision changed in a port directory,
	// replacing one recorded for the same revision, base, and directory.
	RecordChange(record model.ChangeRecord) error
	// AddCheckpoint records a checkpoint; its Number must be
	// NextCheckpointNumber's.
	AddCheckpoint(checkpoint model.Checkpoint) error
	NextCheckpointNumber() (int, error)
	// SettleCheckpoint records a prepared checkpoint as applied or
	// abandoned, once.
	SettleCheckpoint(checkpoint model.Checkpoint) error
	// MarkRestored records an applied checkpoint's restore, once.
	MarkRestored(checkpoint model.Checkpoint) error
	// AddDecision records a decision; repeating one changes nothing.
	AddDecision(decision model.Decision) error
	// AddCIFailure records a CI job's failure, classified; a job's attempt
	// is recorded once, and repeating it changes nothing.
	AddCIFailure(failure model.CIFailure) error
	// SetNetworkHolds records what held a submission plan of a tree that
	// took the network to find, replacing the last plan's of it.
	SetNetworkHolds(branch model.BranchID, tree model.ObjectID, at time.Time, holds []model.Concern) error
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

// Pruned counts what PruneHistory removed, by kind.
type Pruned struct {
	Runs, Executions, Results, Plans, Revisions, Assessments, Changes, Inputs int
}

// Add counts another chunk's prune with this one.
func (p *Pruned) Add(other Pruned) {
	p.Runs += other.Runs
	p.Executions += other.Executions
	p.Results += other.Results
	p.Plans += other.Plans
	p.Revisions += other.Revisions
	p.Assessments += other.Assessments
	p.Changes += other.Changes
	p.Inputs += other.Inputs
}

// LiveUnderMigrationError is a migration refused while dockhand processes
// with fresh heartbeats read the schema it would replace (the v0.3.1 port
// update, 2026-10-15: a main build migrated the database under v0.3.1's
// running check, which then couldn't record itself and stopped): how
// many, the builds they run, and the migration's schemas.
type LiveUnderMigrationError struct {
	Processes int
	Builds    []string
	From, To  int
}

func (e *LiveUnderMigrationError) Error() string {
	builds := ""
	if len(e.Builds) > 0 {
		builds = " (" + strings.Join(e.Builds, ", ") + ")"
	}
	processes := "1 dockhand process is"
	if e.Processes != 1 {
		processes = fmt.Sprintf("%d dockhand processes are", e.Processes)
	}
	return fmt.Sprintf("%s working on schema %d%s: migrating the database to schema %d would leave them unable to record what they do", processes, e.From, builds, e.To)
}

func (e *LiveUnderMigrationError) Unwrap() error { return ErrLiveUnderMigration }

// ReleaseUnderMigrationError is a migration a development or rc build
// refused of a database a release last opened, unasked: it would lock that
// release out. The exiv2 session's read-only status, on a main build,
// migrated the shared database from 32 to 39, and the installed v0.4.0
// then refused it (the exiv2 session's notes, 2026-10-24).
type ReleaseUnderMigrationError struct {
	// Release is the release that last opened the database; Copy is where
	// a migration keeps its form at From.
	Release, Copy string
	From, To      int
}

func (e *ReleaseUnderMigrationError) Error() string {
	return fmt.Sprintf("dockhand %s, a release, last opened this database, at schema %d; migrating it to schema %d, as this build would, leaves %s unable to open it", e.Release, e.From, e.To, e.Release)
}

func (e *ReleaseUnderMigrationError) Unwrap() error { return ErrReleaseUnderMigration }

// Check is a check's records read in one call, where four places read a
// run's executions and each one's results row by row (the SQL study's
// O2): the run, its plan, its executions by attempt, each one's results in
// the order recorded, each reused result's origin execution with the run
// it belongs to, and the inputs records its results name.
type Check struct {
	Run        model.Run
	Plan       model.Plan
	Executions []model.GuestExecution
	Results    map[model.ExecutionID][]model.TargetResult
	Origins    map[model.ExecutionID]Origin
	Inputs     map[string]model.TargetInputs
}

// Origin is the execution a reused result was built in, and its run.
type Origin struct {
	Execution model.GuestExecution
	Run       model.Run
}
