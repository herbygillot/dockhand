package local

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
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
	// The guest that builds, by the PID its first save recorded: once the
	// stop is through, it is gone. A pgrep of the directory's guest.tcl
	// also found the put-back, the same program run again from the same
	// directory by the driver's settle once the build ended, which a
	// loaded Mac could still be running (architecture's second sighting).
	results, err := (&guest.Follower{Root: dir}).Read(t.Context(), hostGuest{})
	require.NoError(t, err)
	building, err := strconv.Atoi(results.Environment["pid"])
	require.NoError(t, err)
	_, err = l.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, ProviderRef: dir, Launch: note}, Lock: pathLock(lock)})
	require.NoError(t, err)
	require.ErrorIs(t, syscall.Kill(building, 0), syscall.ESRCH, "the guest that built is gone once the stop is through")
	select {
	case <-done:
	case <-time.After(testsupport.Settle):
		t.Fatal("the check never ended")
	}
	require.Eventually(t, func() bool { return readRegistry(t, path) == "  zlib @1.3.2_0 (active)\n" }, testsupport.Settle, 10*time.Millisecond, "the prefix is as it was")
}

// A root build an older dockhand started isn't signalled: its stop isn't
// known, with the command to stop it by hand
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
	require.EqualError(t, err, "check-12 builds in /opt/local as root, and dockhand doesn't stop this root build: it was started before dockhand handed a root build its stop; stop it by hand, sudo kill -TERM -4321, then dockhand cancel check-12 asks again")

	_, err = l.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + filepath.Join(t.TempDir(), "gone") + `"}`}, Lock: lock})
	require.NoError(t, err)
}

// keepRootProof keeps a proof, proved or not, for the sudo at path, as the
// probe does.
func keepRootProof(t *testing.T, l *Local, sudo string, proved bool) {
	t.Helper()
	identity, err := sudoIdentity(sudo)
	require.NoError(t, err)
	data, err := json.Marshal(RootProof{Sudo: identity, Proved: proved, Found: []string{"without a terminal: standard input the lock: false"}, At: time.Now()})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(l.Proofs, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(l.Proofs, rootProofName), data, 0o600))
}

// A root build handed its stop is stopped only where this Mac's sudo has
// passed the probe: not probed, or failed, its stop isn't known, saying
// which, with what the probe found; proved, it is stopped as any build
// is, its starter's lock had (the lifecycle design's rule 4).
func TestARootBuildIsStoppedOnlyUnderAProvedSudo(t *testing.T) {
	t.Parallel()
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nexec \"$@\"\n")
	l := &Local{Temp: t.TempDir(), Proofs: t.TempDir(), sudo: []string{sudo}}
	dir := t.TempDir()
	owned, err := json.Marshal(owner{PID: 1, Check: "check-12", Prefix: "/opt/local", Root: true, RootStop: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	request := buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + dir + `"}`}, Lock: pathLock(filepath.Join(t.TempDir(), "attempt.lock"))}

	_, err = l.Stop(t.Context(), request)
	require.ErrorContains(t, err, "dockhand doesn't stop this root build: this Mac's sudo hasn't been probed yet")

	keepRootProof(t, l, sudo, false)
	_, err = l.Stop(t.Context(), request)
	require.ErrorContains(t, err, "this Mac's sudo failed the probe of a root build's stop: without a terminal: standard input the lock: false")

	keepRootProof(t, l, sudo, true)
	_, err = l.Stop(t.Context(), request)
	require.NoError(t, err, "no starter recorded itself, and the lock is had")
}

// A proved root build's stop sees its guest's group gone too: a group the
// guest leads that still has members, as something it started that let go
// of the lock would, isn't known to be gone, and isn't signalled.
func TestARootBuildsStopSeesItsGuestsGroupGone(t *testing.T) {
	t.Parallel()
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nexec \"$@\"\n")
	l := &Local{Temp: t.TempDir(), Locks: filepath.Join(t.TempDir(), "locks"), Proofs: t.TempDir(), sudo: []string{sudo}}
	keepRootProof(t, l, sudo, true)
	dir := t.TempDir()
	owned, err := json.Marshal(owner{PID: 1, Check: "check-12", Prefix: t.TempDir(), Root: true, RootStop: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	member := exec.Command("/bin/sleep", "60")
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, member.Start())
	t.Cleanup(func() { _ = member.Process.Kill(); _, _ = member.Process.Wait() })
	pid := strconv.Itoa(member.Process.Pid)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "results.json"), []byte(`{"environment":{"pid":"`+pid+`"}}`), 0o644))
	request := buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + dir + `"}`}, Lock: pathLock(filepath.Join(t.TempDir(), "attempt.lock"))}
	_, err = l.Stop(t.Context(), request)
	require.ErrorContains(t, err, "process group "+pid+" with members")
	require.ErrorContains(t, err, "stop it by hand")
	require.NoError(t, member.Process.Signal(syscall.Signal(0)), "not signalled")

	require.NoError(t, member.Process.Kill())
	_, _ = member.Process.Wait()
	_, err = l.Stop(t.Context(), request)
	require.NoError(t, err, "ended once the guest's group is gone")
}

