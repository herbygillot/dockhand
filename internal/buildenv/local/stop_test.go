package local

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// pathLock is an attempt's launch lock by its path.
type pathLock string

func (l pathLock) Hold(ctx context.Context, within time.Duration) (func(), error) {
	bounded, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	file, err := filelock.Acquire(bounded, string(l), filelock.Exclusive)
	if err != nil {
		return nil, err
	}
	return func() { _ = filelock.Release(file) }, nil
}

// A cancel stops a local check's guest program, found by the directory its
// attempt noted: its starter is signalled, stops its group, and the stop
// sees the group gone and the lock free; the prefix is as it was before
// the check (the lifecycle design's "Local and Script").
func TestALocalStopEndsTheGuestAndThePrefixIsPutBack(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active, "HANG=fetch:harbor-cli")
	l := localProvider(t, env)
	build := &fakeBuild{}
	done := make(chan error, 1)
	go func() { done <- l.Execute(context.WithoutCancel(t.Context()), localJob(t, prefix), build) }()
	require.Eventually(t, func() bool { return strings.Contains(readRegistry(t, path), "libharbor") }, testsupport.Settle, 10*time.Millisecond)
	build.mu.Lock()
	note, lock, dir := build.launches[0], build.lock, build.refs[0]
	build.mu.Unlock()
	_, err := l.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, ProviderRef: dir, Launch: note}, Lock: pathLock(lock)})
	require.NoError(t, err)
	require.Error(t, exec.Command("pgrep", "-f", filepath.Join(dir, "guest.tcl")).Run(), "nothing of the guest runs")
	select {
	case <-done:
	case <-time.After(testsupport.Settle):
		t.Fatal("the check never ended")
	}
	require.Eventually(t, func() bool { return readRegistry(t, path) == "  zlib @1.3.2_0 (active)\n" }, testsupport.Settle, 10*time.Millisecond, "the prefix is as it was")
}

// A root build isn't signalled until this Mac's sudo has passed the
// design's test: its stop isn't known, with the command to stop it by hand
// and the one to run after. A directory that's gone, or that was never
// made, is ended once the lock is had.
func TestARootBuildsStopSaysHowToStopItByHand(t *testing.T) {
	t.Parallel()
	l := &Local{}
	dir := t.TempDir()
	owned, err := json.Marshal(owner{PID: 1, Check: "check-12", Prefix: "/opt/local", Root: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "starter.json"), []byte(`{"pid":4321,"start":"x","boot":"b"}`), 0o644))
	lock := pathLock(filepath.Join(t.TempDir(), "attempt.lock"))
	_, err = l.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + dir + `"}`}, Lock: lock})
	require.EqualError(t, err, "check-12 builds in /opt/local as root, and dockhand doesn't stop a root build until this Mac's sudo has passed its test of a stop; stop it by hand, sudo kill -TERM -4321, then dockhand cancel check-12 asks again")

	_, err = l.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + filepath.Join(t.TempDir(), "gone") + `"}`}, Lock: lock})
	require.NoError(t, err)
}
