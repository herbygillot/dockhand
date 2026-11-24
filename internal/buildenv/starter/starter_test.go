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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestMain(m *testing.M) {
	RunIfAsked()
	if dir := os.Getenv("STARTER_TEST_DRIVER"); dir != "" {
		os.Exit(drive(dir))
	}
	os.Exit(m.Run())
}

// drive is a driver in a process of its own, as dockhand's is: it takes
// the attempt's lock, starts a build through the starter, lets go of its
// own copy, and waits to be killed.
func drive(dir string) int {
	file, err := filelock.Acquire(context.Background(), filepath.Join(dir, "attempt.lock"), filelock.Exclusive)
	if err != nil {
		return 1
	}
	program, err := Self()
	if err != nil {
		return 1
	}
	record := filepath.Join(dir, "record")
	if err := os.MkdirAll(record, 0o755); err != nil {
		return 1
	}
	if _, err := Start(Spec{Program: program, Directory: record, Signal: syscall.SIGTERM, Grace: 2 * time.Second, Lock: file, Command: []string{"/bin/sleep", "60"}}); err != nil {
		return 1
	}
	_ = file.Close()
	select {}
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

// output is a build's output as the test reads it while the build writes.
type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// launched starts command through the starter, as a launcher does: holding
// the attempt's lock, handing it on, and letting go of its own file once
// the starter is forked.
func launched(t *testing.T, command ...string) (*exec.Cmd, string, lockAt, *output) {
	t.Helper()
	return launchedWith(t, false, command...)
}

// launchedWith is launched, with the lock handed to the command as its
// standard input where stdinLock, as a root build's sudo is.
func launchedWith(t *testing.T, stdinLock bool, command ...string) (*exec.Cmd, string, lockAt, *output) {
	t.Helper()
	return launchedSpec(t, func(spec *Spec) { spec.StdinLock = stdinLock }, command...)
}

// launchedSpec is launched, with the spec changed by change first.
func launchedSpec(t *testing.T, change func(*Spec), command ...string) (*exec.Cmd, string, lockAt, *output) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	program, err := Self()
	require.NoError(t, err)
	var out output
	spec := Spec{Program: program, Directory: dir, Signal: syscall.SIGTERM, Grace: 2 * time.Second, Lock: file, Command: command, Stdout: &out, Stderr: &out}
	change(&spec)
	started, err := Start(spec)
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
	// A directory where the record can't go: a directory stands in its
	// place.
	blocked := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(blocked, RecordName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, RecordName, "occupied"), nil, 0o644))
	started, err := Start(Spec{Program: program, Directory: blocked, Signal: syscall.SIGTERM, Grace: time.Second, Lock: file,
		Command: []string{"/usr/bin/touch", marker}, Stdout: &out, Stderr: &out})
	require.NoError(t, err)
	err = started.Wait()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit))
	require.Equal(t, noRecord, exit.ExitCode())
	require.Contains(t, out.String(), "couldn't record itself, so nothing was started")
	require.NoFileExists(t, marker)
	// Its reason is read back from what it said, for the error that names
	// it (#129's second round, 5).
	reason, ok := Unrecorded(err, append([]byte("building from x\n"), out.Bytes()...))
	require.True(t, ok)
	require.Contains(t, reason, "the starter couldn't record itself")
	_, ok = Unrecorded(errors.New("exit status 1"), out.Bytes())
	require.False(t, ok, "only the starter's own exit for it")
	// A build script that itself exits 70 isn't the starter failing: with
	// none of the starter's own words it's no unrecorded start (#243).
	_, ok = Unrecorded(err, []byte("building from x\nmake: *** [all] Error 70\n"))
	require.False(t, ok, "a script's own exit 70, without the starter's line")
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

// hookedLock runs before when the stop asks for the lock.
type hookedLock struct {
	path   string
	before func()
}

func (l hookedLock) Hold(ctx context.Context, bound time.Duration) (func(), error) {
	l.before()
	return lockAt(l.path).Hold(ctx, bound)
}

