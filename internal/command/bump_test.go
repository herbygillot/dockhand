package command

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestBumpGoesFromUpdateToPullRequestAskingNothing(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	out, errs, err := cmdtest.BumpOn(t, "jq")
	require.NoError(t, err, errs)
	require.NotRegexp(t, `\[y/N\]|\[Y/n\]|Apply \[a\]`, errs, "bump asks nothing, even on a terminal")
	require.Regexp(t, `Started dockhand/jq-1\.8\.1 from master `, out)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.PRs, 1)
	require.Contains(t, g.PRs[34901].Body, "- [ ] tested basic functionality of all binary files?", "only a person can say that")

	// Run again, it resumes the branch, already at that release and
	// submitted.
	out, errs, err = cmdtest.BumpOn(t, "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq-1.8.1  existing branch; update if needed to the newest release, to submit\n")
	require.Contains(t, out, "jq is already at 1.8.1; nothing to change.\n")
	require.Contains(t, out, "#34901 has nothing new")
	require.Len(t, g.PRs, 1)
}

// bump passes submit's title, note, and [skip notification] on, so a
// test's pull request can say it is one (the rc6 full stage, B2).
func TestBumpPassesSubmitsWordsOn(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, errs, err := cmdtest.BumpOn(t, "jq", "--title", "[testing] jq: update to 1.8.1", "--note", "A test; it will be closed.", "--skip-notification")
	require.NoError(t, err, errs)
	require.Len(t, g.PRs, 1)
	require.Equal(t, "[testing] jq: update to 1.8.1", g.PRs[34901].Title)
	require.Contains(t, g.PRs[34901].Body, "A test; it will be closed.")
	require.Contains(t, g.PRs[34901].Body, "[skip notification]")

	_, _, err = cmdtest.Dockhand(t, "update", "jq", "--title", "x")
	require.ErrorContains(t, err, "unknown flag: --title", "update is the edit alone")
}

