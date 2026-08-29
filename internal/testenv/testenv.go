// Package testenv gates tests on external tools, after the pattern of the
// Go toolchain's internal/testenv.
//
// The project's testing policy: `go test ./...` must pass on a machine with
// nothing installed but Go. Tests that need an external utility (tclsh,
// port-tclsh, git) ask this package for it and are skipped when it is
// absent. Skipping is loud in verbose mode but silent in CI summaries, so
// environments that are SUPPOSED to have the tools — a CI job that installs
// MacPorts, a maintainer's workstation — can set DOCKHAND_TEST_REQUIRE=1 to
// turn every would-be skip into a failure, catching a broken environment
// instead of quietly testing nothing.
package testenv

import (
	"os"
	"os/exec"
	"testing"
)

// Tool returns the path to the named executable, skipping the calling test
// if it is not on PATH — or failing it when DOCKHAND_TEST_REQUIRE is set.
func Tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("DOCKHAND_TEST_REQUIRE") != "" {
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
