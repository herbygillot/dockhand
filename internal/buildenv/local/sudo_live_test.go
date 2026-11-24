//go:build darwin

package local

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This Mac's sudo, with real processes, as the lifecycle design asks
// before Local's Stop of a root build is written (docs/designs/
// check-lifecycle.md at bbb4c0b7, "Local and Script", rule 4, and "What remains
// unproved"; #193, step 5e). DOCKHAND_TEST_SUDO_PREFIX names a prefix
// whose port-tclsh this user may run as root without a password:
//
//	<you> ALL=(root) NOPASSWD: <prefix>/bin/port-tclsh
//
// For each of a sudo without a terminal and one with a terminal, which a
// sudo set to use_pty runs its command under a pseudo-terminal of its own
// for, it runs ProbeRootStop, the probe a Mac's first root build runs, one
// implementation: the prefix's port-tclsh under sudo -n with the probe's
// locked file as its standard input, as the starter hands it, and holds
// that:
//
//   - the command's standard input is the lock's file, not a pipe or a
//     terminal sudo put in its place;
//   - the lock stays held while the command lives, after the launcher's
//     own copy is closed, and is free once it has ended;
//   - after the sudo and the command have ended, what the command handed
//     its input on to still holds the lock: the very opening that was
//     locked, not the file opened again;
//   - a SIGTERM reaches the command: sent to the sudo by this process
//     where that's permitted, or else by the root stop, port-tclsh run
//     under sudo -n to send it, as the rule names;
//   - and sudo -n refuses a command the rule doesn't name at once, with no
//     prompt, as the root stop's refused case is answered.
//
// What it finds of the rest it says: whether this process may signal a
// root sudo, and whether the command keeps the group and session it was
// started in.
func TestLiveSudoHandsTheLockToItsCommand(t *testing.T) {
	prefix := os.Getenv("DOCKHAND_TEST_SUDO_PREFIX")
	if prefix == "" {
		t.Skip("DOCKHAND_TEST_SUDO_PREFIX names a prefix whose port-tclsh sudo runs without a password")
	}
	tclsh := filepath.Join(prefix, "bin", "port-tclsh")
	sudo := []string{"/usr/bin/sudo"}
	ready := exec.Command(sudo[0], "-n", tclsh, os.DevNull)
	if out, err := ready.CombinedOutput(); err != nil {
		t.Fatalf("sudo -n %s: %v %s; the test needs the sudoers line: %s ALL=(root) NOPASSWD: %s", tclsh, err, out, os.Getenv("USER"), tclsh)
	}
	dir := t.TempDir()
	for _, terminal := range []bool{false, true} {
		name := "without a terminal"
		if terminal {
			name = "with a terminal"
		}
		t.Run(name, func(t *testing.T) {
			found, err := ProbeRootStop(t.Context(), sudo, os.Environ(), tclsh, filepath.Join(dir, strings.ReplaceAll(name, " ", "-")), terminal)
			require.NoError(t, err, "the probe")
			t.Log(found.Words())
			require.True(t, found.StdinIsLock, "the command's standard input is the lock's file, not a %s sudo put there", found.Input)
			require.True(t, found.LockHeld, "the lock is held while the command lives")
			require.True(t, found.SignalReached, "the SIGTERM reached the command")
			require.True(t, found.SameOpening, "what the command handed its input on to holds the lock: the opening that was locked, not the file opened again")
			require.True(t, found.Held(), "every part held")
			require.True(t, found.LockFreed, "the lock is free once the command has ended")
			t.Logf("this process signals the root sudo: %v; the command keeps the sudo's group: %v, its session: %v", found.SelfSignals, found.KeepsGroup, found.KeepsSession)
		})
	}

	// A command the rule doesn't name, or a user it doesn't, is refused at
	// once: sudo -n asks no one.
	refused := exec.Command(sudo[0], "-n", "-u", "nobody", tclsh, os.DevNull)
	start := time.Now()
	out, err := refused.CombinedOutput()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit), "sudo -n -u nobody is refused: %v %s", err, out)
	require.Less(t, time.Since(start), 10*time.Second, "without waiting for a password")
	t.Logf("refused: %s", strings.TrimSpace(string(out)))
}
