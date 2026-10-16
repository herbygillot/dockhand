package command

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// asMaintainer names the person as the fixture's maintainers lines do,
// for --mine.
func asMaintainer(t *testing.T, w world) {
	t.Helper()
	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o755))
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"@ada\"\n"), data...), 0o644))
}

// shipAnswering runs ship on a terminal that answers.
func shipAnswering(t *testing.T, answers string, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := Run(t.Context(), append([]string{"ship"}, args...), Streams{In: strings.NewReader(answers), Out: &out, Err: &errs, interactive: true})
	return out.String(), errs.String(), err
}

// ship carries a port from master to its pull request, showing its plan and
// asking once; run again, it resumes the branch, here already submitted
// (the command-line UX second look's §6).
func TestShipCarriesAPortToItsPullRequest(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	out, errs, err := shipAnswering(t, "n\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq  a version update from master, to submit\n")
	require.Contains(t, errs, "? Ship it? [y/N] ")
	require.Contains(t, out, "Nothing was shipped.")
	require.Empty(t, g.PRs)

	out, errs, err = shipAnswering(t, "y\nn\nn\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Regexp(t, `jq: 1.7.1 → 1.8.1 .*\n(.*\n)*\njq-1\.8\.1 · tidying edits not yet committed\n\nProposed commit\n`, out)
	require.NotContains(t, out, "Next: review it")
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.PRs, 1)

	out, _, err = dockhand(t, "ship", "jq", "--to", "check")
	require.NoError(t, err)
	require.Contains(t, out, "  jq-1.8.1  its branch, updated again where it isn't at the newest release, to check\n", "resumed, not started again")
	require.Contains(t, out, "jq is already at 1.8.1; nothing to change.\n")
	require.Contains(t, out, "passed for this commit already; nothing to check")
}

// --to commit stops at the commit, and --to names a stage.
func TestShipStopsWhereToSays(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	_, _, err := dockhand(t, "ship", "jq", "--to", "review")
	require.EqualError(t, err, "--to review: commit, check, or submit")
	out, errs, err := dockhand(t, "ship", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.NotContains(t, out, "checking commit ")
	require.Empty(t, g.PRs)
	_, _, err = dockhand(t, "ship", "jq", "-b", "jq-1.8.1")
	require.EqualError(t, err, "name ports, --mine, --passing, or a branch: one of them")
}

// --mine ships the person's ports outdated --mine finds newer releases of,
// at the release it found, and says what it leaves; --passing, the
// branches whose checks passed and aren't submitted.
func TestShipMineAndPassing(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	reader := withOutdated(t)
	g := withGitHub(t, w)
	g.Others = nil
	asMaintainer(t, w)

	_, _, err := dockhand(t, "ship", "--mine", "jq")
	require.EqualError(t, err, "name ports, --mine, --passing, or a branch: one of them")
	out, errs, err := dockhand(t, "ship", "--mine", "--to", "check")
	require.NoError(t, err, errs)
	require.Equal(t, []string{"@ada"}, reader.asked[0].Maintainers)
	require.Contains(t, out, "  lost: left as it is: no forge could be found for it\n")
	require.Contains(t, out, "  jq  a version update from master, to check\n")
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Empty(t, g.PRs)

	out, errs, err = dockhand(t, "ship", "--passing", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq-1.8.1  its branch, where it stands, to commit\n")
}

// A batch queues every change's check before it sees any through, and
// submits each as its check passes (decision 4).
func TestShipQueuesABatchsChecksFirst(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	require.NoError(t, os.MkdirAll(filepath.Join(w.upstream, "textproc/yq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/yq/Portfile"), []byte("name yq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.upstream, "add", ".")
	testsupport.Git(t, w.upstream, "commit", "-q", "-m", "yq: 1.7.1")
	withBumper(t)
	withScript(t, w, "passed")
	require.NoError(t, os.WriteFile(filepath.Join(w.home, "bin", "build-ports"), []byte(`#!/bin/sh
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}, {"id": "yq", "outcome": "passed"}]}
JSON
`), 0o755))
	g := withGitHub(t, w)
	g.Others = nil

	// The bumper stands in jq's release for any port, so yq's commit is
	// held by the commit rules: one change held, and the rest goes on.
	var both bytes.Buffer
	err := Run(t.Context(), []string{"ship", "jq", "yq", "--yes"}, Streams{In: strings.NewReader(""), Out: &both, Err: &both})
	out := both.String()
	require.Equal(t, 3, ExitCode(err), out)
	require.EqualError(t, err, "yq-1.8.1 stopped; the rest went on")
	jq, yq, ran := strings.Index(out, "jq-1.8.1 · checking commit "), strings.Index(out, "yq-1.8.1 · checking commit "), strings.Index(out, "check-1 runs here")
	require.True(t, jq >= 0 && yq > jq && ran > yq, "both queued before either ran:\n%s", out)
	require.Contains(t, out, "yq-1.8.1: yq-1.8.1 passed its check and waits for your look")
	require.Len(t, g.PRs, 1)
}
