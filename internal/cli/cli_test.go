package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/herbygillot/dockhand/internal/tool"
)

// run executes the command tree with output discarded, returning the
// execution error.
func runCLI(t *testing.T, args ...string) error {
	t.Helper()
	root := Root("test")
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

// code executes the command tree the way main does, returning the
// process exit code.
func code(t *testing.T, args ...string) int {
	t.Helper()
	return execute(context.Background(), "test", args, io.Discard, io.Discard)
}

// testTools is the tool finder a test may stand in for the real PATH
// search. It is a package variable rather than a parameter because the
// verbs under test build their own Services through Root, which is
// exactly the composition this package is; a test that wanted a
// different finder per call would be testing a different program.
var testTools *tool.Finder

// testFinder is the finder a fixture should carry: the stated one when a
// test stated one, else the real PATH search — the composition root's
// own answer, for the verbs whose behaviour does not depend on tart.
func testFinder() *tool.Finder {
	if testTools != nil {
		return testTools
	}
	return tool.NewFinder(nil)
}

// stubTool makes one binary lookup answer a fixed path and error,
// leaving every other lookup to the real search.
func stubTool(t *testing.T, name tool.Tool, path string, err error) {
	t.Helper()
	prev := testTools
	testTools = tool.NewFinder(func(want string) (string, error) {
		if want == string(name) {
			return path, err
		}
		return exec.LookPath(want)
	})
	t.Cleanup(func() { testTools = prev })
}

// requireNoTree clears the tree environment so a test's invocation
// cannot pick up the developer's own checkout.
func requireNoTree(t *testing.T) {
	t.Helper()
	t.Setenv("DOCKHAND_TREE", "")
	t.Setenv("DOCKHAND_PREFIX", "")
	t.Chdir(t.TempDir())
	_ = os.Getenv // the chdir is the whole of it; named so the reason is visible
}
