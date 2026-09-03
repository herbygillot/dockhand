package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/sweep"
)

// selectorTree builds a ports tree with a PortIndex, enough for the
// grammar to be exercised through the command surface. Resolution is
// all these tests reach: an evaluator would be needed to classify
// anything, and the arguments are wrong long before one is asked for.
func selectorTree(t *testing.T, portdirs ...string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	var index strings.Builder
	for _, pd := range portdirs {
		dir := filepath.Join(root, filepath.FromSlash(pd))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
			[]byte("PortSystem 1.0\n"), 0o644))
		body := fmt.Sprintf("name %s portdir %s\n", filepath.Base(pd), pd)
		fmt.Fprintf(&index, "%s %d\n%s", filepath.Base(pd), len(utf16.Encode([]rune(body))), body)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile),
		[]byte(index.String()), 0o644))
	return root
}

// The two usage errors --all can raise still come in the order they
// always have: the tree is opened before the arity is checked, so a
// user who typed --all at a directory that is not a ports tree hears
// about the tree.
func TestClassifyAllErrorOrderIsUnchanged(t *testing.T) {
	err := run(t, "classify", "-a", "-t", t.TempDir(), "foo")
	require.ErrorIs(t, err, tree.ErrNotPortsTree)

	root := selectorTree(t, "devel/ivy")
	err = run(t, "classify", "-a", "-t", root, "foo")
	require.ErrorContains(t, err, "--all takes no arguments")
	assert.Equal(t, exitcode.Usage, ExitCode(err))
}

// The grammar's own errors reach the process with a band on them,
// rather than as a bare failure: a maintainer nobody in the tree names
// is a mistake in the invocation.
func TestClassifySelectorErrors(t *testing.T) {
	root := selectorTree(t, "devel/ivy", "lang/ruby")

	err := run(t, "classify", "-t", root, "maintainer:nobody")
	require.ErrorIs(t, err, sweep.ErrNoMaintainer)

	// "me" is asked of the machine, not of a seam somebody forgot to
	// wire. Which of the two answers comes back depends on the machine
	// — a checkout with a git identity or an authenticated gh resolves
	// a key and this synthetic tree names nobody under it; a machine
	// with neither cannot tell who you are — and both are answers about
	// the user. What must never come back is the third thing: a message
	// saying a lookup "is wired", which describes a bug in dockhand and
	// was what this form used to say on every verb but the write ones.
	err = run(t, "classify", "-t", root, "maintainer:me")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "is wired",
		"the selector reached the grammar with half its lookups")
	assert.True(t,
		errorsIsAny(err, sweep.ErrNoMaintainer, sweep.ErrNoIdentity),
		"unexpected answer for maintainer:me: %v", err)

	err = run(t, "classify", "-t", root, "category:nosuch")
	require.ErrorIs(t, err, tree.ErrPortNotFound)
	assert.Equal(t, exitcode.PortNotFound, ExitCode(err))
}

// `outdated` advertises maintainer:me in its own usage line, so it has
// to be able to answer it. It resolves through the same sources every
// other verb does, and reaches the same two machine-dependent answers.
func TestOutdatedResolvesTheMaintainerForms(t *testing.T) {
	root := selectorTree(t, "devel/ivy", "lang/ruby")

	err := run(t, "outdated", "-t", root, "maintainer:nobody")
	require.ErrorIs(t, err, sweep.ErrNoMaintainer)

	err = run(t, "outdated", "-t", root, "maintainer:me")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "is wired",
		"the verb whose usage line advertises maintainer:me cannot resolve it")
	assert.True(t,
		errorsIsAny(err, sweep.ErrNoMaintainer, sweep.ErrNoIdentity),
		"unexpected answer for maintainer:me: %v", err)
}

// errorsIsAny reports whether err is any of the targets.
func errorsIsAny(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// A bare <category>/<port> token names the port on every road into the
// grammar, and it is pinned at the command layer because this is the
// one single-target invocation whose answer moved.
//
// It used to stat as a directory, be read as a CATEGORY, expand to the
// portdirs under it — of which a portdir has none — and name zero
// ports: `classify sysutils/jq` reported "0 ports classified" and
// `bump --plan sysutils/jq` refused in the usage band with "names 0"
// rather than reaching the port. Neither gate sees it — the live proof
// passes absolute portdirs and no golden drives the bare relative form
// — so the pin is here.
func TestSelectorReadsCategorySlashPortAsThePort(t *testing.T) {
	root := selectorTree(t, "sysutils/jq", "sysutils/kubectl")
	// From outside the tree, so nothing resolves as a relative path.
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: root, Tools: testFinder(), Out: &out, Err: &errb}
	ctx := context.Background()

	// The report road: classify and outdated resolve through this.
	targets, err := resolveTargets(ctx, rs, false, []string{"sysutils/jq"})
	require.NoError(t, err)
	require.Len(t, targets, 1, "a category/port token named the category and swept nothing")
	assert.Equal(t, filepath.Join(root, "sysutils", "jq"), targets[0].Portdir)

	// The write road: one port, so it is the single-port road and not a
	// sweep, which is what makes it a single-target invocation at all.
	res, err := resolveSelector(ctx, rs, "sysutils/jq")
	require.NoError(t, err)
	require.Len(t, res.Targets, 1)
	assert.Equal(t, filepath.Join(root, "sysutils", "jq"), res.Targets[0].Portdir)

	// The bare category still sweeps, which is the reading that must
	// not have moved.
	targets, err = resolveTargets(ctx, rs, false, []string{"sysutils"})
	require.NoError(t, err)
	assert.Len(t, targets, 2)
}
