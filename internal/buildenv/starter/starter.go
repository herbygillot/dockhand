// Package starter is the process a Local or a Script build is started
// through, and the stop that sees it end (the lifecycle design's "Local and
// Script: which processes, and when they are gone"). The launcher's child
// is dockhand itself, run as Command: it leads a session of its own, holds
// the attempt's launch lock from the moment it is forked, records itself
// in the build's directory, and runs the build's command as its child, in
// its own group, staying until that child has ended. Stop signals it by
// its record, and sees the end: the launch lock free and the group gone.
package starter

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
)

// Command is the hidden argument that makes dockhand the starter, ahead of
// every other: main hands the rest to Main before anything else runs.
const Command = "__starter"

// RecordName is the starter's record in the build's directory.
const RecordName = "starter.json"

// Record is what the starter writes of itself before it starts anything:
// its PID and the system's own start time for it (procid), so a later
// process with its PID isn't taken for it; the boot it ran in, so one from
// before a restart is known to be gone; and the launch lock's path, which
// it holds for as long as it lives, so a reader of the directory can tell
// it is still being built in.
type Record struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Boot  string `json:"boot"`
	Lock  string `json:"lock"`
}

// Program is how a launcher runs dockhand as the starter: the executable,
// the arguments that come before Command's (none for dockhand itself), and
// what to add to the environment, as a test binary standing in for
// dockhand needs.
type Program struct {
	Path string
	Args []string
	Env  []string
}

