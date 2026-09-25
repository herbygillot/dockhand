// Package coord coordinates dockhand processes that share one database
// (docs/design-v3.md §11): sessions, fenced leases, one serve leader with
// standbys, and the event journal observers tail.
//
// Every process that runs or watches work opens a Session. A session's
// death is known directly, since every process is on one Mac: a PID that
// no longer exists, or that names a process started at another time, is a
// dead session at once. A heartbeat covers a process that is alive but
// hung. It is judged only by a coordinator that has itself been awake, so
// a laptop waking from sleep does not declare every peer dead.
//
// A lease on a resource names its session and a generation. Taking a lease
// from a dead holder raises the generation, and every write made under a
// lease checks it first, so a holder that was paused and resumes cannot
// write on a claim it lost. External calls it makes after resuming are
// guarded by the providers' idempotency and reconciliation, not by leases.
package coord

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// LeaderResource is the lease a repository's leading serve holds.
const LeaderResource = "leader"

// ErrHeld reports a lease another live session holds.
var ErrHeld = errors.New("held by another session")

// HeldError names the session that holds a lease.
type HeldError struct {
	Resource string
	Holder   model.Session
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("%s is held by %s session %s (pid %d)", e.Resource, e.Holder.Kind, e.Holder.ID, e.Holder.PID)
}

func (e *HeldError) Unwrap() error { return ErrHeld }

// Coordinator binds coordination to one repository in one store.
type Coordinator struct {
	Store      store.Store
	Repository model.RepositoryID
	// Liveness defaults to what the operating system reports.
	Liveness Liveness
	// Now defaults to time.Now.
	Now func() time.Time
	// Heartbeat is how often a session records that it is alive; five
	// seconds by default.
	Heartbeat time.Duration
	// HungAfter is how long a live process may go without a heartbeat
	// before its leases may be taken; two minutes by default.
	HungAfter time.Duration
}

func (c *Coordinator) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Coordinator) liveness() Liveness {
	if c.Liveness != nil {
		return c.Liveness
	}
	return System{}
}

func (c *Coordinator) heartbeat() time.Duration {
	if c.Heartbeat > 0 {
		return c.Heartbeat
	}
	return 5 * time.Second
}

func (c *Coordinator) hungAfter() time.Duration {
	if c.HungAfter > 0 {
		return c.HungAfter
	}
	return 2 * time.Minute
}

// Session is this process's session.
type Session struct {
	c      *Coordinator
	record model.Session
	// lastBeat is when this process last recorded its heartbeat, by its
	// own clock; a long gap means it was asleep and must not judge peers.
	lastBeat time.Time
}

// Start opens a session for the running process.
func (c *Coordinator) Start(ctx context.Context, kind model.SessionKind, version string) (*Session, error) {
	self, err := Self()
	if err != nil {
		return nil, err
	}
	return c.StartFor(ctx, self, kind, version)
}

// StartFor opens a session for the given process; Start is the usual way,
// and this form serves tests and supervisors.
func (c *Coordinator) StartFor(ctx context.Context, p Process, kind model.SessionKind, version string) (*Session, error) {
	now := c.now()
	record := model.Session{
		ID: model.SessionID(store.NewID("ses")), Repository: c.Repository, Kind: kind,
		PID: p.PID, ProcessStart: p.Start, Version: version, StartedAt: now, HeartbeatAt: now,
	}
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		if err := tx.AddSession(record); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: now, Session: record.ID, Kind: "session.start", Level: model.LevelVerbose,
			Message: fmt.Sprintf("%s session started (pid %d, dockhand %s)", kind, p.PID, version)})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Session{c: c, record: record, lastBeat: now}, nil
}

// Record is the session as last written.
func (s *Session) Record() model.Session { return s.record }

// ID is the session's identity.
func (s *Session) ID() model.SessionID { return s.record.ID }

// Beat records that the session is alive.
func (s *Session) Beat(ctx context.Context) error {
	now := s.c.now()
	next := s.record
	next.HeartbeatAt = now
	if err := s.c.Store.Update(ctx, s.c.Repository, func(tx store.Tx) error { return tx.UpdateSession(next) }); err != nil {
		return err
	}
	s.record, s.lastBeat = next, now
	return nil
}

// KeepAlive beats until ctx is done, returning the first failure.
func (s *Session) KeepAlive(ctx context.Context) error {
	ticker := time.NewTicker(s.c.heartbeat())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.Beat(ctx); err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
}

// End closes the session cleanly. Leases it still holds become free to
// take, since an ended session is dead to every judge.
func (s *Session) End(ctx context.Context) error {
	now := s.c.now()
	next := s.record
	next.HeartbeatAt, next.EndedAt = now, &now
	err := s.c.Store.Update(ctx, s.c.Repository, func(tx store.Tx) error {
		if err := tx.UpdateSession(next); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: now, Session: next.ID, Kind: "session.end", Level: model.LevelVerbose, Message: fmt.Sprintf("%s session ended", next.Kind)})
		return err
	})
	if err == nil {
		s.record = next
	}
	return err
}