// A starter forked holding the lock, paused before its first instruction,
// can record itself and leave a child in its group while the stop waits for
// the lock: the stop reads the record again once it has the lock, and asks
// that group, never calling it ended unseen (Codex's review of #193 steps 5
// and 6, finding 1).
func TestAStopReadsTheRecordAgainOnceItHasTheLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	defer file.Close()
	program, err := Self()
	require.NoError(t, err)
	cmd, err := Start(Spec{Program: Program{Path: "/bin/sh", Args: []string{"-c", `kill -STOP $$; exec "$@"`, "sh", program.Path}},
		Directory: dir, Signal: syscall.SIGTERM, Grace: time.Second, Lock: file,
		Command: []string{"/bin/sh", "-c", "/bin/sleep 60 </dev/null >/dev/null 2>&1 &"}})
	require.NoError(t, err)
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL); _ = cmd.Process.Kill() })
	require.NoError(t, file.Close())
	require.Eventually(t, func() bool {
		out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
		return err == nil && strings.Contains(string(out), "T")
	}, 5*time.Second, time.Millisecond)
	hook := hookedLock{path: path, before: func() {
		require.NoError(t, cmd.Process.Signal(syscall.SIGCONT))
		require.NoError(t, cmd.Wait())
		_, found, err := ReadRecord(dir)
		require.NoError(t, err)
		require.True(t, found)
	}}
	err = Stop(t.Context(), dir, hook, time.Second)
	require.NoError(t, syscall.Kill(-pid, 0), "its child runs still, unsignalled")
	require.ErrorContains(t, err, "process group "+strconv.Itoa(pid)+" still has processes")
}

// A root build's command is handed the attempt's locked file as its
// standard input, through a sudo that leaves it as it came: the command
// reads that its standard input is the lock's file, and holds the lock
// for as long as it lives, after the starter has ended; free once it has
// (the lifecycle design's rule 4).
func TestARootBuildIsHandedTheLockAsItsStandardInput(t *testing.T) {
	t.Parallel()
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n")
	report := filepath.Join(t.TempDir(), "stdin")
	release := filepath.Join(t.TempDir(), "release")
	started, _, lock, out := launchedWith(t, true, sudo, "-n", "/bin/sh", "-c",
		"/usr/bin/stat -f '%d %i' > "+report+".tmp; mv "+report+".tmp "+report+"; (while [ ! -e "+release+" ]; do sleep 0.05; done) <&0 >/dev/null 2>&1 &")
	require.Eventually(t, func() bool { _, err := os.Stat(report); return err == nil }, 10*time.Second, 10*time.Millisecond, "%s", out)
	said, err := os.ReadFile(report)
	require.NoError(t, err)
	var held syscall.Stat_t
	require.NoError(t, syscall.Stat(string(lock), &held))
	require.Equal(t, fmt.Sprintf("%d %d", held.Dev, held.Ino), strings.TrimSpace(string(said)), "the command's standard input is the lock's file")
	_ = started.Wait()
	_, err = lock.Hold(t.Context(), 200*time.Millisecond)
	require.Error(t, err, "the build's own child kept its standard input, and holds the lock after the starter ended")
	require.NoError(t, os.WriteFile(release, nil, 0o644))
	unlock, err := lock.Hold(t.Context(), 10*time.Second)
	require.NoError(t, err, "free once it has ended")
	unlock()
}

// rootStandIns are a sudo that ignores SIGTERM, as a root sudo this process
// can't signal is, for the starter's purpose, and passes its standard input
// on, and a root stop that logs what it was asked and sends it as this
// user: the starter's sequence for a root build, without root.
func rootStandIns(t *testing.T) (sudo, rootStop, asked string) {
	t.Helper()
	dir := t.TempDir()
	sudo, rootStop, asked = filepath.Join(dir, "sudo"), filepath.Join(dir, "root-stop"), filepath.Join(dir, "asked")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\n[ \"$1\" = -n ] && shift\ntrap '' TERM\n\"$@\"\n")
	testsupport.WriteExecutable(t, rootStop, "#!/bin/sh\n[ \"$1\" = --stop ] || exit 2\necho \"$2 $3 $4\" >> "+asked+"\n"+
		"case $3 in group) /bin/kill -$2 -- -$4 ;; *) /bin/kill -$2 $4 ;; esac\n")
	return sudo, rootStop, asked
}

