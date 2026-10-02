package coord

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
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
	require.NoError(t, f.c.Store.View(t.Context(), f.c.Repository, func(r store.Reader) error {
		events, err = r.Events(0, 500)
		return err
	}))
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
		ended, err := r.Session(a.ID())
		require.NoError(t, err)
		require.NotNil(t, ended.EndedAt)
		standby, err := r.Session(b.ID())
		require.NoError(t, err)
		require.Nil(t, standby.EndedAt)
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

func TestHolderNamesOnlyALiveHolder(t *testing.T) {
	f := setup(t)
	a, b := f.session(t, 100, model.SessionServe), f.session(t, 200, model.SessionForeground)
	holder, err := b.Holder(t.Context(), LeaderResource)
	require.NoError(t, err)
	require.Nil(t, holder, "no one leads")
	_, err = a.Lead(t.Context())
	require.NoError(t, err)
	holder, err = b.Holder(t.Context(), LeaderResource)
	require.NoError(t, err)
	require.Equal(t, a.ID(), holder.ID)
	f.procs.kill(100)
	holder, err = b.Holder(t.Context(), LeaderResource)
	require.NoError(t, err)
	require.Nil(t, holder, "a dead leader leads nothing")
}

func TestKeepAliveBeats(t *testing.T) {
	f := setup(t)
	f.c.Now, f.c.Heartbeat = nil, 10*time.Millisecond
	s := f.session(t, 100, model.SessionServe)
	started := s.Record().HeartbeatAt
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	s.KeepAlive(ctx)
	require.True(t, s.Record().HeartbeatAt.After(started))
}

// failingWrites is a store whose next writes fail, as one that waited past
// SQLite's busy or transaction timeout does.
type failingWrites struct {
	store.Store
	mu   sync.Mutex
	fail int
}

func (f *failingWrites) Update(ctx context.Context, repo model.RepositoryID, fn func(store.Tx) error) error {
	f.mu.Lock()
	failing := f.fail > 0
	if failing {
		f.fail--
	}
	f.mu.Unlock()
	if failing {
		return context.DeadlineExceeded
	}
	return f.Store.Update(ctx, repo, fn)
}

// A failed beat doesn't end the heartbeat: one write that waited past the
// store's timeouts stopped it for good, and another session could then
// judge a live serve hung and take its lead (the limits sweep). The run of
// failures is said once, and so is the beat that ends it.
func TestAHeartbeatGoesOnAfterAFailedBeat(t *testing.T) {
	f := setup(t)
	f.c.Now, f.c.Heartbeat = nil, 10*time.Millisecond
	s := f.session(t, 100, model.SessionServe)
	flaky := &failingWrites{Store: f.c.Store, fail: 3}
	f.c.Store = flaky
	started := s.Record().HeartbeatAt
	var said []string
	var mu sync.Mutex
	ctx := progress.WithReporter(t.Context(), func(update progress.Update) {
		mu.Lock()
		defer mu.Unlock()
		said = append(said, update.Message)
	})
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	s.KeepAlive(ctx)
	require.True(t, s.Record().HeartbeatAt.After(started), "it beat again after the failures")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, said, 2, "%q", said)
	require.Contains(t, said[0], "Couldn't record that this dockhand is still running (context deadline exceeded)")
	require.Equal(t, "This dockhand is recorded as running again.", said[1])
}

// An ended session's heartbeat stops with it, and says nothing: bump's
// check ended its session while the process went on to submit, and the
// next beat, refused, read as a heartbeat lost (field testing, 2026-10-02).
func TestAnEndedSessionStopsBeatingQuietly(t *testing.T) {
	f := setup(t)
	f.c.Now, f.c.Heartbeat = nil, 10*time.Millisecond
	s := f.session(t, 100, model.SessionForeground)
	var said []string
	var mu sync.Mutex
	ctx := progress.WithReporter(t.Context(), func(update progress.Update) {
		mu.Lock()
		defer mu.Unlock()
		said = append(said, update.Message)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.KeepAlive(ctx)
	}()
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, s.End(t.Context()))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the heartbeat went on after the session ended")
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, said)
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
