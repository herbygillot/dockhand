package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/filelock"
)

// SudoIdentity is the sudo binary a proof is of: its path, size and
// modification time, so a sudo replaced by an update is proved again.
type SudoIdentity struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// sudoIdentity reads the identity of the sudo at path.
func sudoIdentity(path string) (SudoIdentity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return SudoIdentity{}, err
	}
	return SudoIdentity{Path: path, Size: info.Size(), ModTime: info.ModTime().UTC()}, nil
}

// RootProof is what a probe of this Mac's sudo found, the lifecycle
// design's test of a root build's stop (rule 4), run once for each sudo
// binary: whether its command is handed the launch lock as its standard
// input and holds it while it lives, and whether a SIGTERM reaches it,
// with and without a terminal. Proved only where every part held.
type RootProof struct {
	Sudo   SudoIdentity `json:"sudo"`
	Proved bool         `json:"proved"`
	// Found is what the probe found, in words, each case's line.
	Found []string  `json:"found"`
	At    time.Time `json:"at"`
}

// ProbeCase is one case of the probe, with a terminal or without, and what
// it saw.
type ProbeCase struct {
	Terminal bool
	// StdinIsLock, LockHeld, SignalReached and LockFreed are the four
	// parts that must hold.
	StdinIsLock, LockHeld, SignalReached, LockFreed bool
	// SelfSignals is whether this process may signal the root sudo, else
	// the root stop under sudo -n sent it; KeepsGroup and KeepsSession
	// whether the command stayed in the sudo's group and session.
	SelfSignals, KeepsGroup, KeepsSession bool
	// Input is what the command's standard input was, as stat(1) said,
	// where it wasn't the lock.
	Input string
}

// Held reports whether every part of the case held.
func (c ProbeCase) Held() bool {
	return c.StdinIsLock && c.LockHeld && c.SignalReached && c.LockFreed
}

// Words is the case as a line of the proof.
func (c ProbeCase) Words() string {
	terminal := "without a terminal"
	if c.Terminal {
		terminal = "with a terminal"
	}
	parts := []string{fmt.Sprintf("standard input the lock: %v", c.StdinIsLock), fmt.Sprintf("lock held: %v", c.LockHeld),
		fmt.Sprintf("SIGTERM reached the command: %v", c.SignalReached), fmt.Sprintf("lock freed: %v", c.LockFreed),
		fmt.Sprintf("this process signals the sudo: %v", c.SelfSignals), fmt.Sprintf("keeps the sudo's group: %v", c.KeepsGroup)}
	if !c.StdinIsLock && c.Input != "" {
		parts = append(parts, "its standard input was a "+c.Input)
	}
	return terminal + ": " + strings.Join(parts, "; ")
}

// probeBound is how long each step of the probe waits.
const probeBound = 30 * time.Second

