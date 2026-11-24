package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite/sqlitetest"
)

// launching is a provider that starts its work through the launch call:
// before runs first, start is the work's start, and it then returns.
type launching struct {
	before  func()
	start   func(ctx context.Context) error
	started atomic.Int32
	err     error
}

func (l *launching) Name() string { return "launching" }

func (l *launching) Execute(ctx context.Context, _ buildenv.Job, build buildenv.Build) error {
	if l.before != nil {
		l.before()
	}
	l.err = build.Launch(`{"what":"the work"}`, func(ctx context.Context) (string, error) {
		l.started.Add(1)
		if l.start != nil {
			return `{"what":"the work","started":true}`, l.start(ctx)
		}
		return `{"what":"the work","started":true}`, nil
	})
	return l.err
}

func (c *Service) executions(t *testing.T, run model.Run) []model.GuestExecution {
	t.Helper()
	var executions []model.GuestExecution
	require.NoError(t, c.Store.View(t.Context(), c.Repository, func(r store.Reader) error {
		var err error
		executions, err = r.Executions(run.ID)
		return err
	}))
	return executions
}

// A launcher that reaches its launch after the check's cancel is recorded
// reads it under the lock and starts nothing, and the check is canceled;
// one that launches saves its note before it starts, and the note its
// start hands back changed (the lifecycle design's "Launching and
// stopping exclude each other").
func TestALaunchReadsTheCancelUnderItsLock(t *testing.T) {
	t.Parallel()
	c := service(t)
	environment := model.Environment{Provider: "launching", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	provider := &launching{}
	c.Providers = map[string]buildenv.Provider{"launching": provider}
	// The driver's own watch doesn't see the cancel first: the launch's
	// read under its lock does.
	c.Poll = time.Hour

	// Paused before it takes the lock, while a cancel is recorded.
	run := c.queued(t, "jq-paused", environment)
	provider.before = func() {
		require.NoError(t, c.Store.Update(context.Background(), c.Repository, func(tx store.Tx) error {
			recorded, err := tx.Run(run.ID)
			if err != nil {
				return err
			}
			recorded.CancelRequested = &at
			return tx.UpdateRun(recorded)
		}))
	}
	canceled, err := c.Drive(t.Context(), c.session(t), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State)
	require.ErrorIs(t, provider.err, buildenv.ErrNotLaunched)
	require.Zero(t, provider.started.Load(), "nothing was started")
	require.Empty(t, c.executions(t, run)[0].Launch, "nothing was noted")

	// Launched: the note is saved before the start, and the start's own
	// is saved after it.
	provider.before = nil
	launched := c.queued(t, "jq-launched", environment)
	var noted string
	provider.start = func(context.Context) error {
		noted = c.executions(t, launched)[0].Launch
		return nil
	}
	_, err = c.Drive(t.Context(), c.session(t), launched.ID)
	require.NoError(t, err)
	require.Equal(t, `{"what":"the work"}`, noted, "saved before anything started")
	require.Equal(t, `{"what":"the work","started":true}`, c.executions(t, launched)[0].Launch)
}

// A stop can't have the lock while a launch holds it, and says who, the
// live launcher; nor while a child the launch forked lives on after its
// launcher let go, since the child holds the same open file; once the
// child ends, the stop has it (the lifecycle design's "Every child that
// can start work holds the lock from the moment it is forked").
func TestTheLaunchLockIsHeldByTheLauncherAndWhatItForks(t *testing.T) {
	t.Parallel()
	c := service(t)
	environment := model.Environment{Provider: "launching", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	inside, leave := make(chan struct{}), make(chan struct{})
	// The child is set in the launch's goroutine and read here, so it
	// is handed over atomically (#295's race run).
	var forked atomic.Pointer[exec.Cmd]
	provider := &launching{start: func(ctx context.Context) error {
		close(inside)
		<-leave
		child := exec.Command("/bin/sleep", "30")
		child.ExtraFiles = filelock.Carried(ctx)
		if err := child.Start(); err != nil {
			return err
		}
		forked.Store(child)
		return nil
	}}
	c.Providers = map[string]buildenv.Provider{"launching": provider}
	run := c.queued(t, "jq-held", environment)
	done := make(chan error, 1)
	go func() {
		_, err := c.Drive(context.Background(), c.session(t), run.ID)
		done <- err
	}()
	select {
	case <-inside:
	case <-time.After(10 * time.Second):
		t.Fatal("the launch never started")
	}
	lock := launchLock{path: c.launchLockPath(c.executions(t, run)[0].ID)}
	_, err := lock.Hold(t.Context(), 100*time.Millisecond)
	require.ErrorContains(t, err, "its launch lock is still held after 100ms, by the dockhand launching it, PID ")
	require.ErrorContains(t, err, "which is still alive")

	close(leave)
	require.Eventually(t, func() bool { return provider.started.Load() == 1 && forked.Load() != nil }, 10*time.Second, 10*time.Millisecond)
	child := forked.Load()
	t.Cleanup(func() { _ = child.Process.Kill() })
	// The launch has let go, and the child it forked holds the lock.
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(lock.path)
		return strings.Contains(string(data), `"pid"`)
	}, 10*time.Second, 10*time.Millisecond)
	_, err = lock.Hold(t.Context(), 300*time.Millisecond)
	require.Error(t, err, "the child holds it")

	require.NoError(t, child.Process.Kill())
	_, _ = child.Process.Wait()
	release, err := lock.Hold(t.Context(), 5*time.Second)
	require.NoError(t, err, "with the child gone, the stop has it")
	release()
	_ = done
}

