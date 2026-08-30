// Package testenv gates tests on external tools, after the pattern of the
// Go toolchain's internal/testenv.
//
// The project's testing policy: `go test ./...` must pass on a machine with
// nothing installed but Go. Tests that need an external utility (tclsh,
// port-tclsh, git) ask this package for it and are skipped when it is
// absent. Skipping is loud in verbose mode but silent in CI summaries, so
// environments that are SUPPOSED to have the tools — a CI job that installs
// MacPorts, a maintainer's workstation — set DOCKHAND_TEST_REQUIRE to turn
// would-be skips into failures, catching a broken environment instead of
// quietly testing nothing. "1" or "all" requires every tool; a
// comma-separated list (e.g. "tclsh,port-tclsh") requires exactly those, so
// a CI job demands what it installed and no more.
//
// The same switch names one thing that is not a tool: "network", for the
// tests that check dockhand's assumptions against upstream reality. Those
// are opt-in rather than skip-on-failure, because a network test that
// merely tries will run wherever the network happens to work — which is
// everywhere the hermetic job runs.
package testenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// required reports whether DOCKHAND_TEST_REQUIRE demands the named tool.
func required(name string) bool {
	v := os.Getenv("DOCKHAND_TEST_REQUIRE")
	switch v {
	case "":
		return false
	case "1", "all":
		return true
	}
	for _, want := range strings.Split(v, ",") {
		if strings.TrimSpace(want) == name {
			return true
		}
	}
	return false
}

// Tool returns the path to the named executable, skipping the calling test
// if it is not on PATH — or failing it when DOCKHAND_TEST_REQUIRE demands
// the tool.
func Tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if required(name) {
			t.Fatalf("%s required by DOCKHAND_TEST_REQUIRE but not found on PATH", name)
		}
		t.Skipf("%s not found on PATH; skipping (set DOCKHAND_TEST_REQUIRE=1 to make this a failure)", name)
	}
	return path
}

// Tclsh returns a plain Tcl shell for differential testing against
// Tcl-the-language, with no MacPorts machinery loaded.
func Tclsh(t *testing.T) string {
	t.Helper()
	return Tool(t, "tclsh")
}

// PortTclsh returns MacPorts' Tcl shell — the authoritative evaluator.
// Needs a MacPorts installation, so it is expected to skip in most CI.
func PortTclsh(t *testing.T) string {
	t.Helper()
	return Tool(t, "port-tclsh")
}

// Network gates a test that reaches outside this machine, and is
// deliberately opt-in rather than skip-on-failure.
//
// A test that tries the network and skips when it fails would run
// wherever the network happens to work, which on a hosted runner is
// everywhere — including the job whose whole promise is that it needs
// nothing but Go. Worse, such a test goes red when the world changes
// rather than when the code does, landing on whoever pushes next
// instead of whoever should act.
//
// So a run says whether it wants the outside world, exactly as it says
// which tools it has, and DOCKHAND_TEST_REQUIRE=network is how a job
// that means to check against upstream asks for it.
func Network(t *testing.T) {
	t.Helper()
	if !required("network") {
		t.Skip("network tests are opt-in; set DOCKHAND_TEST_REQUIRE=network to run them")
	}
}