// ProbeRootStop runs the probe once, in dir, with a terminal or without:
// the prefix's port-tclsh under sudo -n, the probe's lock as its standard
// input, which reports whether that input is the lock's file and then
// waits; the lock is read as held after the launcher lets go of its own
// copy; a SIGTERM goes to the sudo, from this process where that's
// permitted, else from the root stop, port-tclsh under sudo -n; and the
// command and the lock are seen to end and be freed. The live test and
// the first root launch on a Mac run this one implementation. An error is
// a probe that couldn't be made, not a part that failed.
func ProbeRootStop(ctx context.Context, sudo []string, tclsh, dir string, terminal bool) (ProbeCase, error) {
	found := ProbeCase{Terminal: terminal}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return found, err
	}
	write := func(name, script string) (string, error) {
		path := filepath.Join(dir, name)
		return path, os.WriteFile(path, []byte(script), 0o644)
	}
	probe, err := write("probe.tcl", string(guest.StdinIs)+`set out [lindex $argv 0]
set lock [lindex $argv 1]
set f [open $out.tmp w]
puts $f "[pid] [stdin_is $lock] [exec /usr/bin/stat -f %HT]"
close $f
file rename -force $out.tmp $out
after 600000
`)
	if err != nil {
		return found, err
	}
	stop, err := write("stop.tcl", "exec /bin/kill -TERM [lindex $argv 0]\n")
	if err != nil {
		return found, err
	}
	lockPath := filepath.Join(dir, "launch.lock")
	lock, err := filelock.TryAcquire(lockPath)
	if err != nil {
		return found, err
	}
	out := filepath.Join(dir, "probe")
	_ = os.Remove(out)
	args := append(append([]string(nil), sudo[1:]...), "-n", tclsh, probe, out, lockPath)
	command := exec.CommandContext(ctx, sudo[0], args...)
	command.Stdin = lock
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if terminal {
		ptm, tty, err := openTerminal()
		if err != nil {
			_ = lock.Close()
			return found, err
		}
		defer ptm.Close()
		defer tty.Close()
		command.Stdout, command.Stderr = tty, tty
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1}
	}
	if err := command.Start(); err != nil {
		_ = lock.Close()
		return found, err
	}
	ended := make(chan error, 1)
	go func() { ended <- command.Wait() }()
	defer func() {
		select {
		case <-ended:
		default:
			_ = command.Process.Kill()
		}
	}()
	// The launcher's own copy goes: from here only the sudo and its command
	// hold the opening.
	if err := lock.Close(); err != nil {
		return found, err
	}
	var said []string
	for deadline := time.Now().Add(probeBound); ; {
		data, err := os.ReadFile(out)
		if said = strings.Fields(string(data)); err == nil && len(said) >= 3 {
			break
		}
		if time.Now().After(deadline) {
			return found, fmt.Errorf("the probe's command reported nothing within %s", probeBound)
		}
		select {
		case <-ctx.Done():
			return found, ctx.Err()
		case err := <-ended:
			return found, fmt.Errorf("sudo -n %s ended before its command reported: %v", tclsh, err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	pid, err := strconv.Atoi(said[0])
	if err != nil {
		return found, fmt.Errorf("the probe's command reported %q", strings.Join(said, " "))
	}
	found.StdinIsLock, found.Input = said[1] == "1", strings.Join(said[2:], " ")
	if _, err := filelock.TryAcquire(lockPath); errors.Is(err, filelock.ErrBusy) {
		found.LockHeld = true
	}
	group, _ := unix.Getpgid(pid)
	session, _ := unix.Getsid(pid)
	found.KeepsGroup, found.KeepsSession = group == command.Process.Pid, session == command.Process.Pid
	if err := syscall.Kill(command.Process.Pid, syscall.SIGTERM); err == nil {
		found.SelfSignals = true
	} else {
		root := exec.CommandContext(ctx, sudo[0], append(append([]string(nil), sudo[1:]...), "-n", tclsh, stop, strconv.Itoa(command.Process.Pid))...)
		if out, err := root.CombinedOutput(); err != nil {
			return found, fmt.Errorf("the root stop under sudo -n: %v %s", err, strings.TrimSpace(string(out)))
		}
	}
	select {
	case <-ended:
	case <-time.After(probeBound):
		return found, nil
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			found.SignalReached = true
			break
		}
	}
	if free, err := filelock.TryAcquire(lockPath); err == nil {
		found.LockFreed = true
		_ = free.Close()
	}
	return found, nil
}

// proveRoot runs the probe in both cases and makes the proof of it.
func proveRoot(ctx context.Context, sudo []string, tclsh, dir string, identity SudoIdentity, now time.Time) RootProof {
	proof := RootProof{Sudo: identity, Proved: true, At: now}
	for _, terminal := range []bool{false, true} {
		name := "plain"
		if terminal {
			name = "terminal"
		}
		found, err := ProbeRootStop(ctx, sudo, tclsh, filepath.Join(dir, name), terminal)
		if err != nil {
			proof.Proved = false
			proof.Found = append(proof.Found, fmt.Sprintf("%s: the probe couldn't be made: %v", found.Words(), err))
			continue
		}
		proof.Proved = proof.Proved && found.Held()
		proof.Found = append(proof.Found, found.Words())
	}
	return proof
}

// rootProofName is this Mac's proof of its sudo, beside the prefixes'
// locks, which every dockhand on the Mac shares whatever its user or
// database: the proof is of the Mac's sudo, not of a store.
const rootProofName = "root-stop-proof.json"

// readRootProof reads the proof kept for this Mac's sudo; found false
// where none is kept, or one of another sudo binary.
func (l *Local) readRootProof(identity SudoIdentity) (RootProof, bool) {
	data, err := os.ReadFile(filepath.Join(l.locks(), rootProofName))
	if err != nil {
		return RootProof{}, false
	}
	var proof RootProof
	if json.Unmarshal(data, &proof) != nil || proof.Sudo.Path != identity.Path || proof.Sudo.Size != identity.Size || !proof.Sudo.ModTime.Equal(identity.ModTime) {
		return RootProof{}, false
	}
	return proof, true
}

// rootStopProved reports whether this Mac's sudo has passed the probe of a
// root build's stop, and, where it hasn't, what the probe found, or that
// none was made: the one place the answer is decided, so another way of
// knowing (a setting, or taking every Mac's sudo for the acceptance Mac's)
// would change it alone.
func (l *Local) rootStopProved(sudo []string) (bool, string) {
	identity, err := sudoIdentity(sudo[0])
	if err != nil {
		return false, fmt.Sprintf("%s couldn't be read: %v", sudo[0], err)
	}
	proof, found := l.readRootProof(identity)
	if !found {
		return false, "this Mac's sudo hasn't been probed yet; the first root build probes it"
	}
	if !proof.Proved {
		return false, "this Mac's sudo failed the probe of a root build's stop: " + strings.Join(proof.Found, "; ")
	}
	return true, ""
}

// proveRootOnce probes this Mac's sudo where no proof is kept for the sudo
// binary, at a root build's launch, so a stop is never delayed by it and
// never the first to learn it fails. A probe that fails or can't be made
// is kept as that; the build starts all the same.
func (l *Local) proveRootOnce(ctx context.Context, sudo []string, tclsh string, say func(string)) {
	identity, err := sudoIdentity(sudo[0])
	if err != nil {
		return
	}
	if _, found := l.readRootProof(identity); found {
		return
	}
	dir, err := os.MkdirTemp(l.temp(), "dockhand-root-probe-")
	if err != nil {
		return
	}
	say("probing this Mac's sudo once, for how a root build is stopped")
	defer os.RemoveAll(dir)
	proof := proveRoot(ctx, sudo, tclsh, dir, identity, time.Now().UTC())
	data, err := json.Marshal(proof)
	if err != nil {
		return
	}
	if err := os.MkdirAll(l.locks(), 0o777); err != nil {
		return
	}
	_ = atomicfile.Write(filepath.Join(l.locks(), rootProofName), data, 0o644)
}
