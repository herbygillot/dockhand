package starter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
)

func TestMain(m *testing.M) {
	RunIfAsked()
	os.Exit(m.Run())
}

// lockAt is a launch lock by its path, as the checks service's is.
type lockAt string

func (l lockAt) Hold(ctx context.Context, within time.Duration) (func(), error) {
	bounded, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	file, err := filelock.Acquire(bounded, string(l), filelock.Exclusive)
	if err != nil {
		return nil, fmt.Errorf("its launch lock is still held: %w", err)
	}
	return func() { _ = filelock.Release(file) }, nil
}

// launched starts command through the starter, as a launcher does: holding
// the attempt's lock, handing it on, and letting go of its own file once
// the starter is forked.
func launched(t *testing.T, command ...string) (*exec.Cmd, string, lockAt, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	program, err := Self()
	require.NoError(t, err)
	var out bytes.Buffer
	started, err := Start(Spec{Program: program, Directory: dir, Signal: syscall.SIGTERM, Grace: 2 * time.Second, Lock: file, Command: command, Stdout: &out, Stderr: &out})
	require.NoError(t, err)
	require.NoError(t, file.Close(), "the launcher lets go; the starter holds it")
	t.Cleanup(func() {
		_ = syscall.Kill(-started.Process.Pid, syscall.SIGKILL)
		_, _ = started.Process.Wait()
	})
	require.Eventually(t, func() bool {
		_, found, _ := ReadRecord(dir)
		return found
	}, 10*time.Second, 10*time.Millisecond, "the starter recorded itself")
	return started, dir, lockAt(path), &out
}

// The starter records itself, its PID and its own start time, the boot and
// the lock, before it starts anything, then runs the build as its child and
// ends with the child's status (the lifecycle design's rules 1 and 2).
func TestTheStarterRecordsItselfAndRunsTheBuild(t *testing.T) {
	t.Parallel()
	started, dir, lock, out := launched(t, "/bin/sh", "-c", "echo built; exit 3")
	err := started.Wait()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 3, exit.ExitCode(), "the build's status")
	require.Equal(t, "built\n", out.String())
	record, found, err := ReadRecord(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, started.Process.Pid, record.PID)
	start, err := procid.Start(started.Process.Pid)
	if err == nil {
		require.Equal(t, start, record.Start, "the system's own start time for it")
	}
	require.Equal(t, string(lock), record.Lock)
	boot, err := procid.BootSession()
	require.NoError(t, err)
	require.Equal(t, boot, record.Boot)
}

// A stop signals the starter, which stops its group and ends; the stop sees
// the end, the lock free and the group gone: ended. A shell that handed
// over to another program is in the group all the same.
func TestAStopSeesTheBuildEnd(t *testing.T) {
	t.Parallel()
	started, dir, lock, _ := launched(t, "/bin/sh", "-c", "exec /bin/sleep 60")
	waited := make(chan error, 1)
	go func() { waited <- started.Wait() }()
	require.NoError(t, Stop(t.Context(), dir, lock, 10*time.Second))
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("the starter never ended")
	}
	require.ErrorIs(t, syscall.Kill(-started.Process.Pid, 0), syscall.ESRCH, "the group is gone")
}

// A starter killed outright leaves its child in its group: the lock is free,
// since the starter kept it to itself, and the group has a member, which
// the stop names and doesn't signal, since it can't be told from another
// group that took the number: not known.
func TestAStarterKilledOutrightLeavesItsGroupNamed(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "started")
	started, dir, lock, _ := launched(t, "/bin/sh", "-c", "touch "+marker+"; exec /bin/sleep 60")
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 10*time.Second, 10*time.Millisecond, "the build started")
	require.NoError(t, started.Process.Signal(syscall.SIGKILL))
	_, _ = started.Process.Wait()
	err := Stop(t.Context(), dir, lock, 2*time.Second)
	require.ErrorContains(t, err, "process group "+strconv.Itoa(started.Process.Pid)+" still has processes after its starter ended")
	require.NoError(t, syscall.Kill(-started.Process.Pid, 0), "its member wasn't signalled")
}

// A stop that can't have the lock, the starter alive and holding it while
// its build ignores the stop past the grace? No: a starter always ends by
// its group's SIGKILL. Something else holding the lock, as a launcher still
// launching, is not known.
func TestAStopWithoutTheLockIsNotKnown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	defer file.Close()
	err = Stop(t.Context(), dir, lockAt(path), 100*time.Millisecond)
	require.ErrorContains(t, err, "no starter recorded itself, and its launch lock is still held")
	require.NoError(t, file.Close())
	require.NoError(t, Stop(t.Context(), dir, lockAt(path), time.Second), "no record and the lock had: no starter ever ran it")
}

// A record that can't be read is an error, never "no record"; one from
// before the Mac's last restart is ended; one naming a PID whose start time
// isn't its own signals nothing.
func TestARecordIsReadForWhatItIs(t *testing.T) {
	t.Parallel()
	lock := lockAt(filepath.Join(t.TempDir(), "attempt.lock"))
	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, RecordName), []byte("{"), 0o644))
	require.ErrorContains(t, Stop(t.Context(), broken, lock, time.Second), "can't be read")

	before := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(before, RecordName), []byte(fmt.Sprintf(`{"pid":%d,"start":"x","boot":"another boot"}`, os.Getpid())), 0o644))
	require.NoError(t, Stop(t.Context(), before, lock, time.Second))

	// This test's own PID, with another start time: never signalled, and
	// no group carries its number.
	reused := t.TempDir()
	boot, err := procid.BootSession()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(reused, RecordName), []byte(fmt.Sprintf(`{"pid":%d,"start":"not its own","boot":%q}`, os.Getpid(), boot)), 0o644))
	err = Stop(t.Context(), reused, lock, time.Second)
	if err != nil {
		require.True(t, strings.Contains(err.Error(), "process group"), err.Error())
	}
}

// A starter that can't save its record starts nothing.
func TestAStarterThatCantRecordItselfStartsNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	defer file.Close()
	program, err := Self()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "ran")
	var out bytes.Buffer
	// A directory under a file can't be made, nor written in.
	blocked := filepath.Join(t.TempDir(), "a file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))
	started, err := Start(Spec{Program: program, Directory: filepath.Join(blocked, "deeper"), Signal: syscall.SIGTERM, Grace: time.Second, Lock: file,
		Command: []string{"/usr/bin/touch", marker}, Stdout: &out, Stderr: &out})
	require.NoError(t, err)
	err = started.Wait()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit))
	require.Equal(t, noRecord, exit.ExitCode())
	require.Contains(t, out.String(), "couldn't record itself, so nothing was started")
	require.NoFileExists(t, marker)
}

// A directory whose starter holds its lock is being built in, from the
// starter's fork; once it ends, it isn't.
func TestADirectoryIsBuiltInWhileItsStarterHoldsTheLock(t *testing.T) {
	t.Parallel()
	started, dir, lock, _ := launched(t, "/bin/sleep", "60")
	require.True(t, Building(t.Context(), dir))
	go func() { _ = started.Wait() }()
	require.NoError(t, Stop(t.Context(), dir, lock, 10*time.Second))
	require.False(t, Building(t.Context(), dir))
	require.False(t, Building(t.Context(), t.TempDir()), "no record, no build")
}