// loseLease takes the run's lease from its driver, as a driver that lost
// it while paused: whatever the driver saves after is refused.
func (c *Service) loseLease(run model.Run) error {
	return c.Store.Update(context.Background(), c.Repository, func(tx store.Tx) error {
		lease, err := tx.Lease(RunResource(run.ID))
		if err != nil {
			return err
		}
		return tx.ReleaseLease(lease)
	})
}

// A launch that can't save its note starts nothing, and nothing is
// noted, so a stop finds no launch to answer for; one whose start hands
// back a changed note that can't be saved keeps the note as first saved,
// which says only that something may have started (the lifecycle
// design's "Launching and stopping exclude each other" and "a note that
// couldn't be saved again").
func TestALaunchWhoseNoteCantBeSavedKeepsWhatWasSaved(t *testing.T) {
	t.Parallel()
	environment := model.Environment{Provider: "launching", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}

	c := service(t)
	c.Poll = time.Hour
	provider := &launching{}
	c.Providers = map[string]buildenv.Provider{"launching": provider}
	unsaved := c.queued(t, "jq-unsaved", environment)
	provider.before = func() { require.NoError(t, c.loseLease(unsaved)) }
	_, _ = c.Drive(t.Context(), c.session(t), unsaved.ID)
	require.ErrorIs(t, provider.err, buildenv.ErrNotLaunched)
	require.ErrorIs(t, provider.err, store.ErrStale, "the note couldn't be saved")
	require.Zero(t, provider.started.Load(), "nothing was started")
	require.Empty(t, c.executions(t, unsaved)[0].Launch, "nothing was noted")

	again := service(t)
	again.Poll = time.Hour
	provider = &launching{}
	again.Providers = map[string]buildenv.Provider{"launching": provider}
	kept := again.queued(t, "jq-kept", environment)
	provider.start = func(context.Context) error { return again.loseLease(kept) }
	_, _ = again.Drive(t.Context(), again.session(t), kept.ID)
	require.Equal(t, int32(1), provider.started.Load())
	require.ErrorIs(t, provider.err, store.ErrStale, "the start's own note couldn't be saved")
	require.Equal(t, `{"what":"the work"}`, again.executions(t, kept)[0].Launch, "the note stays as first saved")
}

// lockStopping is a provider whose Stop is its launch lock alone: ended
// once it holds it, not known while something does.
type lockStopping struct{ launching }

func (*lockStopping) Stop(ctx context.Context, request buildenv.StopRequest) ([]string, error) {
	release, err := request.Lock.Hold(ctx, 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	release()
	return nil, nil
}

// TestLaunchLockChild is a launcher in a process of its own, for
// TestALauncherThatDiedHoldingTheLock: it drives the run named in its
// environment through the launch call on the parent's store, forks a
// child holding the lock where asked, says it has launched, and waits to
// be killed.
func TestLaunchLockChild(t *testing.T) {
	t.Parallel()
	path := os.Getenv("DOCKHAND_LAUNCH_CHILD_DB")
	if path == "" {
		t.Skip("run by TestALauncherThatDiedHoldingTheLock")
	}
	c := serviceOn(t, path, os.Getenv("DOCKHAND_LAUNCH_CHILD_LOCKS"))
	c.Poll = time.Hour
	marker := os.Getenv("DOCKHAND_LAUNCH_CHILD_MARKER")
	c.Providers = map[string]buildenv.Provider{"launching": &launching{start: func(ctx context.Context) error {
		if pidFile := os.Getenv("DOCKHAND_LAUNCH_CHILD_FORK"); pidFile != "" {
			child := exec.Command("/bin/sleep", "60")
			child.ExtraFiles = filelock.Carried(ctx)
			if err := child.Start(); err != nil {
				return err
			}
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
				return err
			}
		}
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			return err
		}
		select {}
	}}}
	_, err := c.Drive(t.Context(), c.session(t), model.RunID(os.Getenv("DOCKHAND_LAUNCH_CHILD_RUN")))
	t.Fatalf("the launcher returned: %v", err)
}

