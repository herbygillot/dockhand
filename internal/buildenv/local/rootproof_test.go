//go:build darwin

package local

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
		{"hands it on", "#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n", true},
		{"logs its input", "#!/bin/sh\n[ \"$1\" = -n ] && shift\ncat | \"$@\"\n", false},
	} {
		sudo := filepath.Join(t.TempDir(), "sudo")
		testsupport.WriteExecutable(t, sudo, c.script)
		l := &Local{Temp: t.TempDir(), Locks: t.TempDir()}
		proved, why := l.rootStopProved([]string{sudo})
		require.False(t, proved, c.name)
		require.Contains(t, why, "hasn't been probed yet", c.name)

		var said []string
		l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) })
		require.Len(t, said, 1, "%s: the probe is said once", c.name)
		proved, why = l.rootStopProved([]string{sudo})
		require.Equal(t, c.proved, proved, "%s: %s", c.name, why)
		if !c.proved {
			require.Contains(t, why, "standard input the lock: false")
		}

		said = nil
		l.proveRootOnce(t.Context(), []string{sudo}, tclsh, func(line string) { said = append(said, line) })
		require.Empty(t, said, "%s: kept, not probed again", c.name)
		later := time.Now().Add(time.Hour)
		require.NoError(t, os.Chtimes(sudo, later, later))
		proved, why = l.rootStopProved([]string{sudo})
		require.False(t, proved, "%s: another binary is probed again", c.name)
		require.Contains(t, why, "hasn't been probed yet")
	}
}
