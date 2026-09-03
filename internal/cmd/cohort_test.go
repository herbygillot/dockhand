package cmd

// The two verbs that answer a proposal, at the command line.
//
// What they DO is the engine's and is proved there, over a real
// PortIndex slice and scripted manifests. What is proved here is the
// surface: that `bump-revision --for <branch>` is reachable, takes no
// port, needs no --reason, refuses the flag combinations that would
// silently do nothing, and that `dismiss` is a verb the tree registers.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// --for names a branch and every member on it, so the verb takes no
// port — and the arity check has to ask the flag before it counts
// arguments, which is the one thing cobra's own ExactArgs cannot do.
func TestTheCohortVerbTakesABranchAndNoPort(t *testing.T) {
	// No port, no --reason: neither is missing, because the proposal
	// holds both. It reaches the action and fails on the tree, which is
	// the next thing that could go wrong rather than the invocation.
	assert.NotEqual(t, exitcode.Usage,
		code(t, "bump-revision", "--for", "dockhand/jq-1.8", "-t", t.TempDir()),
		"--for needs neither a port nor a --reason")

	// And a port BESIDE --for is the invocation being wrong: one of the
	// two says which ports change, and they disagree.
	assert.Equal(t, exitcode.Usage,
		code(t, "bump-revision", "--for", "dockhand/jq-1.8", "jq", "-t", t.TempDir()))

	// The single-port road is untouched: still one port, still --reason.
	assert.Equal(t, exitcode.Usage, code(t, "bump-revision", "-t", t.TempDir()))
	assert.Equal(t, exitcode.Usage, code(t, "bump-revision", "jq", "-t", t.TempDir()),
		"a revision bump still needs --reason")
}

// The combinations --for refuses are the ones that would silently do
// nothing: a cohort is N plans grafted onto a branch that already
// exists, so there is no single document to print and no working tree
// to edit.
func TestTheCohortVerbRefusesTheRealizationsItHasNoAnswerFor(t *testing.T) {
	for _, flag := range []string{"--plan", "--diff", "--in-place", "--riders", "--no-riders", "--replace", "--verify"} {
		assert.Equal(t, exitcode.Usage,
			code(t, "bump-revision", "--for", "dockhand/jq-1.8", flag, "-t", t.TempDir()),
			"%s has no meaning for a cohort", flag)
	}

	// --test and --trace are carried rather than refused: the cohort's
	// own verification is a verification, and each member rebuilds from
	// source against the new library.
	for _, flag := range []string{"--test", "--trace"} {
		assert.NotEqual(t, exitcode.Usage,
			code(t, "bump-revision", "--for", "dockhand/jq-1.8", flag, "-t", t.TempDir()),
			"%s rides the cohort's own verification", flag)
	}
}

// dismiss is a verb the tree registers, taking a branch or a port.
func TestDismissIsRegisteredAndTakesOneTarget(t *testing.T) {
	root := Root("test")
	c, _, err := root.Find([]string{"dismiss"})
	require.NoError(t, err)
	assert.Equal(t, "dismiss", c.Name())
	assert.Equal(t, "test", c.GroupID, "it answers what a verification found")

	assert.Equal(t, exitcode.Usage, code(t, "dismiss", "-t", t.TempDir()))
	assert.Equal(t, exitcode.Usage, code(t, "dismiss", "a", "b", "-t", t.TempDir()))
}
