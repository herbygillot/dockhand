package ship

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// shipAnswering runs ship on a terminal that answers.
func shipAnswering(t *testing.T, answers string, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), append([]string{"ship"}, args...), term.Streams{In: strings.NewReader(answers), Out: &out, Err: &errs}.AsTerminal())
	return out.String(), errs.String(), err
}

// ship carries a port from master to its pull request, showing its plan and
// asking once; run again, it resumes the branch, here already submitted
// (the command-line UX second look's §6).
func TestShipCarriesAPortToItsPullRequest(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	out, errs, err := shipAnswering(t, "n\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq  a version update from master, to submit\n")
	require.Contains(t, errs, "? Ship it? [y/N] ")
	require.Contains(t, out, "Nothing was shipped.")
	require.Empty(t, g.PRs)

	// A line a stage, and a stage's report where it asks: here, what was
	// tested, before the check queued (part 5, item 3).
	out, errs, err = shipAnswering(t, "y\nn\nn\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq 1.7.1 → 1.8.1 (GitHub tag jq-1.8.1), on branch jq-1.8.1\n  ✓ edited     textproc/jq/Portfile\n  ✓ committed  jq: update to 1.8.1\n")
	require.NotContains(t, out, "Proposed commit", "the commit is a line, asking nothing")
	require.NotContains(t, out, "Next: review it")
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "  ✓ checked    passed on command\n  ✓ submitted  opened #34901  https://github.com/macports/macports-ports/pull/34901\n")
	require.Len(t, g.PRs, 1)

	out, _, err = cmdtest.Dockhand(t, "ship", "jq", "--to", "check")
	require.NoError(t, err)
	require.Contains(t, out, "  jq-1.8.1  existing branch; update if needed to the newest release, to check\n", "resumed, not started again")
	require.Contains(t, out, "jq is already at 1.8.1; nothing to change.\n")
	require.Contains(t, out, "passed for this commit already; nothing to check")
}

// --to commit stops at the commit, and --to names a stage.
func TestShipStopsWhereToSays(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, _, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "review")
	require.EqualError(t, err, "--to review: commit, check, or submit")
	// -v says each stage's report.
	out, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "commit", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Contains(t, out, "jq-1.8.1 · tidying edits not yet committed\n")
	require.NotContains(t, out, "✓ edited")
	require.NotContains(t, out, "checking commit ")
	require.Empty(t, g.PRs)
	_, _, err = cmdtest.Dockhand(t, "ship", "jq", "-b", "jq-1.8.1")
	require.EqualError(t, err, "choose one target source: port names, --mine, --passing, or a branch")
}

// ship <port> finds the port's branch as every authoring command does,
// through engine.Select: a script is told the branches where several change
// it, and a terminal asks which (the architecture review of 5897a82, R3-3:
// ship had its own refusal, "Use dockhand ship -b <branch>").
func TestShipChoosesAPortsBranchAsSelectDoes(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	_, errs, err = cmdtest.Dockhand(t, "revbump", "jq", "--subject", "rebuild", "--new=jq-rebuild")
	require.NoError(t, err, errs)
	_, _, err = cmdtest.Dockhand(t, "ship", "jq", "--to", "commit")
	require.EqualError(t, err, "jq is changed in more than one open branch: jq-1.8.1, jq-rebuild (revision only); name one with -b")
	_, errs, err = shipAnswering(t, "1\nn\n", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "? which one, or n for a new branch? [1-2/n] ")
}