// What would stop bump before the edit stops it with nothing changed.
func TestBumpChangesNothingWhenItHasNothingToDo(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.8.1")
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	cmdtest.WithGitHub(t, w)

	out, _, err := cmdtest.BumpOn(t, "jq")
	require.NoError(t, err)
	require.Equal(t, "  jq  a version update from master, to submit\njq is already at 1.8.1; nothing to change, so no branch was started.\n", out)
	out, _, err = cmdtest.BumpOn(t, "jq", "1.8.1")
	require.NoError(t, err)
	require.Equal(t, "  jq  a version update from master, to submit\njq is already at 1.8.1; nothing to change, so no branch was started.\n", out)

	_, _, err = cmdtest.BumpOn(t, "jq", "1.9", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	_, _, err = cmdtest.BumpOn(t, "jq", "--except", "fd")
	require.EqualError(t, err, "--except takes a port out of --revbump-dependents; add --revbump-dependents")
	_, _, err = cmdtest.BumpOn(t, "jq", "--submit")
	require.ErrorContains(t, err, "unknown flag: --submit", "bump is ship <port> --yes already")

	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
}

// ship and bump report one result: each change's update, with each step
// it went on to inside it, as far as it went. bump's can hold its
// submission, and says why.
func TestShipAndBumpReportTheSameJSON(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	for _, command := range [][]string{{"ship", "jq", "--yes"}, {"bump", "jq"}} {
		cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
		submitted, err := cmdtest.JSONOf(t, command...)
		require.NoError(t, err, command)
		require.Len(t, submitted.Result["changes"], 1, command)
		result := cmdtest.Dig(t, submitted.Result, "changes", 0).(map[string]any)
		require.Equal(t, "1.8.1", cmdtest.Dig(t, result, "after", "version"), command)
		require.Equal(t, true, result["started"], command)
		require.Equal(t, true, result["applied"], command)
		require.Equal(t, "jq: update to 1.8.1", cmdtest.Dig(t, result, "tidy", "commits", 0, "subject"), command)
		require.NotNil(t, cmdtest.Dig(t, result, "tidy", "applied"), command)
		require.Equal(t, "passed", cmdtest.Dig(t, result, "check", "run", "state"), command)
		require.Equal(t, true, cmdtest.Dig(t, result, "submit", "pull_request", "created"), command)
		require.NotContains(t, result["submit"], "held", command)
		// Out of the way, so bump starts a branch of its own for jq.
		_, _, err = cmdtest.Dockhand(t, "archive", cmdtest.Dig(t, result, "branch", "name").(string))
		require.NoError(t, err)
	}

	// #34777 opens while jq is checked, after bump looked before its edit.
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	g.Quiet = g.Searches + 1
	held, err := cmdtest.JSONOf(t, "bump", "jq")
	require.Equal(t, 3, term.ExitCode(err))
	change := cmdtest.Dig(t, held.Result, "changes", 0).(map[string]any)
	require.Equal(t, "1.8.1", cmdtest.Dig(t, change, "after", "version"))
	require.Nil(t, cmdtest.Dig(t, change, "submit", "pull_request"))
	require.Equal(t, []any{"#34777 is open for the same port: jq: update to 1.8.0"}, cmdtest.Dig(t, change, "submit", "held"))
}

// Nobody looks before bump submits, so what holds serve's pull requests
// holds bump's: here, another pull request open for the port. It's looked
// for before the edit, so one already open stops bump before anything is
// downloaded or built, as does not knowing; and again before submitting,
// since one may have opened meanwhile (the update-workflow review's
// efficiency item).
func TestBumpHoldsWhatServeWould(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w) // it finds #34777 open for jq

	_, _, err := cmdtest.BumpOn(t, "jq", "--tested-binaries")
	require.Equal(t, 3, term.ExitCode(err), "it needs your attention")
	require.EqualError(t, err, "jq waits for your look, so nothing was changed: #34777 is open for the same port: jq: update to 1.8.0\nOnce it's fine: dockhand ship jq, on a terminal")
	require.Equal(t, 1, g.Searches, "looked for once, before the edit")
	g.SearchErr = errors.New("rate limited")
	g.Others = nil
	_, _, err = cmdtest.BumpOn(t, "jq", "1.8.1")
	require.Equal(t, 3, term.ExitCode(err))
	require.EqualError(t, err, "jq waits for your look, so nothing was changed: couldn't look for other open pull requests: rate limited\nOnce it's fine: dockhand update jq 1.8.1 --new, then dockhand ship -p jq on a terminal")
	held, err := cmdtest.JSONOf(t, "bump", "jq")
	require.Equal(t, 3, term.ExitCode(err))
	require.Nil(t, held.Result, "a refusal before anything is done reports only its error")
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started, so nothing was downloaded or built")

	// #34777 opens while jq is checked.
	g.SearchErr, g.Others = nil, []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	g.Quiet = g.Searches + 1
	_, _, err = cmdtest.BumpOn(t, "jq", "--tested-binaries")
	require.Equal(t, 3, term.ExitCode(err), "it needs your attention")
	require.Regexp(t, `^jq-1\.8\.1(-[a-z0-9]{4})? passed its check and waits for your look, so nothing was submitted: #34777 is open for the same port: jq: update to 1\.8\.0\nOnce it's fine: dockhand submit --branch jq-1\.8\.1(-[a-z0-9]{4})?$`, err.Error())
	require.Empty(t, g.PRs)

	name := regexp.MustCompile(`--branch (jq-1\.8\.1(?:-[a-z0-9]{4})?)$`).FindStringSubmatch(err.Error())[1]
	out, _, err := cmdtest.Dockhand(t, "submit", "--branch", name, "--yes", "--tested-binaries")
	require.NoError(t, err)
	require.Contains(t, out, "Opened #34901", "a person's submit after a look needs no new check")
}