// A launcher that dies holding its launch lock lets go of it as it dies,
// so a stop then has it and the cancel finishes; one that forked a child
// holding it before it died leaves the stop not known, naming the dead
// launcher as what started the holder, until the child ends (the
// lifecycle design's "Launch and stop" tests: one that died holding it).
func TestALauncherThatDiedHoldingTheLock(t *testing.T) {
	t.Parallel()
	environment := model.Environment{Provider: "launching", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	for _, forks := range []bool{false, true} {
		t.Run(map[bool]string{false: "alone", true: "with a child"}[forks], func(t *testing.T) {
			t.Parallel()
			path := sqlitetest.Fresh(t, filepath.Join(t.TempDir(), "dockhand.db"))
			launches := t.TempDir()
			c := serviceOn(t, path, launches)
			c.Providers = map[string]buildenv.Provider{"launching": &lockStopping{}}
			run := c.queued(t, "jq-died", environment)
			scratch := t.TempDir()
			marker, pidFile := filepath.Join(scratch, "launched"), filepath.Join(scratch, "child")
			launcher := exec.Command(os.Args[0], "-test.run=^TestLaunchLockChild$")
			launcher.Env = append(os.Environ(), "DOCKHAND_LAUNCH_CHILD_DB="+path, "DOCKHAND_LAUNCH_CHILD_LOCKS="+launches,
				"DOCKHAND_LAUNCH_CHILD_MARKER="+marker, "DOCKHAND_LAUNCH_CHILD_RUN="+string(run.ID))
			if forks {
				launcher.Env = append(launcher.Env, "DOCKHAND_LAUNCH_CHILD_FORK="+pidFile)
			}
			launcher.Stderr = os.Stderr
			require.NoError(t, launcher.Start())
			t.Cleanup(func() { _ = launcher.Process.Kill(); _ = launcher.Wait() })
			require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 30*time.Second, 20*time.Millisecond, "the launcher launched")
			require.Equal(t, `{"what":"the work"}`, c.executions(t, run)[0].Launch)
			var child *os.Process
			if forks {
				data, err := os.ReadFile(pidFile)
				require.NoError(t, err)
				pid, err := strconv.Atoi(string(data))
				require.NoError(t, err)
				child, err = os.FindProcess(pid)
				require.NoError(t, err)
				t.Cleanup(func() { _ = child.Kill() })
			}
			require.NoError(t, launcher.Process.Kill())
			_ = launcher.Wait()

			if forks {
				pending, err := c.RequestCancel(t.Context(), c.session(t), run.ID)
				require.ErrorContains(t, err, "its launch lock is still held after 300ms, by a process that the dockhand which launched it, PID "+strconv.Itoa(launcher.Process.Pid)+", now gone, started")
				require.Equal(t, model.RunRunning, pending.State, "not known while the child holds it")
				require.NoError(t, child.Kill())
				require.Eventually(t, func() bool { return syscall.Kill(child.Pid, 0) != nil }, 10*time.Second, 20*time.Millisecond)
			}
			canceled, err := c.RequestCancel(t.Context(), c.session(t), run.ID)
			require.NoError(t, err)
			require.Equal(t, model.RunCanceled, canceled.State, "the stop had the lock")
		})
	}
}

// relaunching is a provider that launches twice through the launch call,
// as GitHub's pushes and then runs a run again, with between run between.
type relaunching struct {
	between func()
	errs    []error
	started atomic.Int32
}

func (*relaunching) Name() string { return "launching" }

func (r *relaunching) Execute(_ context.Context, _ buildenv.Job, build buildenv.Build) error {
	for i, note := range []string{`{"action":"push"}`, `{"action":"rerun"}`} {
		if i == 1 && r.between != nil {
			r.between()
		}
		err := build.Launch(note, func(context.Context) (string, error) {
			r.started.Add(1)
			return note, nil
		})
		r.errs = append(r.errs, err)
		if err != nil {
			return err
		}
	}
	return nil
}

// A rerun asked once the check's cancel is recorded reads it under its
// lock and starts nothing, and the attempt's note stays the launch
// before it (the lifecycle design's "Running a finished GitHub run again
// ... goes through the same call").
func TestARerunAskedWhileACancelIsRecordedStartsNothing(t *testing.T) {
	t.Parallel()
	c := service(t)
	c.Poll = time.Hour
	environment := model.Environment{Provider: "launching", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	provider := &relaunching{}
	c.Providers = map[string]buildenv.Provider{"launching": provider}
	run := c.queued(t, "jq-rerun", environment)
	provider.between = func() {
		require.NoError(t, c.Store.Update(context.Background(), c.Repository, func(tx store.Tx) error {
			recorded, err := tx.Run(run.ID)
			if err != nil {
				return err
			}
			recorded.CancelRequested = &at
			return tx.UpdateRun(recorded)
		}))
	}
	canceled, err := c.Drive(t.Context(), c.session(t), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State)
	require.Len(t, provider.errs, 2)
	require.NoError(t, provider.errs[0])
	require.ErrorIs(t, provider.errs[1], buildenv.ErrNotLaunched)
	require.Equal(t, int32(1), provider.started.Load(), "the push started, the rerun didn't")
	require.Equal(t, `{"action":"push"}`, c.executions(t, run)[0].Launch)
}
