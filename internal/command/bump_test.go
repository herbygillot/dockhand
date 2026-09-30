package command

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
)

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
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: 1.8.1")
	withBumper(t)
	withScript(t, w, "passed")
	withGitHub(t, w)

	out, _, err := bumpOn(t, "jq")
	require.NoError(t, err)
	require.Equal(t, "jq is already at 1.8.1; nothing to change, so no branch was started.\n", out)
	out, _, err = bumpOn(t, "jq", "1.8.1")
	require.NoError(t, err)
	require.Equal(t, "jq is already at 1.8.1; nothing to change, so no branch was started.\n", out)

	_, _, err = bumpOn(t, "jq", "1.9", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	_, _, err = bumpOn(t, "jq", "--except", "fd")
	require.EqualError(t, err, "--except takes a port out of --revbump-dependents; add --revbump-dependents")
	_, _, err = bumpOn(t, "jq", "--submit")
	require.ErrorContains(t, err, "unknown flag: --submit", "bump is update --new --submit --yes already")

	require.Empty(t, gitRun(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")
}

// update --submit and bump report one result: the update's, with each step
// it went on to inside it, as far as it went. bump's can hold its
// submission, and says why.
func TestUpdateSubmitAndBumpReportTheSameJSON(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.others = nil

	for _, command := range [][]string{{"update", "jq", "--new", "--submit"}, {"bump", "jq"}} {
		t.Setenv("MACPORTS_TREE", w.clone)
		submitted, err := jsonOf(t, command...)
		require.NoError(t, err, command)
		result := submitted.Result
		require.Equal(t, "1.8.1", dig(t, result, "after", "version"), command)
		require.Equal(t, true, result["started"], command)
		require.Equal(t, true, result["applied"], command)
		require.Equal(t, "jq: update to 1.8.1", dig(t, result, "tidy", "commits", 0, "subject"), command)
		require.NotNil(t, dig(t, result, "tidy", "applied"), command)
		require.Equal(t, "passed", dig(t, result, "check", "run", "state"), command)
		require.Equal(t, true, dig(t, result, "submit", "pull_request", "created"), command)
		require.NotContains(t, result["submit"], "held", command)
		// Out of the way, so bump starts a branch of its own for jq.
		_, _, err = dockhand(t, "archive", dig(t, result, "branch", "name").(string))
		require.NoError(t, err)
	}

	// #34777 opens while jq is checked, after bump looked before its edit.
	g.others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	g.quiet = g.searches + 1
	held, err := jsonOf(t, "bump", "jq")
	require.Equal(t, 3, ExitCode(err))
	require.Equal(t, "1.8.1", dig(t, held.Result, "after", "version"))
	require.Nil(t, dig(t, held.Result, "submit", "pull_request"))
	require.Equal(t, []any{"#34777 is open for the same port: jq: update to 1.8.0"}, dig(t, held.Result, "submit", "held"))
}

// Nobody looks before bump submits, so what holds serve's pull requests
// holds bump's: here, another pull request open for the port. It's looked
// for before the edit, so one already open stops bump before anything is
// downloaded or built, as does not knowing; and again before submitting,
// since one may have opened meanwhile (the update-workflow review's
// efficiency item).
func TestBumpHoldsWhatServeWould(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w) // it finds #34777 open for jq

	_, _, err := bumpOn(t, "jq", "--tested-binaries")
	require.Equal(t, 3, ExitCode(err), "it needs your attention")
	require.EqualError(t, err, "jq waits for your look, so nothing was changed: #34777 is open for the same port: jq: update to 1.8.0\nOnce it's fine: dockhand update jq --new --submit")
	require.Equal(t, 1, g.searches, "looked for once, before the edit")
	g.searchErr = errors.New("rate limited")
	g.others = nil
	_, _, err = bumpOn(t, "jq", "1.8.1")
	require.Equal(t, 3, ExitCode(err))
	require.EqualError(t, err, "jq waits for your look, so nothing was changed: couldn't look for other open pull requests: rate limited\nOnce it's fine: dockhand update jq 1.8.1 --new --submit")
	held, err := jsonOf(t, "bump", "jq")
	require.Equal(t, 3, ExitCode(err))
	require.Nil(t, held.Result, "a refusal before anything is done reports only its error")
	require.Empty(t, gitRun(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started, so nothing was downloaded or built")

	// #34777 opens while jq is checked.
	g.searchErr, g.others = nil, []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	g.quiet = g.searches + 1
	_, _, err = bumpOn(t, "jq", "--tested-binaries")
	require.Equal(t, 3, ExitCode(err), "it needs your attention")
	require.Regexp(t, `^jq-[a-z0-9]{4} passed its check and waits for your look, so nothing was submitted: #34777 is open for the same port: jq: update to 1\.8\.0\nOnce it's fine: dockhand submit --branch jq-[a-z0-9]{4}$`, err.Error())
	require.Empty(t, g.prs)

	name := regexp.MustCompile(`--branch (jq-[a-z0-9]{4})$`).FindStringSubmatch(err.Error())[1]
	out, _, err := dockhand(t, "submit", "--branch", name, "--yes", "--tested-binaries")
	require.NoError(t, err)
	require.Contains(t, out, "Opened #34901", "a person's submit after a look needs no new check")
}
