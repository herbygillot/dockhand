//go:build darwin

package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The probe of a root build's stop, with stand-ins for sudo: one that
// hands its command its standard input as it came proves it, with a
// terminal and without; one that puts a pipe there, as a sudo logging a
// command's input does, fails it and says what the input was. The proof
// is kept for the sudo binary, read by rootStopProved, and probed again
// only for another binary (the lifecycle design's rule 4; architecture's
// ruling of 2026-11-23).
func TestARootProbeProvesOnlyASudoThatHandsTheLockOn(t *testing.T) {
	t.Parallel()
	tclsh := testsupport.MacPortsTclsh(t)
	for _, c := range []struct {
		name, script string
		proved       bool
	}{
		{"hands it on", "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\nexec \"$@\"\n", true},
		{"logs its input", "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\ncat | \"$@\"\n", false},
		// The same file opened again: the command's own check passes,
		// and only what it hands its input on to shows it holds no lock.
		{"opens the lock again", "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\nexec \"$@\" < \"$4\"\n", false},
	} {
		sudo := filepath.Join(t.TempDir(), "sudo")
		testsupport.WriteExecutable(t, sudo, c.script)
		l := &Local{Temp: t.TempDir(), Proofs: filepath.Join(t.TempDir(), "local")}
		proved, why := l.rootStopProved([]string{sudo}, false)
		require.False(t, proved, c.name)
		require.Contains(t, why, "hasn't been probed yet", c.name)

		var said []string
		l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) }, false)
		require.Len(t, said, 1, "%s: the probe is said once", c.name)
		proved, why = l.rootStopProved([]string{sudo}, false)
		require.Equal(t, c.proved, proved, "%s: %s", c.name, why)
		switch c.name {
		case "logs its input":
			require.Contains(t, why, "standard input the lock: false")
		case "opens the lock again":
			require.Contains(t, why, "standard input the lock: true")
			require.Contains(t, why, "the opening locked: false")
		}

		// A proof that passed is kept; one that failed is made again at
		// the next root build, as after a sudoers line is added (#341).
		said = nil
		l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) }, false)
		if c.proved {
			require.Empty(t, said, "%s: kept, not probed again", c.name)
		} else {
			require.Len(t, said, 1, "%s: a failed probe is made again", c.name)
		}
		later := time.Now().Add(time.Hour)
		require.NoError(t, os.Chtimes(sudo, later, later))
		proved, why = l.rootStopProved([]string{sudo}, false)
		require.False(t, proved, "%s: another binary is probed again", c.name)
		require.Contains(t, why, "hasn't been probed yet")
	}
}

// A probe that couldn't be made, as one canceled by Ctrl-C or refused by
// sudo -n before the sudoers line is there, keeps nothing: the next root
// build probes again, where it was kept as failed for good (#341).
func TestAProbeThatCouldntBeMadeKeepsNothing(t *testing.T) {
	t.Parallel()
	tclsh := testsupport.MacPortsTclsh(t)
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\necho 'sudo: a password is required' >&2\nexit 1\n")
	l := &Local{Temp: t.TempDir(), Proofs: filepath.Join(t.TempDir(), "local")}
	l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(string) {}, false)
	_, why := l.rootStopProved([]string{sudo}, false)
	require.Contains(t, why, "hasn't been probed yet", "refused: nothing kept")

	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\nexec \"$@\"\n")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	l.proveRootOnce(canceled, []string{sudo}, tclsh, func(string) {}, false)
	_, why = l.rootStopProved([]string{sudo}, false)
	require.Contains(t, why, "hasn't been probed yet", "canceled: nothing kept")

	l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(string) {}, false)
	proved, why := l.rootStopProved([]string{sudo}, false)
	require.True(t, proved, why)
}