// guestSays is a stand-in guest's script: it records its PID in the
// build's results, whole, by a rename, as the guest's first save does,
// then waits.
func guestSays(dir string) string {
	results := filepath.Join(dir, "results.json")
	return "printf '{\"environment\":{\"pid\":\"%s\"}}' $$ > " + results + ".tmp && mv " + results + ".tmp " + results + "; exec /bin/sleep 60"
}

// A root build's sudo that doesn't end on SIGTERM has its guest sent
// root's SIGKILL by the root stop, by the PID the guest recorded and its
// start time; once the sudo has ended, the starter's group is sent root's
// SIGKILL too, and the stop sees the build end (the lifecycle design's
// rule 4, ruling of 2026-11-23).
func TestARootBuildsGuestIsKilledByTheRootStop(t *testing.T) {
	t.Parallel()
	sudo, rootStop, asked := rootStandIns(t)
	var dir string
	started, dir, lock, out := launchedSpec(t, func(spec *Spec) {
		spec.StdinLock, spec.RootStop = true, []string{rootStop}
		dir = spec.Directory
		spec.Command = []string{sudo, "-n", "/bin/sh", "-c", guestSays(dir)}
	})
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "results.json")); return err == nil }, 10*time.Second, 10*time.Millisecond, "%s", out)
	waited := make(chan error, 1)
	go func() { waited <- started.Wait() }()
	require.NoError(t, Stop(t.Context(), dir, lock, 15*time.Second), "%s", out)
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("the starter never ended")
	}
	said, err := os.ReadFile(asked)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(said)), "\n")
	require.Len(t, lines, 2, "%s", said)
	require.Regexp(t, `^KILL process [0-9]+$`, lines[0], "the guest, alone: it leads no group of its own")
	require.Equal(t, "KILL group "+strconv.Itoa(started.Process.Pid), lines[1], "the starter's group, once the sudo ended")
}

// A root build whose guest recorded nothing, and whose sudo doesn't end,
// is never signalled past its sudo: the starter stays, keeping the lock,
// and the stop isn't known.
func TestARootBuildThatRecordedNothingKeepsItsLock(t *testing.T) {
	t.Parallel()
	sudo, rootStop, asked := rootStandIns(t)
	marker := filepath.Join(t.TempDir(), "started")
	started, dir, lock, out := launchedSpec(t, func(spec *Spec) {
		spec.StdinLock, spec.RootStop, spec.Grace = true, []string{rootStop}, 200*time.Millisecond
	}, sudo, "-n", "/bin/sh", "-c", "touch "+marker+"; exec /bin/sleep 60")
	// The stand-in sudo ignores SIGTERM once it runs its command, as a root
	// one can't be sent it.
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 10*time.Second, 10*time.Millisecond, "the build started")
	err := Stop(t.Context(), dir, lock, 2*time.Second)
	require.ErrorContains(t, err, "was asked to stop", "%s", out)
	require.NoError(t, started.Process.Signal(syscall.Signal(0)), "the starter stays")
	_, err = os.Stat(asked)
	require.ErrorIs(t, err, os.ErrNotExist, "the root stop was never asked: the sudo was this user's to signal, and nothing else was known")
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "keeps its lock")
	}, 5*time.Second, 10*time.Millisecond, "the starter said why it stays")
}

