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
	"github.com/herbygillot/dockhand/internal/subprocess"
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

// RootProof is what a probe of this Mac's sudo found for this person, the
// lifecycle design's test of a root build's stop (rule 4), run once for
// each sudo binary: whether its command is handed the very opening of the
// launch lock that was locked, as its standard input, and holds it while
// it lives, and whether a SIGTERM reaches it, with and without a terminal.
// Proved only where every part held. It is the person's, kept in their own
// dockhand directory: it proves their sudo rule as much as the Mac's sudo,
// and a proof another account could write would let a stop say a live
// root build ended (architecture's ruling of 2026-11-23).
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
	// StdinIsLock, LockHeld, SignalReached, SameOpening and LockFreed are
	// the parts that must hold. SameOpening is the lock still held, after
	// the sudo and its command have ended, by what the command handed its
	// standard input on to: it holds the lock only where that input is the
	// opening the launcher locked, not the same file opened again, which
	// the command's own check can't tell (Codex's read of 25ee3c59).
	StdinIsLock, LockHeld, SignalReached, SameOpening, LockFreed bool
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
	return c.StdinIsLock && c.LockHeld && c.SignalReached && c.SameOpening && c.LockFreed
}

// Words is the case as a line of the proof.
func (c ProbeCase) Words() string {
	terminal := "without a terminal"
	if c.Terminal {
		terminal = "with a terminal"
	}
	parts := []string{fmt.Sprintf("standard input the lock: %v", c.StdinIsLock), fmt.Sprintf("lock held: %v", c.LockHeld),
		fmt.Sprintf("SIGTERM reached the command: %v", c.SignalReached), fmt.Sprintf("the opening locked: %v", c.SameOpening),
		fmt.Sprintf("lock freed: %v", c.LockFreed),
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
// input, which hands that input on to a holder that ignores the stop,
// reports whether the input is the lock's file, and waits; the lock is
// read as held after the launcher lets go of its own copy; a SIGTERM goes
// to the sudo, from this process where that's permitted, else from the
// root stop, port-tclsh under sudo -n; the command is seen to end, the
// lock still held by the holder alone, and then, the holder let go, freed.
// Everything the probe starts ends by itself once it's let go, on every
// path out, and within a bound where nothing lets it go. The live test and
// the first root launch on a Mac run this one implementation. An error is
// a probe that couldn't be made, not a part that failed.
func ProbeRootStop(ctx context.Context, sudo, env []string, tclsh, dir string, terminal bool) (ProbeCase, error) {
	found := ProbeCase{Terminal: terminal}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return found, err
	}
	write := func(name, script string) (string, error) {
		path := filepath.Join(dir, name)
		return path, os.WriteFile(path, []byte(script), 0o644)
	}
	release := filepath.Join(dir, "release")
	_ = os.Remove(release)
	bound := strconv.Itoa(int(3 * probeBound / time.Second))
	probe, err := write("probe.tcl", string(guest.StdinIs)+`set out [lindex $argv 0]
set lock [lindex $argv 1]
set release [lindex $argv 2]
set bound [lindex $argv 3]
# The holder keeps this command's standard input as it came, ignores the
# stop, and ends once let go, or by the bound.
set holder [exec /bin/sh -c {trap '' TERM HUP INT; n=0; while [ ! -e "$0" ] && [ $n -lt $(($1 * 20)) ]; do sleep 0.05; n=$((n+1)); done} $release $bound >/dev/null 2>/dev/null &]
set f [open $out.tmp w]
puts $f "[pid] $holder [stdin_is $lock] [exec /usr/bin/stat -f %HT]"
close $f
file rename -force $out.tmp $out
set deadline [expr {[clock seconds] + $bound}]
while {![file exists $release] && [clock seconds] < $deadline} { after 50 }
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
	args := append(append([]string(nil), sudo[1:]...), "-n", tclsh, probe, out, lockPath, release, bound)
	// Not subprocess.Run: the probe holds the sudo as a process while it
	// runs, signals it and watches it end, in a session of its own, with
	// a controlling terminal or without, which Run, a call that returns
	// once its program has ended, can't give. Its Env is the one compose
	// gives, handed on explicitly.
	command := exec.CommandContext(ctx, sudo[0], args...)
	command.Stdin, command.Env = lock, env
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
	// ended is closed once the sudo has ended, so every look at it, the
	// last on the way out included, sees it; waited is how.
	ended := make(chan struct{})
	var waited error
	go func() { waited = command.Wait(); close(ended) }()
	// On every path out, everything the probe started is let go, and ends
	// by itself: a root command this process can't signal, and a sudo
	// that wouldn't pass a SIGKILL on, alike.
	defer func() {
		_ = os.WriteFile(release, nil, 0o644)
		select {
		case <-ended:
		case <-time.After(probeBound):
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
		if said = strings.Fields(string(data)); err == nil && len(said) >= 4 {
			break
		}
		if time.Now().After(deadline) {
			return found, fmt.Errorf("the probe's command reported nothing within %s", probeBound)
		}
		select {
		case <-ctx.Done():
			return found, ctx.Err()
		case <-ended:
			return found, fmt.Errorf("sudo -n %s ended before its command reported: %v", tclsh, waited)
		case <-time.After(50 * time.Millisecond):
		}
	}
	pid, err := strconv.Atoi(said[0])
	if err != nil {
		return found, fmt.Errorf("the probe's command reported %q", strings.Join(said, " "))
	}
	found.StdinIsLock, found.Input = said[2] == "1", strings.Join(said[3:], " ")
	found.LockHeld = lockBusy(lockPath)
	group, _ := unix.Getpgid(pid)
	session, _ := unix.Getsid(pid)
	found.KeepsGroup, found.KeepsSession = group == command.Process.Pid, session == command.Process.Pid
	if err := syscall.Kill(command.Process.Pid, syscall.SIGTERM); err == nil {
		found.SelfSignals = true
	} else {
		if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "sudo", Path: sudo[0], Args: append(append([]string(nil), sudo[1:]...), "-n", tclsh, stop, strconv.Itoa(command.Process.Pid)), Env: env}); err != nil {
			return found, fmt.Errorf("the root stop under sudo -n: %w", err)
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
	// The sudo and its command have ended: only the holder can hold the
	// lock now, and only where its input is the opening that was locked.
	found.SameOpening = lockBusy(lockPath)
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		return found, err
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if free, err := filelock.TryAcquire(lockPath); err == nil {
			found.LockFreed = true
			_ = free.Close()
			break
		}
	}
	return found, nil
}

// lockBusy reports whether another opening holds the lock at path; an
// opening the probe gets is let go at once, so the probe never holds it
// itself past the look.
func lockBusy(path string) bool {
	file, err := filelock.TryAcquire(path)
	if err == nil {
		_ = file.Close()
	}
	return errors.Is(err, filelock.ErrBusy)
}

// proveRoot runs the probe in both cases and makes the proof of it.
func proveRoot(ctx context.Context, sudo, env []string, tclsh, dir string, identity SudoIdentity, now time.Time) (proof RootProof, made bool) {
	proof, made = RootProof{Sudo: identity, Proved: true, At: now}, true
	for _, terminal := range []bool{false, true} {
		name := "plain"
		if terminal {
			name = "terminal"
		}
		found, err := ProbeRootStop(ctx, sudo, env, tclsh, filepath.Join(dir, name), terminal)
		if err != nil {
			proof.Proved, made = false, false
			proof.Found = append(proof.Found, fmt.Sprintf("%s: the probe couldn't be made: %v", found.Words(), err))
			continue
		}
		proof.Proved = proof.Proved && found.Held()
		proof.Found = append(proof.Found, found.Words())
	}
	return proof, made && ctx.Err() == nil
}

// rootProofName is the person's proof of this Mac's sudo, in Proofs.
const rootProofName = "root-stop-proof.json"

// readRootProof reads the proof kept for this Mac's sudo; found false
// where none is kept, where Proofs is unset, where the file isn't this
// account's alone (its owner, and writable by no one else), where it can't
// be read, or where it is of another sudo binary.
func (l *Local) readRootProof(identity SudoIdentity) (RootProof, bool) {
	if l.Proofs == "" {
		return RootProof{}, false
	}
	path := filepath.Join(l.Proofs, rootProofName)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return RootProof{}, false
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		return RootProof{}, false
	}
	data, err := os.ReadFile(path)
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
	switch {
	case !found && l.Proofs == "":
		return false, "dockhand has no directory of yours to keep a probe of this Mac's sudo in"
	case !found:
		return false, "this Mac's sudo hasn't been probed yet; the first root build probes it"
	}
	if !proof.Proved {
		return false, "this Mac's sudo failed the probe of a root build's stop: " + strings.Join(proof.Found, "; ")
	}
	return true, ""
}

// proveRootOnce probes this Mac's sudo where no proof that passed is kept
// for the sudo binary, at a root build's launch, so a stop is never
// delayed by it and never the first to learn it fails. Only a probe that
// was made is kept: one canceled, or refused by sudo -n before the
// sudoers line was there, was kept as failed for good, and a failed one
// is made again at the next root build (#341). The build starts all the
// same.
func (l *Local) proveRootOnce(ctx context.Context, sudo []string, tclsh string, say func(string)) {
	identity, err := sudoIdentity(sudo[0])
	if err != nil || l.Proofs == "" {
		return
	}
	if kept, found := l.readRootProof(identity); found && kept.Proved {
		return
	}
	dir, err := os.MkdirTemp(l.temp(), "dockhand-root-probe-")
	if err != nil {
		return
	}
	say("probing this Mac's sudo once, for how a root build is stopped")
	defer os.RemoveAll(dir)
	proof, made := proveRoot(ctx, sudo, l.Env, tclsh, dir, identity, time.Now().UTC())
	if !made {
		return
	}
	data, err := json.Marshal(proof)
	if err != nil {
		return
	}
	if err := os.MkdirAll(l.Proofs, 0o700); err != nil {
		return
	}
	_ = atomicfile.Write(filepath.Join(l.Proofs, rootProofName), data, 0o600)
}