// A proof counts only where it is this account's alone and of this sudo
// binary: one unreadable, writable by others, or kept nowhere is not
// proved (architecture's ruling of 2026-11-23).
func TestARootProofIsTrustedOnlyWhereItIsThePersonsOwn(t *testing.T) {
	t.Parallel()
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nexec \"$@\"\n")
	identity, err := sudoIdentity(sudo)
	require.NoError(t, err)
	proved, err := json.Marshal(RootProof{Sudo: identity, Proved: true, At: time.Now()})
	require.NoError(t, err)
	for _, c := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"its own, proved", proved, 0o600},
		{"unreadable", []byte("{not json"), 0o600},
		{"writable by others", proved, 0o666},
		{"group-writable", proved, 0o620},
	} {
		l := &Local{Proofs: t.TempDir()}
		path := filepath.Join(l.Proofs, rootProofName)
		require.NoError(t, os.WriteFile(path, c.data, 0o600))
		require.NoError(t, os.Chmod(path, c.mode))
		ok, why := l.rootStopProved([]string{sudo}, false)
		require.Equal(t, c.name == "its own, proved", ok, "%s: %s", c.name, why)
	}
	ok, why := (&Local{}).rootStopProved([]string{sudo}, false)
	require.False(t, ok)
	require.Contains(t, why, "no directory of yours")
}

// A root build's first launch probes this Mac's sudo, and a probe that
// fails still lets the build start and finish, through Execute: here a
// sudo that opens the lock's file again, which the guest's own check can't
// tell, builds, and the proof kept says what the probe found (the
// lifecycle design's rule 4, as amended 2026-11-23).
func TestAFailedProbeStillStartsTheBuild(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active, "DEPS=libharbor=zlib harbor-cli=libharbor")
	registryDir := filepath.Join(prefix, "var", "macports", "registry")
	require.NoError(t, os.Chmod(registryDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(registryDir, 0o755) })
	sudo := filepath.Join(t.TempDir(), "sudo")
	// It opens the lock's file again only where the fourth word is a file,
	// the build's and the probe's lock path, so the root stop it runs, its
	// fourth word --stop, runs as it is (a gate's run hung where the
	// build's guest outlived Follow and the stop couldn't reach it).
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\nif [ -f \"$4\" ]; then exec \"$@\" < \"$4\"; fi\nexec \"$@\"\n")
	l := localProvider(t, env)
	l.sudo, l.Proofs = []string{sudo}, filepath.Join(t.TempDir(), "local")
	build := &fakeBuild{}
	require.NoError(t, l.Execute(t.Context(), localJob(t, prefix), build))
	require.Len(t, build.results, 2, "the build ran")
	require.Equal(t, model.OutcomePassed, build.results[0].Outcome)
	require.Contains(t, strings.Join(build.progress, "\n"), "probing this Mac's sudo once")
	proved, why := l.rootStopProved([]string{sudo}, false)
	require.False(t, proved)
	require.Contains(t, why, "the opening locked: false")
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path), "and the prefix put back")
}

// A root build started in this terminal's session, after a typed password,
// is probed as it is started, a group of its own in this session, and the
// proof stands for that launch alone: the other launch, in a session of
// its own, is probed again before its stop counts (#332).
func TestAProbeStandsForTheLaunchItProbed(t *testing.T) {
	t.Parallel()
	tclsh := testsupport.MacPortsTclsh(t)
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\nwhile [ \"$1\" = -n ] || [ \"$1\" = -k ]; do shift; done\nexec \"$@\"\n")
	l := &Local{Temp: t.TempDir(), Proofs: filepath.Join(t.TempDir(), "local")}
	var said []string
	l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) }, true)
	require.Len(t, said, 1)
	proved, why := l.rootStopProved([]string{sudo}, true)
	require.True(t, proved, why)
	data, err := os.ReadFile(filepath.Join(l.Proofs, rootProofName))
	require.NoError(t, err)
	var proof RootProof
	require.NoError(t, json.Unmarshal(data, &proof))
	require.True(t, proof.InSession)
	require.Len(t, proof.Found, 1)
	require.True(t, strings.HasPrefix(proof.Found[0], "in this terminal's session: "), proof.Found[0])

	proved, why = l.rootStopProved([]string{sudo}, false)
	require.False(t, proved)
	require.Contains(t, why, "hasn't been probed yet for a root build that asks no password")
	said = nil
	l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) }, false)
	require.Len(t, said, 1, "the other launch is probed")
	proved, why = l.rootStopProved([]string{sudo}, false)
	require.True(t, proved, why)
}
