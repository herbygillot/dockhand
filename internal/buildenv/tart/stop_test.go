package tart

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	guestprogram "github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// lock is a launch lock, held where held is set.
type lock struct {
	held  error
	taken bool
}

func (l *lock) Hold(context.Context, time.Duration) (func(), error) {
	if l.held != nil {
		return nil, l.held
	}
	l.taken = true
	return func() {}, nil
}

func stopping(t *testing.T) (*fakeMac, *Provider) {
	t.Helper()
	mac := newMac()
	p := testProvider(mac)
	p.Tart = tartvm.Client{Home: filepath.Join(t.TempDir(), ".dockhand", "tart"), Env: os.Environ()}
	require.NoError(t, os.MkdirAll(p.Tart.Home, 0o700))
	return mac, p
}

func attempt(clone string) model.GuestExecution {
	return model.GuestExecution{ID: "tart_1", Attempt: 1, ProviderRef: clone, Launch: launchNote{Clone: clone}.String()}
}

// A cancel of a Tart check stops the clone the attempt noted, then, holding
// its launch lock, which the clone's tart run holds until the VM is gone,
// deletes it and forgets its marker: ended. A clone gone already is ended
// too, and one an attempt named and never made. A lock still held, a stop
// or a delete that fails, is not known, with Tart's words; a clone a
// finished attempt kept for shell is left, and said (the lifecycle design's
// "Tart").
func TestATartStopRemovesTheClonesWorkAndLeavesAKeptOne(t *testing.T) {
	t.Parallel()
	clone := clonePrefix("run-1", "tahoe") + "-1"

	mac, p := stopping(t)
	mac.images = append(mac.images, clone)
	held := &lock{}
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: held})
	require.NoError(t, err)
	require.Empty(t, left)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.events, "stopped, then deleted")
	require.True(t, held.taken, "deleted holding the lock")
	require.NotContains(t, mac.images, clone)

	// The clone named and never made, or gone already: ended all the same.
	mac, p = stopping(t)
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.events, "asked, and nothing was there")
	// Nothing named at all: nothing was cloned, once the lock is had.
	nothing := &lock{}
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{ID: "tart_2"}, Lock: nothing})
	require.NoError(t, err)
	require.True(t, nothing.taken)

	// A tart run that still holds the lock: the VM was asked to stop, and
	// isn't known to be gone.
	mac, p = stopping(t)
	mac.images = append(mac.images, clone)
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{held: errors.New("its launch lock is still held after 1m0s, by a process that the dockhand which launched it, PID 7, now gone, started")}})
	require.ErrorContains(t, err, clone+" was asked to stop, and its launch lock is still held")
	require.Contains(t, mac.images, clone, "not deleted while something holds the lock")

	mac.stopErr = errors.New("tart: the VM didn't stop")
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.ErrorContains(t, err, "tart couldn't stop "+clone+": tart: the VM didn't stop")
	mac.stopErr, mac.deleteErr = nil, errors.New("tart: disk busy")
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.ErrorContains(t, err, "tart couldn't delete "+clone+": tart: disk busy")

	// A clone kept for shell is no unfinished work: left, and said.
	mac, p = stopping(t)
	mac.images = append(mac.images, clone)
	require.NoError(t, p.keepClone(clone, "dockhand-base-tahoe", tartvm.KeptFailed))
	left, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err)
	require.Equal(t, []string{"the clone " + clone + ", kept for dockhand shell, which dockhand clean removes"}, left)
	require.Empty(t, mac.events, "neither stopped nor deleted")

	// A name that isn't a check's clone is never touched.
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt("dockhand-base-tahoe"), Lock: &lock{}})
	require.ErrorContains(t, err, "dockhand-base-tahoe is not a check's clone")
}

// A kept marker is no proof the clone's tart run ended: the launch lock is
// taken first, and a lock still held is not known; the marker only changes
// what's said as left (Codex's review of #193 steps 5 and 6, finding 4).
func TestAKeptCloneStillNeedsItsWorkSeenEnded(t *testing.T) {
	t.Parallel()
	mac, p := stopping(t)
	clone := clonePrefix("run-1", "tahoe") + "-1"
	mac.images = append(mac.images, clone)
	require.NoError(t, p.keepClone(clone, "dockhand-base-tahoe", tartvm.KeptFailed))
	_, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{held: errors.New("its launch lock is still held")}})
	require.ErrorContains(t, err, "its launch lock is still held")
}

