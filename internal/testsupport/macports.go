package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// MacPortsTclsh is the port-tclsh MacPorts integration tests run against:
// DOCKHAND_TEST_MACPORTS_TCLSH, and never one found on PATH, which would
// test whichever Base PATH happens to reach. Without it the test skips.
func MacPortsTclsh(t testing.TB) string {
	t.Helper()
	executable := os.Getenv("DOCKHAND_TEST_MACPORTS_TCLSH")
	if executable == "" {
		t.Skip("set DOCKHAND_TEST_MACPORTS_TCLSH to a port-tclsh for MacPorts integration tests")
	}
	return executable
}

// MacPortsTool is another program of the installation MacPortsTclsh names,
// such as portindex, from the same bin directory.
func MacPortsTool(t testing.TB, name string) string {
	t.Helper()
	tool := filepath.Join(filepath.Dir(MacPortsTclsh(t)), name)
	if _, err := os.Stat(tool); err != nil {
		t.Skipf("MacPorts %s is not beside DOCKHAND_TEST_MACPORTS_TCLSH: %v", name, err)
	}
	return tool
}

// BaseAdapter is DOCKHAND_TEST_BASE_ADAPTER, the evaluator adapter the
// tests select: "preview" admits a development build of Base, such as
// master, which the evaluator otherwise refuses.
func BaseAdapter() string { return os.Getenv("DOCKHAND_TEST_BASE_ADAPTER") }
