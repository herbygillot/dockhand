//go:build darwin

package guest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/procid"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The root stop signals a target only where its PID started in the very
// second it was given, procid's start to the second, and nothing else: a target whose
// start differs is named and left alone (the lifecycle design's rule 4;
// architecture's ruling of 2026-11-23).
func TestTheRootStopSignalsOnlyTheProcessRecorded(t *testing.T) {
	t.Parallel()
	tclsh := testsupport.MacPortsTclsh(t)
	script := filepath.Join(t.TempDir(), "guest.tcl")
	require.NoError(t, os.WriteFile(script, Program, 0o644))
	sleeper := exec.Command("/bin/sleep", "60")
	require.NoError(t, sleeper.Start())
	ended := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(ended) }()
	t.Cleanup(func() { _ = sleeper.Process.Kill() })
	exact, err := procid.Start(sleeper.Process.Pid)
	require.NoError(t, err)
	start, ok := procid.Seconds(exact)
	require.True(t, ok, exact)
	pid := strconv.Itoa(sleeper.Process.Pid)

	out, err := exec.Command(tclsh, script, "--stop", "TERM", "process", pid, start+"1").CombinedOutput()
	require.Error(t, err, "%s", out)
	require.Contains(t, string(out), "not signalled: PID "+pid+" isn't the process recorded")
	require.NoError(t, syscall.Kill(sleeper.Process.Pid, 0), "left alone")

	out, err = exec.Command(tclsh, script, "--stop", "TERM", "process", pid, start).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "signalled: TERM to "+pid)
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the recorded process wasn't stopped")
	}

	out, err = exec.Command(tclsh, script, "--stop", "KILL", "group", "1", "x").CombinedOutput()
	require.Error(t, err, "%s", out)
	require.Contains(t, string(out), "isn't a target this stop takes", "launchd is never a target")
}