// Spec is a build to start through the starter.
type Spec struct {
	Program Program
	// Directory is where the starter records itself.
	Directory string
	// Signal is the stop the starter sends its group, SIGTERM for Local and
	// SIGINT for Script, and Grace how long it waits before the last
	// SIGKILL to its own group.
	Signal syscall.Signal
	Grace  time.Duration
	// Lock is the attempt's launch lock's file, which the starter is
	// handed and holds from its fork.
	Lock *os.File
	// Command is the build's, run as the starter's child, in Dir with Env,
	// this process's environment where Env is nil.
	Command []string
	Dir     string
	Env     []string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Start forks the starter, which holds the lock from that moment, and
// returns it running: the caller waits for it. A spec with no program is
// dockhand's own fault.
func Start(spec Spec) (*exec.Cmd, error) {
	if spec.Program.Path == "" {
		return nil, buildenv.ErrDockhandFault.Because("a build was started with no starter program")
	}
	if spec.Lock == nil {
		return nil, buildenv.ErrDockhandFault.Because("a build was started with no launch lock")
	}
	args := append([]string(nil), spec.Program.Args...)
	args = append(args, Command, "-record", spec.Directory, "-signal", strconv.Itoa(int(spec.Signal)), "-grace", spec.Grace.String(), "-lock", spec.Lock.Name(), "--")
	command := exec.Command(spec.Program.Path, append(args, spec.Command...)...)
	command.Dir = spec.Dir
	// No Env and no program's leaves the starter this process's, as exec
	// gives it.
	if spec.Env != nil || spec.Program.Env != nil {
		command.Env = append(append([]string(nil), spec.Env...), spec.Program.Env...)
	}
	command.Stdout, command.Stderr = spec.Stdout, spec.Stderr
	// The lock is the starter's descriptor 3. Its path is written in the
	// directory before the fork, so a reader of the directory finds the
	// lock, and sees the starter building, from the moment it is forked,
	// before it has recorded itself (Codex's review of #193 steps 5 and 6,
	// finding 5).
	command.ExtraFiles = []*os.File{spec.Lock}
	if err := atomicfile.Write(filepath.Join(spec.Directory, LockName), []byte(spec.Lock.Name()), 0o644); err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

// LockName is where a build's directory says the path of the launch lock
// its starter holds, written by the launcher before the starter's fork.
const LockName = "launch.lock.path"

// RunIfAsked runs the process as the starter, and exits, where its first
// argument is Command: dockhand's main asks it first, and a test binary
// standing in for dockhand asks it in TestMain (Self).
func RunIfAsked() {
	if len(os.Args) > 1 && os.Args[1] == Command {
		os.Exit(Main(os.Args[2:]))
	}
}

// Self is the running executable as the starter's program: dockhand's own,
// or a test binary's whose TestMain calls RunIfAsked.
func Self() (Program, error) {
	path, err := os.Executable()
	return Program{Path: path}, err
}

// noRecord is the starter's exit where it couldn't record itself, having
// started nothing.
const noRecord = 70

// Unrecorded is why a starter that ended with err started nothing, as it
// said in output, where its standard error went: its own line saying so,
// where err is its exit for that. A provider's error names it, where the
// build's log would otherwise be the only place it's said. Exit 70 with
// none of the starter's words is the build's own, a script that exits 70,
// and no unrecorded start (#243).
func Unrecorded(err error, output []byte) (string, bool) {
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != noRecord {
		return "", false
	}
	for line := range strings.Lines(string(output)) {
		if reason, ok := strings.CutPrefix(strings.TrimSpace(line), "dockhand: "); ok && strings.HasPrefix(reason, "the starter couldn't") {
			return reason, true
		}
	}
	return "", false
}

// Main is the starter's body, run as dockhand Command with its arguments,
// returning its exit status: the child's, or the signal it died of again.
func Main(args []string) int {
	flags := flag.NewFlagSet(Command, flag.ContinueOnError)
	directory := flags.String("record", "", "")
	signalNumber := flags.Int("signal", int(syscall.SIGTERM), "")
	grace := flags.Duration("grace", 30*time.Second, "")
	lockPath := flags.String("lock", "", "")
	if err := flags.Parse(args); err != nil || *directory == "" || flags.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "dockhand: the starter needs -record, the lock, and a command")
		return 2
	}
	// A session of its own, which it leads: its group's number is its PID,
	// on record before anything is started.
	if _, err := syscall.Setsid(); err != nil {
		fmt.Fprintf(os.Stderr, "dockhand: the starter couldn't lead a session: %v\n", err)
		return noRecord
	}
	// The lock, descriptor 3, is the starter's alone: its child isn't
	// handed it on exec.
	syscall.CloseOnExec(3)
	self, err := procid.Self()
	if err == nil {
		var boot string
		if boot, err = procid.BootSession(); err == nil {
			var data []byte
			if data, err = json.Marshal(Record{PID: self.PID, Start: self.Start, Boot: boot, Lock: *lockPath}); err == nil {
				err = atomicfile.Write(filepath.Join(*directory, RecordName), data, 0o644)
			}
		}
	}
	if err != nil {
		// A starter that can't save its record starts nothing.
		fmt.Fprintf(os.Stderr, "dockhand: the starter couldn't record itself, so nothing was started: %v\n", err)
		return noRecord
	}
	stop := syscall.Signal(*signalNumber)
	// It ignores the stop it sends its group, and the one Stop sends it.
	asked := make(chan os.Signal, 1)
	signal.Notify(asked, syscall.SIGTERM, syscall.SIGINT, stop)
	child := exec.Command(flags.Arg(0), flags.Args()[1:]...)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "dockhand: the starter couldn't start the build: %v\n", err)
		return 127
	}
	ended := make(chan error, 1)
	go func() { ended <- child.Wait() }()
	select {
	case err := <-ended:
		return exitOf(err)
	case <-asked:
	}
	// Stopped: the group gets the provider's own stop, which the starter
	// ignores itself, and the grace to end in, the build's own child
	// ending sooner or not: what it started, as a script's job in the
	// background that ignores an interrupt, may still run.
	_ = syscall.Kill(-self.PID, stop)
	select {
	case <-ended:
	case <-time.After(*grace):
	}
	// The last act: SIGKILL to its own group, with no number, which it
	// dies by too, so it can't outlive it and look afterwards (Apple's
	// kill(2)); Stop looks instead.
	_ = syscall.Kill(0, syscall.SIGKILL)
	return 137
}

// exitOf is the starter's own exit for its child's end: its status, or the
// signal it died of raised again, so a launcher reads the same end.
func exitOf(err error) int {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		if err != nil {
			return 1
		}
		return 0
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		signal.Reset(status.Signal())
		_ = syscall.Kill(os.Getpid(), status.Signal())
		// A signal that doesn't end a process ends here as its number.
		return 128 + int(status.Signal())
	}
	return exit.ExitCode()
}

