package guest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// stdin_is reads the guest's standard input by fstat: the lock's own file
// passes, another file fails, and the lock's path reopened after the file
// there was replaced, the same name with another file number, fails (the
// lifecycle design's rule 4, as architecture read the /dev/fd/0 finding,
// 2026-11-12; #193).
func TestTheGuestKnowsItsStandardInputByItsFileNumber(t *testing.T) {
	t.Parallel()
	tclsh := testsupport.MacPortsTclsh(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "check.tcl")
	require.NoError(t, os.WriteFile(script, append(append([]byte{}, StdinIs...), "puts [stdin_is [lindex $argv 0]]\n"...), 0o644))
	lock := filepath.Join(dir, "launch.lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o600))
	is := func(stdin *os.File) string {
		cmd := exec.Command(tclsh, script, lock)
		cmd.Stdin = stdin
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	held, err := os.Open(lock)
	require.NoError(t, err)
	defer held.Close()
	require.Equal(t, "1", is(held), "the lock's own file")

	other := filepath.Join(dir, "other")
	require.NoError(t, os.WriteFile(other, nil, 0o600))
	elsewhere, err := os.Open(other)
	require.NoError(t, err)
	defer elsewhere.Close()
	require.Equal(t, "0", is(elsewhere), "another file")

	// The lock's path now names another file: the opening held is of the
	// one that was there.
	replacement := filepath.Join(dir, "replacement")
	require.NoError(t, os.WriteFile(replacement, nil, 0o600))
	require.NoError(t, os.Rename(replacement, lock))
	require.Equal(t, "0", is(held), "the path's file replaced after it was opened")

	reopened, err := os.Open(lock)
	require.NoError(t, err)
	defer reopened.Close()
	require.Equal(t, "1", is(reopened))
	require.Equal(t, "0", is(nil), "no input at all")
}