// A root stop sudo -n refuses, as on a Mac whose rule was taken away after
// its probe passed, kills nothing: the starter stays, keeping the lock,
// and the stop answers not known within its bound, never ended, and never
// waits on a prompt (architecture's question of 2026-11-24).
func TestARootStopSudoRefusesIsNotKnown(t *testing.T) {
	t.Parallel()
	sudo, _, _ := rootStandIns(t)
	refused := filepath.Join(t.TempDir(), "root-stop")
	asked := filepath.Join(t.TempDir(), "asked")
	input := filepath.Join(t.TempDir(), "input")
	testsupport.WriteExecutable(t, refused, "#!/bin/sh\necho \"$*\" >> "+asked+"\necho \"$(/usr/bin/stat -f %r) $(/usr/bin/stat -f %r /dev/null)\" > "+input+"\necho 'sudo: a password is required' >&2\nexit 1\n")
	var dir string
	started, dir, lock, out := launchedSpec(t, func(spec *Spec) {
		spec.StdinLock, spec.RootStop, spec.Grace = true, []string{refused}, 200*time.Millisecond
		dir = spec.Directory
		spec.Command = []string{sudo, "-n", "/bin/sh", "-c", guestSays(dir)}
	})
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "results.json")); return err == nil }, 10*time.Second, 10*time.Millisecond, "%s", out)
	begun := time.Now()
	err := Stop(t.Context(), dir, lock, 3*time.Second)
	require.ErrorContains(t, err, "was asked to stop", "not known: %s", out)
	require.Less(t, time.Since(begun), 10*time.Second, "within its bound, with no prompt waited on")
	require.NoError(t, started.Process.Signal(syscall.Signal(0)), "the starter stays")
	// The starter asks root after its grace, which a loaded Mac may reach
	// after the stop's bound: the ask is waited for, not assumed done.
	var said []byte
	require.Eventually(t, func() bool { said, err = os.ReadFile(asked); return err == nil }, 20*time.Second, 20*time.Millisecond, "root's SIGKILL to the guest was asked")
	require.Regexp(t, `^--stop KILL process [0-9]+ [0-9]+\n$`, string(said), "root's SIGKILL to the guest was asked once, refused, and nothing after")
	time.Sleep(500 * time.Millisecond)
	again, err := os.ReadFile(asked)
	require.NoError(t, err)
	require.Equal(t, string(said), string(again), "nothing asked after the refusal")
	require.NoError(t, started.Process.Signal(syscall.Signal(0)), "the starter still stays")
	var both []string
	require.Eventually(t, func() bool {
		devices, _ := os.ReadFile(input)
		both = strings.Fields(string(devices))
		return len(both) == 2
	}, 10*time.Second, 20*time.Millisecond, "the root stop said what its input was")
	require.Len(t, both, 2)
	require.Equal(t, both[1], both[0], "the stop-only guest is given /dev/null")
}

// A driver killed after its starter recorded itself leaves the build
// running in the starter's own session; a stop from another process finds
// it by the record, and sees it end (the design's Tests: "the driver
// killed ... after" the starter's record).
func TestAStopAfterTheDriverWasKilledSeesTheBuildEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	driver := exec.Command(os.Args[0])
	driver.Env = append(os.Environ(), "STARTER_TEST_DRIVER="+dir)
	require.NoError(t, driver.Start())
	record := filepath.Join(dir, "record")
	require.Eventually(t, func() bool {
		_, found, _ := ReadRecord(record)
		return found
	}, 10*time.Second, 10*time.Millisecond, "the starter recorded itself")
	require.NoError(t, driver.Process.Kill())
	_, _ = driver.Process.Wait()
	starter, found, err := ReadRecord(record)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, syscall.Kill(starter.PID, 0), "the build outlived its driver")
	require.NoError(t, Stop(t.Context(), record, lockAt(filepath.Join(dir, "attempt.lock")), 10*time.Second))
	require.ErrorIs(t, syscall.Kill(-starter.PID, 0), syscall.ESRCH, "the group is gone")
}

// sessionOf is a command run in a session of its own, keeping its
// standard input, as a sudo may run its command.
func sessionOf(command string) string {
	return "/opt/local/bin/perl -e 'use POSIX; POSIX::setsid(); exec @ARGV' /bin/sh -c '" + command + "'"
}

