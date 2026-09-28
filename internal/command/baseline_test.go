package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// withOutcomeScript configures a command provider that reports jq as
// ~/outcome says, "passed", or "failed" and the phase, install unless
// named, so a test can change what the next build does.
func withOutcomeScript(t *testing.T, w world, config string) func(string) {
	t.Helper()
	script := filepath.Join(w.home, "bin", "build-ports")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o755))
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
read -r outcome phase < "$HOME/outcome"
[ "$outcome" = failed ] && phase=", \"phase\": \"${phase:-install}\""
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "$outcome"$phase}]}
JSON
`), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\n"+config), 0o644))
	testPortReader = onePort{}
	t.Cleanup(func() { testPortReader = nil })
	return func(outcome string) {
		require.NoError(t, os.WriteFile(filepath.Join(w.home, "outcome"), []byte(outcome), 0o644))
	}
}

func TestBaselineComparesWithTheBase(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	next := withOutcomeScript(t, w, "")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "has no finished check to compare with")
	next("passed")
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "nothing failed in check-1, so there is nothing to compare; name ports with --only")

	// A failed check points to the baseline.
	next("failed")
	out, _, err := dockhand(t, "check")
	require.Equal(t, 2, ExitCode(err))
	require.Regexp(t, "\n\nTo see whether jq fails at master [0-9a-f]{7} too: dockhand check --baseline --branch jq-update\n$", out)
	next("passed")
	out, _, err = dockhand(t, "check", "--baseline")
	require.NoError(t, err, "a baseline is evidence, and never fails")
	require.Contains(t, out, "jq-update · baseline of check-2: jq at master ")
	require.Contains(t, out, "jq at master ")
	require.Contains(t, out, "  ✓ builds at the base. This branch's result differs (check-2); the cause isn't established.\n")

	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "check-2", "the branch's check is still its latest")
	require.NotContains(t, out, "passed for", "a baseline's pass is not the branch's")
	_, _, err = dockhand(t, "submit", "--no-check", "--yes", "--head")
	require.Error(t, err, "nothing committed yet; the baseline changed nothing about that")

	// check.baseline runs one by itself after a failure.
	withOutcomeScript(t, w, "[check]\nbaseline = true\n")
	next("failed")
	out, _, err = dockhand(t, "check")
	require.Equal(t, 2, ExitCode(err))
	require.Contains(t, out, "dockhand check --baseline --branch jq-update\ncheck.baseline runs it now:\njq-update · baseline of check-4: jq at master ")
	require.Contains(t, out, "  ✗ fails at the base too, at install. Both results are kept; the cause isn't established.\n")

	// A port that failed before building is the branch's own doing: no
	// hint, and check.baseline doesn't run.
	next("failed fetch")
	out, _, err = dockhand(t, "check")
	require.Equal(t, 2, ExitCode(err))
	require.Contains(t, out, "  jq  ✗ failed at fetch\n")
	require.NotContains(t, out, "--baseline")
	require.NotContains(t, out, "check.baseline")
	_, _, err = dockhand(t, "check", "--baseline")
	require.ErrorContains(t, err, "jq failed only before building, at lint, fetch, or checksum, which master can't speak to; --only builds them there anyway")

	// Only the branch's newest check points to it, the one --baseline
	// looks into.
	out, _, err = dockhand(t, "wait", "check-4")
	require.Equal(t, 2, ExitCode(err))
	require.NotContains(t, out, "--baseline")
}

// check reports the check's result whatever follows it: a baseline
// check.baseline runs is inside it, marked as that check's baseline.
func TestCheckReportsItsBaselineInsideIt(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	next := withOutcomeScript(t, w, "[check]\nbaseline = true\n")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	next("failed")
	checked, err := jsonOf(t, "check")
	require.Equal(t, 2, ExitCode(err))
	require.Equal(t, "check-1", dig(t, checked.Result, "run", "name"))
	require.Equal(t, "failed", dig(t, checked.Result, "run", "state"))
	require.NotContains(t, checked.Result["run"], "baseline_of", "the check is the branch's own")
	require.Equal(t, dig(t, checked.Result, "run", "id"), dig(t, checked.Result, "baseline", "run", "baseline_of"))
}