// fileLock is an attempt's launch lock on a real file, taken as a stop
// takes it: within its bound, while nothing holds it, and never from a
// holder.
type fileLock struct{ path string }

func (l fileLock) Hold(ctx context.Context, within time.Duration) (func(), error) {
	bounded, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	file, err := filelock.Acquire(bounded, l.path, filelock.Exclusive)
	if err != nil {
		return nil, err
	}
	return func() { _ = filelock.Release(file) }, nil
}

// orphan forks a child handed the launch lock at path, as tart clone is,
// and closes the launcher's own file, as a driver that died does: the
// child alone holds the lock until finish ends it.
func orphan(t *testing.T, path string) (finish func()) {
	t.Helper()
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	child := exec.Command("/bin/sh", "-c", "read line || true")
	child.ExtraFiles = []*os.File{file}
	input, err := child.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	require.NoError(t, file.Close(), "the driver is gone")
	var once sync.Once
	finish = func() {
		once.Do(func() {
			_ = input.Close()
			_ = child.Wait()
		})
	}
	t.Cleanup(finish)
	return finish
}

// A clone still being made by a child whose driver died: the stop asks the
// VM to stop, which isn't there yet, then waits for the lock the child
// holds, and deletes nothing meanwhile. Once the child has made the clone
// and ended, the clone is deleted. A child that outlives the stop's bound
// leaves it not known, its clone kept for the next ask, which removes it
// (the lifecycle design's "Tart" tests).
func TestATartStopWaitsOutACloneAChildOfADeadDriverIsMaking(t *testing.T) {
	t.Parallel()
	clone := clonePrefix("run-1", "tahoe") + "-1"

	mac, p := stopping(t)
	path := filepath.Join(t.TempDir(), "launch.lock")
	finish := orphan(t, path)
	type answer struct {
		left []string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: fileLock{path}})
		answered <- answer{left, err}
	}()
	require.Eventually(t, func() bool { return slices.Contains(mac.logged(), "stop "+clone) }, 10*time.Second, 10*time.Millisecond)
	select {
	case got := <-answered:
		t.Fatalf("the stop answered %v, %v while the child held the lock", got.left, got.err)
	case <-time.After(200 * time.Millisecond):
	}
	require.Equal(t, []string{"stop " + clone}, mac.logged(), "nothing deleted while the child clones")
	// The child finishes the clone, and ends.
	mac.mu.Lock()
	mac.images = append(mac.images, clone)
	mac.mu.Unlock()
	finish()
	got := <-answered
	require.NoError(t, got.err)
	require.Empty(t, got.left)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.logged())
	require.NotContains(t, mac.images, clone, "the clone the child made is deleted")

	// A child that outlives the bound: not known, and the clone stays.
	mac, p = stopping(t)
	path = filepath.Join(t.TempDir(), "launch.lock")
	finish = orphan(t, path)
	mac.images = append(mac.images, clone)
	bounded, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := p.Stop(bounded, buildenv.StopRequest{Execution: attempt(clone), Lock: fileLock{path}})
	require.ErrorContains(t, err, clone+" was asked to stop, and ")
	require.Contains(t, mac.images, clone, "not deleted while the child holds the lock")
	require.Equal(t, []string{"stop " + clone}, mac.logged())
	finish()
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: fileLock{path}})
	require.NoError(t, err, "the next ask, the child gone")
	require.NotContains(t, mac.images, clone)
}

// A clone made though Tart's answer to the clone was lost: the launch
// fails, having noted the clone before it asked, so the clone, which the
// failed launch doesn't delete, is the one a stop given that note removes
// (the lifecycle design's "Tart" tests).
func TestATartStopRemovesACloneMadeThoughItsReplyWasLost(t *testing.T) {
	t.Parallel()
	mac, p := stopping(t)
	mac.lostReply = errors.New("tart clone: the connection was lost")
	build := &fakeBuild{}
	err := p.Execute(t.Context(), tartJob(t, 1), build)
	require.Equal(t, refusal.Infrastructure, refusal.KindOf(err))
	clone := "dockhand-check-run-7-tahoe-1"
	require.Contains(t, mac.images, clone, "made, whatever Tart answered")
	require.NotContains(t, mac.events, "delete "+clone, "the launch took it as never made")
	require.Equal(t, []string{launchNote{Clone: clone}.String()}, build.launches, "noted before the clone was asked")

	execution := model.GuestExecution{ID: "ex_1", Attempt: 1, ProviderRef: build.refs[0], Launch: build.launches[0]}
	held := &lock{}
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: execution, Lock: held})
	require.NoError(t, err)
	require.Empty(t, left)
	require.True(t, held.taken)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.events[len(mac.events)-2:])
	require.NotContains(t, mac.images, clone)
}