// Codex's shapes under sudo: a guest in a session of its own that recorded
// nothing, its starter and its sudo gone however each went, killed, dead
// of another signal, or left after only signalling its command, holds the
// lock it was handed: the stop is not known while it lives, and ended once
// it is gone (the design's Tests, A6).
func TestAGuestLeftInASessionOfItsOwnKeepsTheStopNotKnown(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/opt/local/bin/perl"); err != nil {
		t.Skip("MacPorts' perl makes the guest's session")
	}
	for _, shape := range []struct{ name, sudo string }{
		{"killed", "#!/bin/sh\n[ \"$1\" = -n ] && shift\n\"$@\"\n"},
		{"dead of another signal", "#!/bin/sh\n[ \"$1\" = -n ] && shift\n\"$@\"\n"},
		{"left after signalling its command", "#!/bin/sh\n[ \"$1\" = -n ] && shift\n\"$@\" &\ncommand=$!\ntrap 'kill -TERM $command; exit 1' TERM\nwait\n"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			sudo := filepath.Join(t.TempDir(), "sudo")
			testsupport.WriteExecutable(t, sudo, shape.sudo)
			pidFile := filepath.Join(t.TempDir(), "guest")
			started, dir, lock, out := launchedSpec(t, func(spec *Spec) {
				spec.StdinLock, spec.Grace = true, 200*time.Millisecond
			}, sudo, "-n", "/bin/sh", "-c", sessionOf("trap \"\" TERM HUP INT; echo $$ > "+pidFile+".tmp; mv "+pidFile+".tmp "+pidFile+"; exec /bin/sleep 60"))
			require.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, 10*time.Second, 10*time.Millisecond, "%s", out)
			data, err := os.ReadFile(pidFile)
			require.NoError(t, err)
			guest, err := strconv.Atoi(strings.TrimSpace(string(data)))
			require.NoError(t, err)
			t.Cleanup(func() { _ = syscall.Kill(guest, syscall.SIGKILL) })
			sudoPID := 0
			require.Eventually(t, func() bool {
				listed, _ := exec.Command("/usr/bin/pgrep", "-P", strconv.Itoa(started.Process.Pid)).Output()
				sudoPID, _ = strconv.Atoi(strings.TrimSpace(string(listed)))
				return sudoPID > 0
			}, 10*time.Second, 10*time.Millisecond)
			switch shape.name {
			case "killed":
				require.NoError(t, started.Process.Kill())
				_ = syscall.Kill(sudoPID, syscall.SIGKILL)
			case "dead of another signal":
				require.NoError(t, syscall.Kill(sudoPID, syscall.SIGSEGV))
			case "left after signalling its command":
				require.NoError(t, syscall.Kill(sudoPID, syscall.SIGTERM))
			}
			err = Stop(t.Context(), dir, lock, time.Second)
			require.Error(t, err, "not known while the guest lives")
			require.NoError(t, syscall.Kill(guest, 0), "the guest lives, never signalled")
			require.NoError(t, syscall.Kill(guest, syscall.SIGKILL))
			require.Eventually(t, func() bool { return syscall.Kill(guest, 0) != nil }, 10*time.Second, 10*time.Millisecond)
			_ = syscall.Kill(started.Process.Pid, syscall.SIGKILL)
			_, _ = started.Process.Wait()
			require.NoError(t, Stop(t.Context(), dir, lock, 10*time.Second), "ended once it is gone")
		})
	}
}

// A guest that leads a group of its own is sent root's SIGKILL as that
// group, by the root stop, its PID and start time tested first.
func TestARootBuildsGuestLeadingAGroupIsKilledAsTheGroup(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/opt/local/bin/perl"); err != nil {
		t.Skip("MacPorts' perl makes the guest's group")
	}
	sudo, rootStop, asked := rootStandIns(t)
	var dir string
	started, dir, lock, out := launchedSpec(t, func(spec *Spec) {
		spec.StdinLock, spec.RootStop, spec.Grace = true, []string{rootStop}, 500*time.Millisecond
		dir = spec.Directory
		results := filepath.Join(dir, "results.json")
		spec.Command = []string{sudo, "-n", "/opt/local/bin/perl", "-e", "setpgrp(0,0); exec @ARGV", "/bin/sh", "-c",
			"printf '{\"environment\":{\"pid\":\"%s\"}}' $$ > " + results + ".tmp && mv " + results + ".tmp " + results + "; exec /bin/sleep 60"}
	})
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "results.json")); return err == nil }, 10*time.Second, 10*time.Millisecond, "%s", out)
	waited := make(chan error, 1)
	go func() { waited <- started.Wait() }()
	require.NoError(t, Stop(t.Context(), dir, lock, 15*time.Second), "%s", out)
	<-waited
	said, err := os.ReadFile(asked)
	require.NoError(t, err)
	require.Regexp(t, `^KILL group [0-9]+\n`, string(said), "the guest's own group")
}
