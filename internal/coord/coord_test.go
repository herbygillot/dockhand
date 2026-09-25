package coord

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
)

// processes is a process table the test controls.
type processes struct {
	mu   sync.Mutex
	dead map[int]bool
}

func (p *processes) Alive(proc Process) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.dead[proc.PID], nil
}

func (p *processes) kill(pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dead[pid] = true
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	c     *Coordinator
	procs *processes
	clock *clock
}

func setup(t *testing.T) fixture {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "dockhand.db"), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	repo, err := s.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)
	f := fixture{procs: &processes{dead: map[int]bool{}}, clock: &clock{now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}}
	f.c = &Coordinator{Store: s, Repository: repo, Liveness: f.procs, Now: f.clock.Now, Heartbeat: 5 * time.Second, HungAfter: 2 * time.Minute}
	return f
}

func (f fixture) session(t *testing.T, pid int, kind model.SessionKind) *Session {
	t.Helper()
	s, err := f.c.StartFor(t.Context(), Process{PID: pid, Start: "test"}, kind, "test")
	require.NoError(t, err)
	return s
}

func TestALiveHolderKeepsItsLease(t *testing.T) {
	f := setup(t)
	a, b := f.session(t, 100, model.SessionServe), f.session(t, 200, model.SessionForeground)
	lease, err := a.Acquire(t.Context(), "run:run_1")
	require.NoError(t, err)
	require.Equal(t, uint64(1), lease.Generation)

	_, err = b.Acquire(t.Context(), "run:run_1")
	var held *HeldError
	require.ErrorAs(t, err, &held)
	require.ErrorIs(t, err, ErrHeld)
	require.Equal(t, a.ID(), held.Holder.ID)
	require.ErrorContains(t, err, "pid 100")

	again, err := a.Acquire(t.Context(), "run:run_1")
	require.NoError(t, err, "a holder may renew its own lease")
	require.Equal(t, uint64(2), again.Generation)
}

func TestADeadHoldersLeaseIsTakenAtOnceAndItsWritesAreFenced(t *testing.T) {
	f := setup(t)
	a, b := f.session(t, 100, model.SessionServe), f.session(t, 200, model.SessionServe)
	old, err := a.Acquire(t.Context(), "execution:ex_1")
	require.NoError(t, err)

	f.procs.kill(100)
	taken, err := b.Acquire(t.Context(), "execution:ex_1")
	require.NoError(t, err, "no step deadline to wait out: the process is gone")
	require.Equal(t, uint64(2), taken.Generation)

	wrote := false
	err = a.Fenced(t.Context(), old, func(store.Tx) error { wrote = true; return nil })
	require.ErrorIs(t, err, store.ErrStale, "a holder that resumes cannot write on a claim it lost")
	require.False(t, wrote)
	require.NoError(t, b.Fenced(t.Context(), taken, func(store.Tx) error { return nil }))

	var events []model.Event
	_, err = f.c.Tail(canceledAfterOnePass(t), 0, time.Millisecond, func(e model.Event) error { events = append(events, e); return nil })
	require.NoError(t, err)
	last := events[len(events)-1]
	require.Equal(t, "lease.acquire", last.Kind)
	require.Contains(t, last.Message, "process 100 has exited")
}

func TestAHungHolderIsJudgedOnlyByAnAwakeSession(t *testing.T) {
	f := setup(t)
	a, b := f.session(t, 100, model.SessionServe), f.session(t, 200, model.SessionServe)
	_, err := a.Acquire(t.Context(), LeaderResource)
	require.NoError(t, err)

	// Both processes sleep with the laptop for ten minutes.
	f.clock.advance(10 * time.Minute)
	_, err = b.Lead(t.Context())
	require.ErrorIs(t, err, ErrHeld, "a session that just woke judges no one")

	// b has been awake and beating, while a stays silent.
	require.NoError(t, b.Beat(t.Context()))
	_, err = b.Lead(t.Context())
	require.NoError(t, err, "silent past HungAfter, seen by an awake session")

	// a fresh heartbeat from a keeps its claim, however long b was awake.
	g := setup(t)
	c, d := g.session(t, 100, model.SessionServe), g.session(t, 200, model.SessionServe)
	_, err = c.Acquire(t.Context(), LeaderResource)
	require.NoError(t, err)
	g.clock.advance(time.Minute)
	require.NoError(t, c.Beat(t.Context()))
	require.NoError(t, d.Beat(t.Context()))
	_, err = d.Lead(t.Context())
	require.ErrorIs(t, err, ErrHeld)
}

