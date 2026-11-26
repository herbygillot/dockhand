package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/buildenv/starter"
)

// standInSudo is the test binary's first argument where it stands in for
// a sudo that keeps a typed password for the terminal's session, as this
// Mac's does ("Type of authentication timestamp record: tty"): -v keeps it
// for the caller's session; -n runs the command only from that session,
// or where the sudoers line asks no password; -k leaves a kept password
// unused for that one command (#332).
const standInSudo = "__sudo"

func init() {
	if len(os.Args) > 1 && os.Args[1] == standInSudo {
		os.Exit(sudoStandIn(os.Args[2:]))
	}
}

// sudoStandIn is the stand-in sudo's body. DOCKHAND_TEST_SUDO_KEPT is the
// file its kept password's session is written to, DOCKHAND_TEST_SUDO_RAN
// where each command it ran is said, and DOCKHAND_TEST_SUDO_RULE=1 a
// sudoers line that asks no password.
func sudoStandIn(args []string) int {
	kept, ran := os.Getenv("DOCKHAND_TEST_SUDO_KEPT"), os.Getenv("DOCKHAND_TEST_SUDO_RAN")
	session, _ := unix.Getsid(0)
	if len(args) > 0 && args[0] == "-v" {
		if err := os.WriteFile(kept, []byte(strconv.Itoa(session)), 0o644); err != nil {
			return 1
		}
		return 0
	}
	ignoreKept := false
	for len(args) > 0 && (args[0] == "-n" || args[0] == "-k") {
		ignoreKept = ignoreKept || args[0] == "-k"
		args = args[1:]
	}
	data, _ := os.ReadFile(kept)
	allowed := os.Getenv("DOCKHAND_TEST_SUDO_RULE") == "1" || !ignoreKept && string(data) == strconv.Itoa(session)
	if !allowed {
		fmt.Fprintln(os.Stderr, "sudo: a password is required")
		return 1
	}
	file, err := os.OpenFile(ran, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 1
	}
	defer file.Close()
	fmt.Fprintln(file, strings.Join(args, " "))
	return 0
}

// sessionSudo is a Local whose sudo is the stand-in, with a terminal to
// ask on, and a prefix whose registry is root's; ran is where the stand-in
// says each command it ran.
func sessionSudo(t *testing.T, rule bool) (l *Local, prefix, kept, ran string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes a registry whatever its mode, so no step asks sudo")
	}
	prefix = t.TempDir()
	registry := filepath.Join(prefix, "var", "macports", "registry")
	require.NoError(t, os.MkdirAll(registry, 0o755))
	require.NoError(t, os.Chmod(registry, 0o555))
	t.Cleanup(func() { os.Chmod(registry, 0o755) })
	kept, ran = filepath.Join(t.TempDir(), "kept"), filepath.Join(t.TempDir(), "ran")
	terminal := filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(terminal, nil, 0o644))
	self, err := starter.Self()
	require.NoError(t, err)
	env := []string{"USER=dhtest", "PATH=/usr/bin:/bin", "DOCKHAND_TEST_SUDO_KEPT=" + kept, "DOCKHAND_TEST_SUDO_RAN=" + ran}
	if rule {
		env = append(env, "DOCKHAND_TEST_SUDO_RULE=1")
	}
	l = &Local{sudo: []string{self.Path, standInSudo}, terminal: terminal, Env: env, Starter: self}
	return l, prefix, kept, ran
}

// launch starts the guest program as a root build is, with the sudo
// asRoot answered, and says whether the stand-in ran it.
func launch(t *testing.T, l *Local, prefix, ran string, sudo []string, inSession bool) bool {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), nil, 0o644))
	lock, err := os.Create(filepath.Join(t.TempDir(), "attempt.lock"))
	require.NoError(t, err)
	defer lock.Close()
	run, err := l.start(dir, prefix, "check-1", sudo, inSession, lock)
	require.NoError(t, err)
	<-run.Done()
	said, _ := os.ReadFile(ran)
	return strings.Contains(string(said), filepath.Join(dir, "guest.tcl"))
}

// A root build whose sudo takes a password typed on this terminal is
// started in this terminal's session, where that sudo keeps the password:
// started in a session of its own, its sudo -n was refused, though the
// password had just been asked (#332). The stand-in refuses -n from any
// session but the one that typed it, as this Mac's sudo does.
func TestARootBuildAfterATypedPasswordRunsInTheTerminalsSession(t *testing.T) {
	t.Parallel()
	l, prefix, _, ran := sessionSudo(t, false)
	sudo, inSession, err := l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.NoError(t, err)
	require.True(t, inSession, "a typed password: the terminal's session")
	require.True(t, launch(t, l, prefix, ran, sudo, inSession), "the build's sudo -n runs, in the session the password was typed in")
	require.False(t, launch(t, l, prefix, ran, sudo, false), "in a session of its own, the same sudo -n is refused")
}

// A password remembered from earlier in this terminal is the typed case
// too: the first probe asks with -k, so only a sudoers line that asks no
// password passes it, and the build goes in this session (#332).
func TestARememberedPasswordIsTheTypedCase(t *testing.T) {
	t.Parallel()
	l, prefix, kept, ran := sessionSudo(t, false)
	session, err := unix.Getsid(0)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(kept, []byte(strconv.Itoa(session)), 0o644))
	sudo, inSession, err := l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.NoError(t, err)
	require.True(t, inSession, "remembered, it is the terminal's session's password still")
	require.True(t, launch(t, l, prefix, ran, sudo, inSession))
}

// A sudoers line that asks no password keeps a session of its own, as
// every launch but the typed one does.
func TestASudoThatAsksNoPasswordKeepsASessionOfItsOwn(t *testing.T) {
	t.Parallel()
	l, prefix, _, ran := sessionSudo(t, true)
	sudo, inSession, err := l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.NoError(t, err)
	require.False(t, inSession)
	require.True(t, launch(t, l, prefix, ran, sudo, inSession))
}