// --mine ships the person's ports outdated --mine finds newer releases of,
// at the release it found, and says what it leaves; --passing, the
// branches whose checks passed and aren't submitted.
func TestShipMineAndPassing(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	reader := cmdtest.WithOutdated(t)
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	cmdtest.AsMaintainer(t, w)

	_, _, err := cmdtest.Dockhand(t, "ship", "--mine", "jq")
	require.EqualError(t, err, "choose one target source: port names, --mine, --passing, or a branch")
	out, errs, err := cmdtest.Dockhand(t, "ship", "--mine", "--to", "check")
	require.NoError(t, err, errs)
	require.Equal(t, []string{"@ada"}, reader.Asked[0].Maintainers)
	require.Contains(t, out, "  lost: left as it is: no forge could be found for it\n")
	require.Contains(t, out, "  jq  a version update from master, to check\n")
	require.Contains(t, out, "jq 1.7.1 → 1.8.1")
	require.Empty(t, g.PRs)

	out, errs, err = cmdtest.Dockhand(t, "ship", "--passing", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq-1.8.1  its branch, where it stands, to commit\n")
}

// twoPortWorld is a world of jq and yq, each with a newer release, built
// by one script that passes both.
func twoPortWorld(t *testing.T) *forgetest.GitHub {
	t.Helper()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Upstream, "textproc/yq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/yq/Portfile"), []byte("name yq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.Upstream, "add", ".")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "yq: 1.7.1")
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, "bin", "build-ports"), []byte(`#!/bin/sh
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}, {"id": "yq", "outcome": "passed"}]}
JSON
`), 0o755))
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	return g
}

// A batch at the default level is a row a change, a cell a stage, then
// what needs you (part 5, item 3).
func TestShipBatchIsARowAChange(t *testing.T) {
	t.Parallel()
	g := twoPortWorld(t)
	var out, errs bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"ship", "jq", "yq", "--yes"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.Equal(t, 3, term.ExitCode(err), out.String())
	require.Contains(t, out.String(), "  jq  ✓ ✓ ✓ #34901\n  yq  ✓ ✓ ✓ !\nNeeds you\n  yq  yq-1.8.1 passed its check and waits for your look")
	require.NotContains(t, out.String(), "checking commit ")
	require.NotContains(t, errs.String(), "yq: ", "said under Needs you")
	require.Len(t, g.PRs, 1)
}

// A batch queues every change's check before it sees any through, and
// submits each as its check passes (decision 4).
func TestShipQueuesABatchsChecksFirst(t *testing.T) {
	t.Parallel()
	g := twoPortWorld(t)

	// The bumper stands in jq's release for any port, so yq's commit is
	// held by the commit rules: one change held, and the rest goes on.
	var both bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"ship", "jq", "yq", "--yes", "-v"}, term.Streams{In: strings.NewReader(""), Out: &both, Err: &both})
	out := both.String()
	require.Equal(t, 3, term.ExitCode(err), out)
	require.EqualError(t, err, "yq-1.8.1 stopped; the rest went on")
	jq, yq, ran := strings.Index(out, "jq-1.8.1 · checking commit "), strings.Index(out, "yq-1.8.1 · checking commit "), strings.Index(out, "check-1 runs here")
	require.True(t, jq >= 0 && yq > jq && ran > yq, "both queued before either ran:\n%s", out)
	require.Contains(t, out, "yq-1.8.1: yq-1.8.1 passed its check and waits for your look")
	require.Len(t, g.PRs, 1)
}

// ship takes each stage's flags: --tests for the check, and --type and
// --draft for the pull request, which bump lacked (the second look's §6).
// A draft goes up whatever its check found.
func TestShipTakesEachStagesFlags(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "failed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, _, err := cmdtest.Dockhand(t, "ship", "jq", "--tests", "sometimes")
	require.EqualError(t, err, `--tests "sometimes" is not declared, required, or skip`)
	out, _, err := cmdtest.Dockhand(t, "ship", "jq", "--yes", "--draft", "--type", "bugfix", "--tests", "skip", "-v")
	require.Equal(t, 2, term.ExitCode(err), out)
	require.Contains(t, out, "then submitting it as a draft")
	require.Contains(t, out, "tests skip")
	require.Len(t, g.PRs, 1)
	require.Equal(t, []bool{true}, g.Drafts)
	require.Contains(t, g.PRs[34901].Body, "- [x] bugfix")
}

