package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// jqIsOutdated stands in for upstream discovery: jq has 1.8.1, and lost
// can't be checked.
type jqIsOutdated struct{ asked []engine.OutdatedRequest }

func (j *jqIsOutdated) Outdated(_ context.Context, _ model.ObjectID, request engine.OutdatedRequest) ([]engine.OutdatedPort, error) {
	j.asked = append(j.asked, request)
	return []engine.OutdatedPort{
		{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true},
		{Port: "lost", Problem: "no forge could be found for it"},
	}, nil
}

func withOutdated(t *testing.T) *jqIsOutdated {
	reader := &jqIsOutdated{}
	testOutdatedReader = reader
	t.Cleanup(func() { testOutdatedReader = nil })
	return reader
}

func TestOutdatedThenUpdateOutdated(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	reader := withOutdated(t)

	_, _, err := dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `--mine needs to know who you are: set maintainer = "{@you example.org:you}"`)
	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), data...), 0o644))

	out, _, err := dockhand(t, "outdated", "--mine")
	require.NoError(t, err)
	require.Equal(t, []string{"@ada", "example.org:ada"}, reader.asked[0].Maintainers)
	require.Contains(t, out, "  PORT   NOW     NEWEST   DOCKHAND CAN\n  jq     1.7.1   1.8.1    update\n")
	require.Contains(t, out, "1 of 2 ports have newer releases, at master ")
	require.Contains(t, out, " · 1 couldn't be checked (--all says why)\n")
	out, _, err = dockhand(t, "outdated", "--mine", "--all")
	require.NoError(t, err)
	require.Contains(t, out, "couldn't check: no forge could be found for it")

	out, _, err = dockhand(t, "update", "--outdated", "--mine", "--check")
	require.ErrorContains(t, err, "without a terminal, --yes starts what is shown")
	require.Contains(t, out, "Will start 1 branch, one per port (unrelated ports go in separate PRs):\n  dockhand/jq-")
	require.Contains(t, out, "Skipped: lost (no forge could be found for it)\n")

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"update", "--outdated", "--mine", "--check"}, Streams{In: strings.NewReader("y\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? go ahead? [y/N] ")
	require.Regexp(t, `  ✓ jq-[a-z0-9]{4}: 1\.7\.1 → 1\.8\.1, one commit, check-1 queued\n`, stdout.String())
	require.Contains(t, stdout.String(), "1 branch updated and tidied into one commit each; 1 check queued\nserve isn't running: dockhand serve, or dockhand wait to run them here\n")

	out, _, err = dockhand(t, "outdated", "jq")
	require.NoError(t, err)
	require.Regexp(t, `jq     1\.7\.1   1\.8\.1    already in jq-[a-z0-9]{4}\n`, out)
	out, _, err = dockhand(t, "update", "--outdated", "jq", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Nothing to update: none of them has a newer release that isn't already in a branch.\nSkipped: jq (already in jq-")

	_, _, err = dockhand(t, "update", "--mine")
	require.ErrorContains(t, err, "--mine and --check go with --outdated")
	_, _, err = dockhand(t, "update")
	require.ErrorContains(t, err, "name the port to update, or update your outdated ports with --outdated --mine")
}
