package command

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// releases stands in for upstream discovery with the ports as given.
type releases []engine.OutdatedPort

func (r releases) Outdated(context.Context, model.ObjectID, engine.OutdatedRequest) ([]engine.OutdatedPort, error) {
	return r, nil
}

// bumpOn runs bump where a person could answer, to show it asks nothing.
func bumpOn(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := Run(t.Context(), append([]string{"bump"}, args...), Streams{In: strings.NewReader("y\ny\ny\n"), Out: &out, Err: &errs, interactive: true})
	return out.String(), errs.String(), err
}

func TestBumpGoesFromUpdateToPullRequestAskingNothing(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	withOutdated(t)
	g := withGitHub(t, w)
	g.others = nil

	out, errs, err := bumpOn(t, "jq")
	require.NoError(t, err, errs)
	require.NotRegexp(t, `\[y/N\]|\[Y/n\]|Apply \[a\]`, errs, "bump asks nothing, even on a terminal")
	require.Regexp(t, `Started dockhand/jq-[a-z0-9]{4} from master `, out)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.prs, 1)
	require.Contains(t, g.prs[0].Body, "- [ ] tested basic functionality of all binary files?", "only a person can say that")

	_, _, err = bumpOn(t, "jq")
	require.Regexp(t, `^jq is already changed in jq-[a-z0-9]{4}, so nothing was changed; dockhand status jq-[a-z0-9]{4} says what it needs$`, err.Error())
	require.Len(t, g.prs, 1)
}

// What would stop bump before the edit stops it with nothing changed.
func TestBumpChangesNothingWhenItHasNothingToDo(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	withGitHub(t, w)
	testOutdatedReader = releases{{Port: "jq", Current: "1.8.1", Newest: "1.8.1"}, {Port: "lost", Problem: "no forge could be found for it"}}
	t.Cleanup(func() { testOutdatedReader = nil })

	out, _, err := bumpOn(t, "jq")
	require.NoError(t, err)
	require.Equal(t, "jq is already at 1.8.1, the newest release; nothing to change.\n", out)
	out, _, err = bumpOn(t, "jq", "1.8.1")
	require.NoError(t, err)
	require.Equal(t, "jq is already at 1.8.1; nothing to change.\n", out)

	_, _, err = bumpOn(t, "lost")
	require.EqualError(t, err, "can't tell whether lost has a newer release: no forge could be found for it; nothing was changed")
	_, _, err = bumpOn(t, "jq", "1.9", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	_, _, err = bumpOn(t, "jq", "--except", "fd")
	require.EqualError(t, err, "--except takes a port out of --revbump-dependents; add --revbump-dependents")
	_, _, err = bumpOn(t, "jq", "--submit")
	require.ErrorContains(t, err, "unknown flag: --submit", "bump is update --new --submit --yes already")

	require.Empty(t, gitRun(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")
}

// Nobody looks before bump submits, so what holds serve's pull requests
// holds bump's: here, another pull request open for the port.
func TestBumpHoldsWhatServeWould(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	withOutdated(t)
	g := withGitHub(t, w) // it finds #34777 open for jq

	_, _, err := bumpOn(t, "jq", "--tested-binaries")
	require.Equal(t, 3, ExitCode(err), "it needs your attention")
	require.Regexp(t, `^jq-[a-z0-9]{4} passed its check and waits for your look, so nothing was submitted: #34777 is open for the same port: jq: update to 1\.8\.0\nOnce it's fine: dockhand submit --branch jq-[a-z0-9]{4}$`, err.Error())
	require.Empty(t, g.prs)

	name := regexp.MustCompile(`--branch (jq-[a-z0-9]{4})$`).FindStringSubmatch(err.Error())[1]
	out, _, err := dockhand(t, "submit", "--branch", name, "--yes", "--tested-binaries")
	require.NoError(t, err)
	require.Contains(t, out, "Opened #34901", "a person's submit after a look needs no new check")
}