// keptDirectory is where the Tart home's kept markers are.
func keptDirectory(t *testing.T, home string) string {
	t.Helper()
	canonical, err := tartvm.CanonicalDirectory(home)
	require.NoError(t, err)
	state, err := tartvm.StateDirectory(home)
	require.NoError(t, err)
	return filepath.Join(state, "tart-kept", model.Digest([]byte(canonical)))
}

// A clone deleted whose marker can't be removed is not known, with why,
// and the next ask, the marker removable, ends it. The marker here is the
// failure mark a removal that stopped halfway left beside no kept mark, in
// a directory that can't be written (the lifecycle design's "Tart" tests).
func TestATartStopWhoseMarkerFailsIsNotKnown(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root removes a file from a directory it can't write")
	}
	clone := clonePrefix("run-1", "tahoe") + "-1"
	mac, p := stopping(t)
	mac.images = append(mac.images, clone)
	require.NoError(t, tartvm.KeepClone(p.Tart.Home, clone, "dockhand-base-tahoe", tartvm.KeptFailed))
	kept := keptDirectory(t, p.Tart.Home)
	require.NoError(t, os.Remove(filepath.Join(kept, clone)))
	require.NoError(t, os.Chmod(kept, 0o500))
	t.Cleanup(func() { _ = os.Chmod(kept, 0o700) })

	_, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.ErrorContains(t, err, clone+" was deleted, and its marker couldn't be removed: ")
	require.ErrorIs(t, err, os.ErrPermission)
	require.Equal(t, []string{"stop " + clone, "delete " + clone}, mac.events)

	require.NoError(t, os.Chmod(kept, 0o700))
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err, "the next ask")
	require.Empty(t, left)
	require.NoFileExists(t, filepath.Join(kept, clone+".failed"))
}

// A check whose finished attempt kept its clone, beside another attempt
// whose driver died with its clone unfinished: each is asked, the kept one
// is left and said, and only the unfinished one is stopped and deleted
// (the lifecycle design's "Tart" tests).
func TestATartStopLeavesAKeptCloneBesideAnUnfinishedOne(t *testing.T) {
	t.Parallel()
	failed := guestprogram.Result{ID: "libharbor", Outcome: "failed", Phase: "install", Log: "target-1.log", Detail: "Failed to install libharbor"}
	mac, p := stopping(t)
	mac.guest.results = []guestprogram.Results{{Protocol: guestprogram.Protocol, State: "finished", Targets: []guestprogram.Result{failed}}}
	p.KeepFailed = true
	build := &fakeBuild{}
	require.NoError(t, p.Execute(t.Context(), tartJob(t, 1), build))
	kept := "dockhand-check-run-7-tahoe-1"
	_, marked := tartvm.KeptAt(p.Tart.Home, kept)
	require.True(t, marked, "the failed attempt kept its clone")
	finished := model.GuestExecution{ID: "ex_1", Attempt: 1, ProviderRef: kept, Launch: build.launches[0]}

	// The other release's driver died with its clone made and unfinished.
	unfinished := clonePrefix("run_7", "sequoia") + "-1"
	mac.images = append(mac.images, unfinished)
	other := model.GuestExecution{ID: "ex_2", Attempt: 1, ProviderRef: unfinished, Launch: launchNote{Clone: unfinished}.String()}
	attempts := []model.GuestExecution{finished, other}
	mac.events = nil

	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: finished, Attempts: attempts, Lock: &lock{}})
	require.NoError(t, err)
	require.Equal(t, []string{"the clone " + kept + ", kept for dockhand shell, which dockhand clean removes"}, left)
	left, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: other, Attempts: attempts, Lock: &lock{}})
	require.NoError(t, err)
	require.Empty(t, left)
	require.Equal(t, []string{"stop " + unfinished, "delete " + unfinished}, mac.events, "only the unfinished clone is touched")
	require.Contains(t, mac.images, kept)
	require.NotContains(t, mac.images, unfinished)
	_, marked = tartvm.KeptAt(p.Tart.Home, kept)
	require.True(t, marked, "the kept clone keeps its marker")
}