// awake reports whether this session has beaten recently by its own clock.
// A process that was asleep, with its peers, judges no one until it beats.
func (s *Session) awake(now time.Time) bool {
	return now.Sub(s.lastBeat) <= 2*s.c.heartbeat()
}

// Verdict is a judgment of another session.
type Verdict struct {
	Dead   bool
	Reason string
}

// Judge decides whether another session is dead: ended, its process gone
// or replaced, or, when this session is awake to see it, silent for longer
// than HungAfter.
func (s *Session) Judge(other model.Session) (Verdict, error) {
	if other.ID == s.record.ID {
		return Verdict{}, nil
	}
	if other.EndedAt != nil {
		return Verdict{Dead: true, Reason: "ended"}, nil
	}
	alive, err := s.c.liveness().Alive(Process{PID: other.PID, Start: other.ProcessStart})
	if err != nil {
		return Verdict{}, err
	}
	if !alive {
		return Verdict{Dead: true, Reason: fmt.Sprintf("process %d has exited", other.PID)}, nil
	}
	now := s.c.now()
	if silent := now.Sub(other.HeartbeatAt); s.awake(now) && silent > s.c.hungAfter() {
		return Verdict{Dead: true, Reason: fmt.Sprintf("no heartbeat for %s", silent.Round(time.Second))}, nil
	}
	return Verdict{}, nil
}

// Acquire takes the lease on a resource, from its holder when that holder
// is dead, and refuses with a HeldError when a live session holds it.
func (s *Session) Acquire(ctx context.Context, resource string) (model.Lease, error) {
	var lease model.Lease
	err := s.c.Store.Update(ctx, s.c.Repository, func(tx store.Tx) error {
		var err error
		lease, err = s.acquire(tx, resource)
		return err
	})
	return lease, err
}

func (s *Session) acquire(tx store.Tx, resource string) (model.Lease, error) {
	current, err := tx.Lease(resource)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return model.Lease{}, err
	}
	reason := ""
	if current.Holder != "" && current.Holder != s.record.ID {
		holder, err := tx.Session(current.Holder)
		if err != nil {
			return model.Lease{}, err
		}
		verdict, err := s.Judge(holder)
		if err != nil {
			return model.Lease{}, err
		}
		if !verdict.Dead {
			return model.Lease{}, &HeldError{Resource: resource, Holder: holder}
		}
		reason = fmt.Sprintf("; taken from session %s: %s", holder.ID, verdict.Reason)
	}
	lease, err := tx.AcquireLease(resource, s.record.ID)
	if err != nil {
		return model.Lease{}, err
	}
	_, err = tx.AppendEvent(model.Event{At: s.c.now(), Session: s.record.ID, Kind: "lease.acquire", Level: model.LevelDebug,
		Message: fmt.Sprintf("%s acquired (generation %d)%s", resource, lease.Generation, reason)})
	return lease, err
}

// Release frees a lease the session still holds.
func (s *Session) Release(ctx context.Context, lease model.Lease) error {
	return s.c.Store.Update(ctx, s.c.Repository, func(tx store.Tx) error { return tx.ReleaseLease(lease) })
}

// Fenced runs fn in a write transaction only while the lease is still
// current; a lost lease returns store.ErrStale and writes nothing.
func (s *Session) Fenced(ctx context.Context, lease model.Lease, fn func(store.Tx) error) error {
	return s.c.Store.Update(ctx, s.c.Repository, func(tx store.Tx) error {
		if err := tx.CheckLease(lease); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Lead takes the repository's leadership, or reports the live leader it
// stands by for.
func (s *Session) Lead(ctx context.Context) (model.Lease, error) {
	return s.Acquire(ctx, LeaderResource)
}

// TakeIfUnattended takes a resource's lease only when no live session
// holds it, reporting who does otherwise. This is how a control is applied
// by whoever can: a cancel whose run has a live holder is left to that
// holder, and one whose holder is dead is applied by the canceller.
func (s *Session) TakeIfUnattended(ctx context.Context, resource string) (lease model.Lease, holder *model.Session, err error) {
	lease, err = s.Acquire(ctx, resource)
	var held *HeldError
	if errors.As(err, &held) {
		return model.Lease{}, &held.Holder, nil
	}
	return lease, nil, err
}

// Emit appends an event from this session within a transaction, stamping
// its session and time.
func (s *Session) Emit(tx store.Tx, event model.Event) (int64, error) {
	event.Session, event.At = s.record.ID, s.c.now()
	return tx.AppendEvent(event)
}

// Tail delivers the repository's events after a sequence number to fn, in
// order, polling every interval until ctx is done or fn fails. It returns
// the last sequence delivered.
func (c *Coordinator) Tail(ctx context.Context, after int64, interval time.Duration, fn func(model.Event) error) (int64, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	for {
		var events []model.Event
		err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
			var err error
			events, err = r.Events(after, 500)
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return after, nil
			}
			return after, err
		}
		for _, event := range events {
			if err := fn(event); err != nil {
				return after, err
			}
			after = event.Sequence
		}
		if len(events) == 500 {
			continue
		}
		select {
		case <-ctx.Done():
			return after, nil
		case <-time.After(interval):
		}
	}
}
