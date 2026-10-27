package model

import (
	"slices"
	"strings"
	"time"
)

// SessionID identifies one dockhand process's session with the database.
type SessionID string

// SessionKind is the part a process plays in coordination (Design v3 §11).
type SessionKind string

const (
	// SessionServe drives the queue, as leader or standby.
	SessionServe SessionKind = "serve"
	// SessionForeground runs its own request because no serve leads.
	SessionForeground SessionKind = "foreground"
	// SessionObserver only reads: status, watch, a followed check.
	SessionObserver SessionKind = "observer"
)

// Session is one dockhand process that runs or watches work. Its death is
// known directly: every process is on one Mac, so a PID that is gone, or
// that now names a process started at another time, is a dead session.
type Session struct {
	ID         SessionID
	Repository RepositoryID
	Kind       SessionKind
	PID        int
	// ProcessStart is the operating system's record of when the process
	// started, in its own opaque form, so a reused PID cannot pass for it.
	ProcessStart string
	// Version is the dockhand build, so a newer database or an older peer
	// can be named.
	Version     string
	StartedAt   time.Time
	HeartbeatAt time.Time
	// EndedAt is set when the process ended cleanly.
	EndedAt *time.Time
	// Schema is the newest database schema the process's build writes,
	// which the store records; 0 for a session an earlier build recorded.
	Schema int
}

// Validate checks the rules every stored session keeps.
func (s Session) Validate() error {
	switch {
	case s.ID == "" || s.Repository == "":
		return invalid("session %q has no ID or repository", s.ID)
	case s.Kind != SessionServe && s.Kind != SessionForeground && s.Kind != SessionObserver:
		return invalid("session %s has unknown kind %q", s.ID, s.Kind)
	case s.PID <= 0 || s.ProcessStart == "":
		return invalid("session %s does not identify its process", s.ID)
	case s.StartedAt.IsZero() || s.HeartbeatAt.Before(s.StartedAt):
		return invalid("session %s has inconsistent times", s.ID)
	}
	return nil
}

// Lease is the fenced claim on one resource: a run, an execution, a
// provider reservation, or a repository's leadership. Generation increases
// with every acquisition and survives release, so a write carrying an
// older generation is refused.
type Lease struct {
	Resource string
	// Holder is empty when the lease is free.
	Holder     SessionID
	Generation uint64
	AcquiredAt time.Time
}

// Event is one entry in a repository's journal: a transition, a provider
// milestone, a capacity wait, a retry, or a problem. Observers tail events
// by sequence, so every terminal tells the same story.
type Event struct {
	// Sequence orders events; the store assigns it.
	Sequence int64
	At       time.Time
	Session  SessionID
	Branch   BranchID
	Run      RunID
	Target   TargetID
	// Kind is a stable name for the event's type, such as run.state;
	// observers show kinds they do not know generically.
	Kind    EventKind
	Level   EventLevel
	Message string
}

// EventKind names an event's type in the journal: what the engine emits
// and what command, watch, and serve match it by (C4b's typed half; the
// command package review's 7a row 14), one constant each.
type EventKind string

const (
	EventSessionStart EventKind = "session.start"
	EventSessionEnd   EventKind = "session.end"
	EventLeaseAcquire EventKind = "lease.acquire"
	EventRunState     EventKind = "run.state"
	EventRunCancel    EventKind = "run.cancel"
	EventProgress     EventKind = "progress"
	// EventWaiting is a run waiting on something outside it, a slot or
	// the Mac's VMs, which every level says (rc1's D-R2).
	EventWaiting        EventKind = "waiting"
	EventTargetResult   EventKind = "target.result"
	EventExecutionState EventKind = "execution.state"
	EventExecutionRetry EventKind = "execution.retry"
	// EventArchiveGone says a dependency's kept archive is gone, and
	// EventArchiveAltered one changed since it was kept.
	EventArchiveGone    EventKind = "archive.gone"
	EventArchiveAltered EventKind = "archive.altered"
	EventCleanup        EventKind = "cleanup"
	// EventServeSubmit is a pull request serve opened unattended, which
	// its daily limit counts.
	EventServeSubmit         EventKind = "serve.submit"
	EventPullRequestObserved EventKind = "pr.observed"
	EventReview              EventKind = "review"
	EventBranchStart         EventKind = "branch.start"
	EventBranchAdopt         EventKind = "branch.adopt"
	EventBranchEdit          EventKind = "branch.edit"
	EventBranchRename        EventKind = "branch.rename"
	EventBranchWorktree      EventKind = "branch.worktree"
	EventBranchState         EventKind = "branch.state"
	EventBranchSubmit        EventKind = "branch.submit"
	EventBranchRerequest     EventKind = "branch.rerequest"
	EventBranchReady         EventKind = "branch.ready"
	EventBranchRestore       EventKind = "branch.restore"
	EventBranchClean         EventKind = "branch.clean"
)

// ChangesStatus reports whether an event of the kind can change what
// status shows: a run's state, a branch's, a pull request's, and serve's
// work, as against a check's progress, a target's result before its run
// ends, and sessions' comings and goings, which a live status needn't
// redraw for (SQL-O6). A checkpoint is a branch's.
func (k EventKind) ChangesStatus() bool {
	switch k {
	case EventRunState, EventRunCancel, EventServeSubmit, EventPullRequestObserved, EventReview, EventCleanup:
		return true
	}
	return strings.HasPrefix(string(k), "branch.")
}

// CheckpointEvent is the event a tidy's or a rebase's checkpoint is
// recorded by: branch.tidy, branch.rebase.
func CheckpointEvent(kind CheckpointKind) EventKind { return EventKind("branch." + string(kind)) }

// EventLevel is the output level at which an event is shown, as v2 defined
// them (docs/v2/output.md, at tag v2-final).
type EventLevel string

const (
	LevelInfo    EventLevel = "info"
	LevelVerbose EventLevel = "verbose"
	LevelDebug   EventLevel = "debug"
)

// AtLeast are the levels at least as important as l, info first: info
// alone for info, and every level for debug. An empty level is every one.
func (l EventLevel) AtLeast() []EventLevel {
	levels := []EventLevel{LevelInfo, LevelVerbose, LevelDebug}
	if i := slices.Index(levels, l); i >= 0 {
		return levels[:i+1]
	}
	return levels
}
