package check

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/stretchr/testify/require"
)

func TestBaselineComparesWithTheBase(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	next := cmdtest.WithOutcomeScript(t, w, "")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = cmdtest.Dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "has no finished check to compare with")
	next("passed")
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "nothing failed in check-1, so there is nothing to compare; name ports with --only")

	// A failed check points to the baseline.
	next("failed")
	out, _, err := cmdtest.Dockhand(t, "check")
	require.Equal(t, 2, term.ExitCode(err))
	require.Regexp(t, "\n\nTo see whether jq fails at master [0-9a-f]{7} too: dockhand check --baseline --branch jq-update\n$", out)
	// --plan shows the baseline, and runs nothing: the numbers below would
	// move if it had (the libuv run's finding 4).
	previewed, _, err := cmdtest.Dockhand(t, "check", "--baseline", "--plan")
	require.NoError(t, err)
	require.Contains(t, previewed, "jq-update · baseline of check-2: jq at master ")
	require.Contains(t, previewed, "Also        jq\nProvider    command · tests declared\n")
	next("passed")
	out, _, err = cmdtest.Dockhand(t, "check", "--baseline")
	require.NoError(t, err, "a baseline is evidence, and never fails")
	require.Contains(t, out, "jq-update · baseline of check-2: jq at master ")
	require.Contains(t, out, "jq at master ")
	require.Contains(t, out, "  ✓ builds at the base. This branch's result differs (check-2); the cause isn't established.\n")

	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "check-2", "the branch's check is still its latest")
	require.NotContains(t, out, "passed for", "a baseline's pass is not the branch's")
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--head")
	require.Error(t, err, "nothing committed yet; the baseline changed nothing about that")

	// check.baseline runs one by itself after a failure.
	cmdtest.WithOutcomeScript(t, w, "[check]\nbaseline = true\n")
	next("failed")
	out, _, err = cmdtest.Dockhand(t, "check")
	require.Equal(t, 2, term.ExitCode(err))
	require.Contains(t, out, "dockhand check --baseline --branch jq-update\nRunning a baseline comparison because check.baseline is enabled:\njq-update · baseline of check-4: jq at master ")
	require.Contains(t, out, "  ✗ fails at the base too, at install. Both results are kept; the cause isn't established.\n")

	// A port that failed before building is the branch's own doing: no
	// hint, and check.baseline doesn't run.
	next("failed fetch")
	out, _, err = cmdtest.Dockhand(t, "check")
	require.Equal(t, 2, term.ExitCode(err))
	require.Contains(t, out, "  jq  ✗ failed at fetch\n")
	require.NotContains(t, out, "--baseline")
	require.NotContains(t, out, "check.baseline")
	_, _, err = cmdtest.Dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "jq failed only before building, at lint, fetch, or checksum, which master can't speak to; --only builds them there anyway")

	// Only the branch's newest check points to it, the one --baseline
	// looks into.
	out, _, err = cmdtest.Dockhand(t, "wait", "check-4")
	require.Equal(t, 2, term.ExitCode(err))
	require.NotContains(t, out, "--baseline")
}

// check reports the check's result whatever follows it: a baseline
// check.baseline runs is inside it, marked as that check's baseline.
func TestCheckReportsItsBaselineInsideIt(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	next := cmdtest.WithOutcomeScript(t, w, "[check]\nbaseline = true\n")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)

	next("failed")
	checked, err := cmdtest.JSONOf(t, "check")
	require.Equal(t, 2, term.ExitCode(err))
	require.Equal(t, "check-1", cmdtest.Dig(t, checked.Result, "run", "name"))
	require.Equal(t, "failed", cmdtest.Dig(t, checked.Result, "run", "state"))
	require.NotContains(t, checked.Result["run"], "baseline_of", "the check is the branch's own")
	require.Equal(t, cmdtest.Dig(t, checked.Result, "run", "id"), cmdtest.Dig(t, checked.Result, "baseline", "run", "baseline_of"))
}
