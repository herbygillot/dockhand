package model

import "time"

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
	// Kind is a stable name for the event's type, such as run.state or
	// guest.clone; observers show kinds they do not know generically.
	Kind    string
	Level   EventLevel
	Message string
}

// EventLevel is the output level at which an event is shown (docs/output.md).
type EventLevel string

const (
	LevelInfo    EventLevel = "info"
	LevelVerbose EventLevel = "verbose"
	LevelDebug   EventLevel = "debug"
)
