package tart

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alive is whether a pid still names a process. Signal 0 delivers
// nothing and reports whether it could have.
func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// kids is a pid's immediate children, by parentage and never by pattern
// — the same discipline stopScript itself is written to.
func kids(t *testing.T, pid int) []int {
	t.Helper()
	out, err := exec.Command("/usr/bin/pgrep", "-P", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil // pgrep exits 1 for no matches
	}
	var out2 []int
	for _, f := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(f); err == nil {
			out2 = append(out2, n)
		}
	}
	return out2
}

// THE REAP ACTUALLY KILLS THE BUILD, which is the whole claim --timeout
// makes and the one a person cannot check for themselves without losing
// an hour to find out. It is run against the REAL runner script and a
// port(1) that never returns, because a stop that works on a stub whose
// build has already exited proves nothing.
//
// What it pins is the property that made stopScript worth writing over
// a `pkill -f`: the runner records its own pid, and everything the build
// spawned is a DESCENDANT of it. A pattern would have had to guess, and
// the last time something in this tree matched processes by pattern it
// matched the waiter that was looking for them.
func TestTheStopScriptEndsTheBuildAndItsChildren(t *testing.T) {
	g := newStubGuest(t, "")

	// A port(1) that does not finish: the build this reap exists for.
	require.NoError(t, os.WriteFile(g.portCmd, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755))
	g.write([]argvFile{{Name: "argv", Body: "-v\ninstall\njq\n"}})

	cmd := exec.Command("/bin/sh", "-c", runnerAt(g.dir, g.portCmd))
	cmd.Env = g.env
	launch, err := cmd.CombinedOutput()
	require.NoError(t, err, "the launch itself failed: %s", launch)

	var runner int
	var building []int
	require.Eventually(t, func() bool {
		s := strings.TrimSpace(g.read("pid"))
		if s == "" {
			return false
		}
		if runner, err = strconv.Atoi(s); err != nil || runner <= 0 {
			return false
		}
		building = kids(t, runner)
		return len(building) > 0
	}, 10*time.Second, 20*time.Millisecond,
		"the runner never recorded a pid with a build under it")

	// Nothing is left behind if an assertion below fails.
	t.Cleanup(func() {
		for _, p := range append(building, runner) {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	})

	stop := exec.Command("/bin/sh", "-c", stopScript(g.dir))
	stop.Env = g.env
	said, err := stop.CombinedOutput()
	require.NoError(t, err,
		"a non-zero stop is a survivor, which reaches the record as \"may still be running\": %s", said)

	assert.False(t, alive(runner), "the runner shell outlived its own stop")
	for _, p := range building {
		assert.False(t, alive(p), "a build process outlived the stop that reported success")
	}
}

// STOPPING NOTHING IS SUCCESS. A guest with no runner pid recorded has
// no build to end, and verify.Stopper's contract is about the state
// afterwards — the work is stopped — and not about whether a signal was
// sent. A reap that complained here would put a failure in front of a
// person for a build that was already over.
func TestTheStopScriptIsQuietWhenThereIsNothingToStop(t *testing.T) {
	g := newStubGuest(t, "")
	stop := exec.Command("/bin/sh", "-c", stopScript(g.dir))
	stop.Env = g.env
	said, err := stop.CombinedOutput()
	assert.NoError(t, err, "%s", said)
}