func TestAnEndedSessionHoldsNothing(t *testing.T) {
	f := setup(t)
	a, b := f.session(t, 100, model.SessionServe), f.session(t, 200, model.SessionServe)
	_, err := a.Lead(t.Context())
	require.NoError(t, err)
	_, err = b.Lead(t.Context())
	require.ErrorIs(t, err, ErrHeld, "b stands by")
	require.NoError(t, a.End(t.Context()))
	lease, err := b.Lead(t.Context())
	require.NoError(t, err, "the standby leads once the leader ends")
	require.Equal(t, uint64(2), lease.Generation)

	require.NoError(t, f.c.Store.View(t.Context(), f.c.Repository, func(r store.Reader) error {
		live, err := r.Sessions()
		require.NoError(t, err)
		require.Len(t, live, 1)
		return nil
	}))
}

func TestControlsAreAppliedByWhoeverCan(t *testing.T) {
	f := setup(t)
	runner, canceller := f.session(t, 100, model.SessionForeground), f.session(t, 200, model.SessionForeground)
	_, err := runner.Acquire(t.Context(), "run:run_1")
	require.NoError(t, err)

	_, holder, err := canceller.TakeIfUnattended(t.Context(), "run:run_1")
	require.NoError(t, err)
	require.NotNil(t, holder, "a live holder applies the cancel itself")
	require.Equal(t, runner.ID(), holder.ID)

	f.procs.kill(100)
	lease, holder, err := canceller.TakeIfUnattended(t.Context(), "run:run_1")
	require.NoError(t, err)
	require.Nil(t, holder)
	require.Equal(t, canceller.ID(), lease.Holder, "with no one attending, the canceller applies it")
}

func TestTailDeliversTheJournalInOrder(t *testing.T) {
	f := setup(t)
	s := f.session(t, 100, model.SessionServe)
	require.NoError(t, f.c.Store.Update(t.Context(), f.c.Repository, func(tx store.Tx) error {
		for _, kind := range []string{"run.state", "guest.clone", "target.result"} {
			if _, err := s.Emit(tx, model.Event{Kind: kind, Run: "run_1"}); err != nil {
				return err
			}
		}
		return nil
	}))
	var kinds []string
	last, err := f.c.Tail(canceledAfterOnePass(t), 0, time.Millisecond, func(e model.Event) error {
		if e.Run == "run_1" {
			kinds = append(kinds, e.Kind)
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"run.state", "guest.clone", "target.result"}, kinds)

	stop := errors.New("stop")
	_, err = f.c.Tail(t.Context(), 0, time.Millisecond, func(model.Event) error { return stop })
	require.ErrorIs(t, err, stop)

	more, err := f.c.Tail(canceledAfterOnePass(t), last, time.Millisecond, func(model.Event) error {
		t.Fatal("nothing new after the last sequence")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, last, more)
}

func TestKeepAliveBeats(t *testing.T) {
	f := setup(t)
	f.c.Now, f.c.Heartbeat = nil, 10*time.Millisecond
	s := f.session(t, 100, model.SessionServe)
	started := s.Record().HeartbeatAt
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	require.NoError(t, s.KeepAlive(ctx))
	require.True(t, s.Record().HeartbeatAt.After(started))
}

func TestSystemLivenessKnowsAReplacedOrExitedProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process start times are read on Linux and macOS")
	}
	self, err := Self()
	require.NoError(t, err)
	alive, err := System{}.Alive(self)
	require.NoError(t, err)
	require.True(t, alive)
	alive, err = System{}.Alive(Process{PID: self.PID, Start: self.Start + "0"})
	require.NoError(t, err)
	require.False(t, alive, "the same PID started at another time is another process")

	child := exec.Command("sleep", "30")
	require.NoError(t, child.Start())
	start, err := processStart(child.Process.Pid)
	require.NoError(t, err)
	proc := Process{PID: child.Process.Pid, Start: start}
	alive, err = System{}.Alive(proc)
	require.NoError(t, err)
	require.True(t, alive)
	require.NoError(t, child.Process.Kill())
	_ = child.Wait()
	alive, err = System{}.Alive(proc)
	require.NoError(t, err)
	require.False(t, alive, "an exited process is dead at once")
}

// canceledAfterOnePass gives Tail a context that ends shortly after it has
// read what is already in the journal.
func canceledAfterOnePass(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}
