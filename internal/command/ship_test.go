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

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
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
	require.Contains(t, out, "  jq-1.8.1  existing branch; update if needed to the newest release, to check\n", "resumed, not started again")
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
	require.EqualError(t, err, "choose one target source: port names, --mine, --passing, or a branch")
}

// ship <port> finds the port's branch as every authoring command does,
// through engine.Select: a script is told the branches where several change
// it, and a terminal asks which (the architecture review of 5897a82, R3-3:
// ship had its own refusal, "Use dockhand ship -b <branch>").
func TestShipChoosesAPortsBranchAsSelectDoes(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	_, errs, err := dockhand(t, "ship", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	_, errs, err = dockhand(t, "revbump", "jq", "--subject", "rebuild", "--new=jq-rebuild")
	require.NoError(t, err, errs)
	_, _, err = dockhand(t, "ship", "jq", "--to", "commit")
	require.EqualError(t, err, "jq is changed in more than one open branch: jq-1.8.1, jq-rebuild (revision only); name one with -b")
	_, errs, err = shipAnswering(t, "1\nn\n", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "? which one, or n for a new branch? [1-2/n] ")
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
	require.EqualError(t, err, "choose one target source: port names, --mine, --passing, or a branch")
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

// ship takes each stage's flags: --tests for the check, and --type and
// --draft for the pull request, which bump lacked (the second look's §6).
// A draft goes up whatever its check found.
func TestShipTakesEachStagesFlags(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "failed")
	g := withGitHub(t, w)
	g.Others = nil

	_, _, err := dockhand(t, "ship", "jq", "--tests", "sometimes")
	require.EqualError(t, err, `--tests "sometimes" is not declared, required, or skip`)
	out, _, err := dockhand(t, "ship", "jq", "--yes", "--draft", "--type", "bugfix", "--tests", "skip")
	require.Equal(t, 2, ExitCode(err), out)
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
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)

	// Another open pull request: held before the edit, no branch started.
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	_, _, err := dockhand(t, "ship", "jq", "--yes")
	require.ErrorContains(t, err, "jq waits for your look, so nothing was changed: #34777 is open for the same port: jq: update to 1.8.0\nOnce it's fine: dockhand ship jq, on a terminal")
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")
	g.Others = nil

	// A hand edit: --yes skips ship's question, and approves no plan; the
	// hold names tidy, as serve's does, not tidy's own --yes, which isn't
	// ship's (Codex's review, checked against a268b6e5).
	_, _, err = dockhand(t, "start", "notes")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "notes")
	_, _, err = dockhand(t, "edit", "jq", "-b", "notes", "--no-open")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# by hand\n"), 0o644))
	_, _, err = dockhand(t, "ship", "-b", "notes", "--yes", "--to", "commit")
	require.ErrorContains(t, err, "review the commit plan before committing: run dockhand tidy --branch notes; nothing was checked or submitted")
	require.NotContains(t, err.Error(), "--yes")
	require.Contains(t, testsupport.Git(t, dir, "status", "--porcelain"), "textproc/jq/Portfile", "nothing was committed")
}

// A pass stands for a check asked of the same files only where it built
// as the check asks: ship --tests required after a pass with declared
// tests checks again (Codex's review of 832f059e, medium 4).
func TestShipChecksAgainWhereAPassAskedLess(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	_, _, err := dockhand(t, "ship", "jq", "--to", "check", "--yes")
	require.NoError(t, err)
	out, _, err := dockhand(t, "ship", "jq", "--to", "check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "passed for this commit already; nothing to check", "the same ask stands")
	out, _, err = dockhand(t, "ship", "jq", "--to", "check", "--yes", "--tests", "required")
	require.NoError(t, err)
	require.Contains(t, out, "jq-1.8.1 · checking commit ", "required tests weren't what passed")
	require.NotContains(t, out, "nothing to check")
}

// ship's JSON says each change that stopped, with why, beside those it
// carried on, where only the error said it (the rc1 full stage, S2).
func TestShipsJSONSaysWhatStopped(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	require.NoError(t, os.MkdirAll(filepath.Join(w.upstream, "textproc/yq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/yq/Portfile"), []byte("name yq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.upstream, "add", ".")
	testsupport.Git(t, w.upstream, "commit", "-q", "-m", "yq: 1.7.1")
	g.Others = []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}
	result, err := jsonOf(t, "ship", "jq", "yq", "--yes", "--to", "commit")
	require.Equal(t, 3, ExitCode(err), "jq stopped, and yq went on")
	require.Len(t, result.Result["skipped"], 1)
	require.Equal(t, "jq", dig(t, result.Result, "skipped", 0, "port"))
	require.Contains(t, dig(t, result.Result, "skipped", 0, "reason"), "#34777 is open for the same port")
	require.Len(t, result.Result["changes"], 1, "yq's change is said as before")
}

// A ship nobody looks over, a batch's or one with --yes, starts no branch
// for an edit dockhand can't make, as serve's doesn't: c360b35f moved the
// batch onto author, which kept one for the edit by hand, and ship --mine
// left an empty branch for each such port (Codex's review, checked
// against a268b6e5).
func TestShipStartsNoBranchForAnEditItCantMakeUnattended(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(*engine.Engine) engine.Preparer { return refuser{} }
	t.Cleanup(func() { testPreparer = nil })
	_, _, err := dockhand(t, "ship", "jq", "--yes", "--to", "commit")
	require.ErrorContains(t, err, "Could not update jq automatically: its pre-fetch hook runs exec\nNo branch was started.")
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")
}