// clean and a stop each find what the other removed: a clone clean
// removed first is ended for the stop that follows, and one the stop
// removed is gone for the clean that follows, which the engine says as
// already removed. Which goes first is the check's lease's to settle,
// which the stop runs under and clean takes or skips the check for (the
// lifecycle design's "Tart").
func TestATartStopAndCleanEachFindWhatTheOtherRemoved(t *testing.T) {
	t.Parallel()
	clone := clonePrefix("run-1", "tahoe") + "-1"

	mac, p := stopping(t)
	mac.images = append(mac.images, clone)
	require.NoError(t, p.RemoveLeftover(t.Context(), clone))
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err, "clean removed it: ended")
	require.Empty(t, left)
	require.NotContains(t, mac.images, clone)

	mac, p = stopping(t)
	mac.images = append(mac.images, clone)
	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: attempt(clone), Lock: &lock{}})
	require.NoError(t, err)
	mac.events = nil
	require.ErrorIs(t, p.RemoveLeftover(t.Context(), clone), buildenv.ErrLeftoverGone)
	require.Empty(t, mac.events, "clean touches nothing the stop removed")
}

// clean and a retry removing the same kept clone, as a retry replaces the
// clones the branch's earlier checks kept: the one that has the clone's
// guard removes it, the other, meanwhile, is refused and touches nothing,
// and one after finds it gone. The clone is deleted once (the lifecycle
// design's "Tart" tests).
func TestCleanAndARetryRemoveAKeptCloneOnce(t *testing.T) {
	t.Parallel()
	clone := clonePrefix("run-1", "tahoe") + "-1"
	mac, p := stopping(t)
	mac.images = append(mac.images, clone)
	require.NoError(t, p.keepClone(clone, "dockhand-base-tahoe", tartvm.KeptFailed))

	asked, resume := make(chan struct{}), make(chan struct{})
	mac.onStop = func(string) {
		close(asked)
		<-resume
	}
	first := make(chan error, 1)
	go func() { first <- p.RemoveLeftover(t.Context(), clone) }()
	<-asked
	competing := p.RemoveLeftover(t.Context(), clone)
	require.ErrorIs(t, competing, buildenv.ErrLeftoverRunning, "refused while the other holds the guard")
	require.EqualError(t, competing, "in use by dockhand shell or another dockhand removing it", "a competing removal isn't called a shell")
	close(resume)
	require.NoError(t, <-first)
	mac.onStop = nil
	require.ErrorIs(t, p.RemoveLeftover(t.Context(), clone), buildenv.ErrLeftoverGone)

	deletes := 0
	for _, event := range mac.logged() {
		if event == "delete "+clone {
			deletes++
		}
	}
	require.Equal(t, 1, deletes)
	require.NotContains(t, mac.images, clone)
	_, marked := tartvm.KeptAt(p.Tart.Home, clone)
	require.False(t, marked, "its marker is forgotten")
}

// A kept clone whose marker can't be read is still kept: a stop leaves
// it, as the marker's presence alone says, and never deletes a clone the
// person asked to keep (architecture's ruling of 2026-11-24: KeptFrom read
// an unreadable marker as not kept, where KeptAt read it as kept).
func TestATartStopLeavesAKeptCloneWhoseMarkerCantBeRead(t *testing.T) {
	t.Parallel()
	mac, p := stopping(t)
	clone := clonePrefix("run_7", "tahoe") + "-1"
	mac.images = append(mac.images, clone)
	require.NoError(t, tartvm.KeepClone(p.Tart.Home, clone, "dockhand-base-tahoe", tartvm.KeptFailed))
	marker := filepath.Join(keptDirectory(t, p.Tart.Home), clone)
	require.NoError(t, os.Chmod(marker, 0o000))
	t.Cleanup(func() { _ = os.Chmod(marker, 0o644) })
	_, readable := os.ReadFile(marker)
	if readable == nil {
		t.Skip("this user reads a mode-000 file")
	}
	execution := model.GuestExecution{ID: "ex_1", Attempt: 1, ProviderRef: clone, Launch: launchNote{Clone: clone}.String()}
	mac.events = nil
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: execution, Attempts: []model.GuestExecution{execution}, Lock: &lock{}})
	require.NoError(t, err)
	require.Equal(t, []string{"the clone " + clone + ", kept for dockhand shell, which dockhand clean removes"}, left)
	require.Empty(t, mac.events, "neither stopped nor deleted")
	require.Contains(t, mac.images, clone)
}