// ship decides as serve's preparation does wherever nobody looks, --yes
// included: a plan of dockhand's own edits goes on, one with the person's
// is held for a look, and another open pull request holds the update
// before its edit (Codex's review of 832f059e, high 1; serve's half is
// engine's TestServeHoldsAnUpdateAnotherPullRequestIsOpenFor and
// TestShipHoldsWhatNobodyLooksOver).
func TestShipWithYesHoldsWhatServeWould(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)

	// Another open pull request: held before the edit, no branch started.
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	_, _, err := cmdtest.Dockhand(t, "ship", "jq", "--yes")
	require.ErrorContains(t, err, "jq waits for your look, so nothing was changed: #34777 is open for the same port: jq: update to 1.8.0\nOnce it's fine: dockhand ship jq, on a terminal")
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
	g.Others = nil

	// A hand edit: --yes skips ship's question, and approves no plan; the
	// hold names tidy, as serve's does, not tidy's own --yes, which isn't
	// ship's (Codex's review, checked against a268b6e5).
	_, _, err = cmdtest.Dockhand(t, "start", "notes")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "notes")
	_, _, err = cmdtest.Dockhand(t, "edit", "jq", "-b", "notes", "--no-open")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# by hand\n"), 0o644))
	_, _, err = cmdtest.Dockhand(t, "ship", "-b", "notes", "--yes", "--to", "commit")
	require.ErrorContains(t, err, "review the commit plan before committing: run dockhand tidy --branch notes; nothing was checked or submitted")
	require.NotContains(t, err.Error(), "--yes")
	require.Contains(t, testsupport.Git(t, dir, "status", "--porcelain"), "textproc/jq/Portfile", "nothing was committed")
}

// A pass stands for a check asked of the same files only where it built
// as the check asks: ship --tests required after a pass with declared
// tests checks again (Codex's review of 832f059e, medium 4).
func TestShipChecksAgainWhereAPassAskedLess(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, _, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--yes")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "passed for this commit already; nothing to check", "the same ask stands")
	out, _, err = cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--yes", "--tests", "required", "-v")
	require.NoError(t, err)
	require.Contains(t, out, "jq-1.8.1 · checking commit ", "required tests weren't what passed")
	require.NotContains(t, out, "nothing to check")
}

// ship's JSON says each change that stopped, with why, beside those it
// carried on, where only the error said it (the rc1 full stage, S2).
func TestShipsJSONSaysWhatStopped(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Upstream, "textproc/yq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/yq/Portfile"), []byte("name yq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.Upstream, "add", ".")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "yq: 1.7.1")
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	result, err := cmdtest.JSONOf(t, "ship", "jq", "yq", "--yes", "--to", "commit")
	require.Equal(t, 3, term.ExitCode(err), "jq stopped, and yq went on")
	require.Len(t, result.Result["skipped"], 1)
	require.Equal(t, "jq", cmdtest.Dig(t, result.Result, "skipped", 0, "port"))
	require.Contains(t, cmdtest.Dig(t, result.Result, "skipped", 0, "reason"), "#34777 is open for the same port")
	require.Len(t, result.Result["changes"], 1, "yq's change is said as before")
}

// A ship nobody looks over, a batch's or one with --yes, starts no branch
// for an edit dockhand can't make, as serve's doesn't: c360b35f moved the
// batch onto author, which kept one for the edit by hand, and ship --mine
// left an empty branch for each such port (Codex's review, checked
// against a268b6e5).
func TestShipStartsNoBranchForAnEditItCantMakeUnattended(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(*engine.Engine) engine.Preparer { return cmdtest.Refuser{} }
	_, _, err := cmdtest.Dockhand(t, "ship", "jq", "--yes", "--to", "commit")
	require.ErrorContains(t, err, "Could not update jq automatically: its pre-fetch hook runs exec\nNo branch was started.")
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
}
