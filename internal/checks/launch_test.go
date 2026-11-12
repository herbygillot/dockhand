package checks

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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
	var child *exec.Cmd
	provider := &launching{start: func(ctx context.Context) error {
		close(inside)
		<-leave
		child = exec.Command("/bin/sleep", "30")
		child.ExtraFiles = filelock.Carried(ctx)
		return child.Start()
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
	require.Eventually(t, func() bool { return provider.started.Load() == 1 && child != nil && child.Process != nil }, 10*time.Second, 10*time.Millisecond)
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