// A proved root build its starter couldn't stop, as where sudo -n now asks
// for a password, is not known, with the one command to stop it by hand,
// as on a Mac never proved (the design's rule 4).
func TestARootStopNotSeenSaysHowToStopItByHand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "starter.json"), []byte(`{"pid":4321,"start":"x","boot":"b"}`), 0o644))
	made := owner{Check: "check-12", Prefix: "/opt/local", Root: true, RootStop: true}
	err := (&Local{}).rootStopUnseen(dir, made, errors.New("its starter, PID 4321, was asked to stop, and its launch lock is still held"))
	require.EqualError(t, err, "check-12 builds in /opt/local as root, and it was asked to stop but wasn't seen to end: its starter, PID 4321, was asked to stop, and its launch lock is still held; stop it by hand, sudo kill -TERM -4321, then dockhand cancel check-12 asks again")
}

// A root build stopped under a proved sudo is put back as root, under
// sudo -n; where sudo would ask, it's ended with the prefix left, its
// snapshot kept, and the words say how to put it back by hand (the
// lifecycle design's "Local and Script": a put-back as root needs sudo).
func TestARootPutBackWhereSudoWouldAskSaysHowByHand(t *testing.T) {
	t.Parallel()
	sudo := filepath.Join(t.TempDir(), "sudo")
	asked := filepath.Join(t.TempDir(), "asked")
	input := filepath.Join(t.TempDir(), "input")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\necho \"$*\" >> "+asked+"\necho \"$(/usr/bin/stat -f %r) $(/usr/bin/stat -f %r /dev/null)\" > "+input+"\necho 'sudo: a password is required' >&2\nexit 1\n")
	prefix := t.TempDir()
	l := &Local{Temp: t.TempDir(), Locks: filepath.Join(t.TempDir(), "locks"), Proofs: t.TempDir(), sudo: []string{sudo}}
	keepRootProof(t, l, sudo, true)
	dir := t.TempDir()
	owned, err := json.Marshal(owner{PID: 1, Check: "check-12", Prefix: prefix, Root: true, RootStop: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"local":{"note":"dockhand check-12 attempt 1"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "results.json"), []byte(`{"state":"running","environment":{"snapshot":"snap-1"}}`), 0o644))
	request := buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + dir + `"}`}, Lock: pathLock(filepath.Join(t.TempDir(), "attempt.lock"))}
	left, err := l.Stop(t.Context(), request)
	require.NoError(t, err, "the build ended")
	require.Len(t, left, 1)
	require.Contains(t, left[0], "sudo port restore --snapshot-id snap-1")
	said, err := os.ReadFile(asked)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(said), "-n "), "root was asked under sudo -n, never a prompt: %s", said)
	devices, err := os.ReadFile(input)
	require.NoError(t, err)
	both := strings.Fields(string(devices))
	require.Len(t, both, 2)
	require.Equal(t, both[1], both[0], "the put-back's guest is given /dev/null, and makes no check of it")
	_, err = os.Stat(dir)
	require.NoError(t, err, "the directory, its snapshot named, stays for undo")
}

// A stopped check whose prefix another check holds is ended with the
// prefix left: the put-back isn't run beside that check, nor assumed of
// it, and the words say the next check there, or undo, puts it back.
func TestAStopWhosePrefixAnotherCheckHoldsLeavesIt(t *testing.T) {
	t.Parallel()
	prefix := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "var", "macports", "registry"), 0o755))
	l := &Local{Temp: t.TempDir(), Locks: filepath.Join(t.TempDir(), "locks")}
	canonical, err := filepath.EvalSymlinks(prefix)
	require.NoError(t, err)
	held, err := filelock.AcquireAcrossUsers(t.Context(), filelock.Path(l.Locks, "prefix:"+canonical))
	require.NoError(t, err)
	defer held.Close()
	dir := t.TempDir()
	owned, err := json.Marshal(owner{PID: 1, Check: "check-12", Prefix: prefix})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "results.json"), []byte(`{"state":"running","environment":{"snapshot":"snap-1"}}`), 0o644))
	request := buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, Launch: `{"directory":"` + dir + `"}`}, Lock: pathLock(filepath.Join(t.TempDir(), "attempt.lock"))}
	left, err := l.Stop(t.Context(), request)
	require.NoError(t, err, "the build ended")
	require.Len(t, left, 1)
	require.Contains(t, left[0], "which check-12 changed, held now by")
	require.Contains(t, left[0], "the next check in it puts it back before it builds, or dockhand undo check-12")
	_, err = os.Stat(dir)
	require.NoError(t, err, "its directory stays for the next check")
}