// ReadRecord reads the starter's record in a build's directory: found false
// where none was written, which says no starter ran the build; an error
// where one can't be read, which is never "no record".
func ReadRecord(directory string) (Record, bool, error) {
	data, err := os.ReadFile(filepath.Join(directory, RecordName))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 {
		return Record{}, false, fmt.Errorf("the starter's record %s can't be read: %v", filepath.Join(directory, RecordName), err)
	}
	return record, true, nil
}

// Stop ends a build the starter started, its record in directory, and sees
// it end (the lifecycle design's rules 3 and 5):
//   - no record: no starter ever ran the build; ended once the launch lock
//     is had, so none can still start one;
//   - a record from before the Mac's last restart: ended;
//   - the starter alive, by its PID and its own start time, exactly: it is
//     sent SIGTERM, and stops its group itself;
//   - then the launch lock, within bound: had, the starter and everything
//     handed the lock are gone; not had, something still runs: not known;
//   - then the starter's group, asked with a signal of zero: "no such
//     process" is gone, and ended; "not permitted" is members that can't be
//     signalled, and members that answer can't be told from a later group
//     that took the number: not known, and nothing is signalled.
func Stop(ctx context.Context, directory string, lock buildenv.LaunchLock, bound time.Duration) error {
	record, found, err := ReadRecord(directory)
	if err != nil {
		return err
	}
	if found {
		if boot, err := procid.BootSession(); err == nil && record.Boot != "" && boot != record.Boot {
			return nil
		}
		if alive, err := (procid.System{}).Alive(procid.Process{PID: record.PID, Start: record.Start}); err == nil && alive {
			if err := syscall.Kill(record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("its starter, PID %d, couldn't be asked to stop: %w", record.PID, err)
			}
		}
	}
	release, err := lock.Hold(ctx, bound)
	if err != nil {
		if !found {
			return fmt.Errorf("no starter recorded itself, and %w", err)
		}
		return fmt.Errorf("its starter, PID %d, was asked to stop, and %w", record.PID, err)
	}
	defer release()
	// A starter forked with the lock may have recorded itself, started its
	// build and ended while the stop waited: its record is read again, and
	// its group asked, never taken as gone unseen (Codex's review of #193
	// steps 5 and 6, finding 1).
	if !found {
		if record, found, err = ReadRecord(directory); err != nil {
			return err
		}
		if !found {
			// None recorded itself, and holding the lock, none can.
			return nil
		}
		if boot, err := procid.BootSession(); err == nil && record.Boot != "" && boot != record.Boot {
			return nil
		}
	}
	// The starter's own end is reaped by its parent a moment after it dies,
	// and a group whose last member is unreaped still answers: it's asked
	// again for a moment before what it answers is taken.
	err = syscall.Kill(-record.PID, 0)
	for deadline := time.Now().Add(groupWait); err == nil || errors.Is(err, syscall.EPERM); {
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		err = syscall.Kill(-record.PID, 0)
	}
	switch {
	case errors.Is(err, syscall.ESRCH):
		return nil
	case errors.Is(err, syscall.EPERM):
		return fmt.Errorf("process group %d has members dockhand may not signal, so it isn't known to be gone", record.PID)
	case err == nil:
		return fmt.Errorf("process group %d still has processes after its starter ended; dockhand doesn't signal them, since they can't be told from another group that took the number", record.PID)
	default:
		return fmt.Errorf("process group %d couldn't be asked whether it's gone: %w", record.PID, err)
	}
}

// groupWait is how long Stop asks again of a group that still answers, for
// its starter's parent to reap it.
const groupWait = 2 * time.Second

// Building reports whether a build's directory is still being built in: the
// launch lock its launcher named there before the starter's fork is held,
// from that moment, before the starter has recorded itself. False where no
// starter was started there.
func Building(ctx context.Context, directory string) bool {
	path, err := os.ReadFile(filepath.Join(directory, LockName))
	if err != nil || len(path) == 0 {
		return false
	}
	file, err := filelock.TryExisting(ctx, string(path), filelock.Exclusive)
	if errors.Is(err, filelock.ErrBusy) {
		return true
	}
	if err == nil {
		_ = filelock.Release(file)
	}
	return false
}
