package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// decide --waiting leaves a concern for later, and submit refuses the
// branch until it's decided --because; a decision with no reason, or
// both, is asked to say why, where no command test drove --waiting (the
// test analysis's step b).
func TestDecideWaitingHoldsTheBranch(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "tidy", "-y")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "decide", "license-changed")
	require.ErrorContains(t, err, `say why: --because "<reason>", or --waiting "<what for>"`)
	out, _, err := cmdtest.Dockhand(t, "decide", "license-changed", "--waiting", "the author's answer on the license")
	require.NoError(t, err)
	require.Contains(t, out, "the author's answer on the license")
	listed, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, listed, "the author's answer on the license")
	planned, errs, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.ErrorContains(t, err, "nothing was submitted")
	require.Contains(t, cmdtest.Flat(planned+errs), "✗ jq license-changed: you're awaiting the author's answer on the license. Decide it once you have it", "submit holds it, saying what for")
}

// start --here starts a branch in the checkout it's run in, switching it,
// and start --from takes only a merged branch's commits, refusing an open
// one with where its work is, where no command test drove either.
func TestStartHereAndFrom(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-here", "--here")
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-here", testsupport.Git(t, w.Clone, "branch", "--show-current"), "the checkout it ran in")
	_, _, err = cmdtest.Dockhand(t, "start", "--from", "jq-here", "jq-followup")
	require.ErrorContains(t, err, "start --from takes a merged branch's commits past its merge, and jq-here is open; dockhand path jq-here works in it")
	require.NoDirExists(t, filepath.Join(w.Home, "Source", "macports-branches", "jq-followup"))
}

// A plan saved with --plan --out that needs words dockhand can't give, a
// change outside any port's version, is shown and refused by tidy --apply
// as not appliable yet, nothing committed, where no test reached it.
func TestTidyApplyRefusesASavedPlanThatNeedsWords(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	dir := w.Clone
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-docs", "--here")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# documented\n"), 0o644))
	before := testsupport.Git(t, dir, "rev-parse", "HEAD")
	saved := filepath.Join(t.TempDir(), "plan.json")
	_, _, err = cmdtest.Dockhand(t, "tidy", "--plan", "--out", saved)
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "tidy", "--apply", saved)
	require.ErrorContains(t, err, "the plan can't be applied yet: ")
	require.Contains(t, cmdtest.Flat(out), "the plan saved in "+saved)
	require.Contains(t, out, "commit 1 needs a subject")
	require.Equal(t, before, testsupport.Git(t, dir, "rev-parse", "HEAD"), "nothing committed")
}
