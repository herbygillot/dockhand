package command

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

func TestJSONEnvelopes(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)

	planned, err := cmdtest.JSONOf(t, "check", "--plan")
	require.NoError(t, err)
	require.Equal(t, "check", planned.Command)
	require.Nil(t, planned.Error)
	require.NotNil(t, planned.GitHubRequests, "every envelope says what it spent of GitHub's API")
	require.Equal(t, "jq", cmdtest.Dig(t, planned.Result, "plan", "targets", 0, "name"))
	require.Equal(t, "substantive", cmdtest.Dig(t, planned.Result, "plan", "targets", 0, "kind"))
	require.Equal(t, "command", cmdtest.Dig(t, planned.Result, "plan", "builds", 0, "environment", "provider"))
	require.Equal(t, []any{"jq"}, cmdtest.Dig(t, planned.Result, "plan", "builds", 0, "order"), "each environment's own order")
	require.Equal(t, "a new snapshot", cmdtest.Dig(t, planned.Result, "revision", "description"), "a plan records and numbers nothing")
	require.Nil(t, planned.Result["run"])
	require.Equal(t, []any{}, cmdtest.Dig(t, planned.Result, "plan", "only"))
	require.Equal(t, []any{}, cmdtest.Dig(t, planned.Result, "plan", "omitted"))
	require.Equal(t, false, cmdtest.Dig(t, planned.Result, "plan", "fresh"))
	narrowed, err := cmdtest.JSONOf(t, "check", "--plan", "--only", "jq", "--fresh")
	require.NoError(t, err)
	require.Equal(t, []any{"jq"}, cmdtest.Dig(t, narrowed.Result, "plan", "only"), "what was asked, as the text says it")
	require.Equal(t, true, cmdtest.Dig(t, narrowed.Result, "plan", "fresh"))

	queued, err := cmdtest.JSONOf(t, "check", "-d")
	require.NoError(t, err)
	require.Equal(t, "queued", cmdtest.Dig(t, queued.Result, "run", "state"))
	// status lists the queue, as queue did.
	listed, err := cmdtest.JSONOf(t, "status", "--all")
	require.NoError(t, err)
	require.Equal(t, "check-1", cmdtest.Dig(t, listed.Result, "queue", 0, "name"))
	require.Equal(t, "jq-update", cmdtest.Dig(t, listed.Result, "queue", 0, "branch"))
	require.Equal(t, "serve: not running · queue: 1 run", listed.Result["serve"])
	// While the branch's first check waits, its latest check is null, and
	// the check is among its active ones.
	waiting, err := cmdtest.JSONOf(t, "status")
	require.NoError(t, err)
	require.Nil(t, cmdtest.Dig(t, waiting.Result, "branches", 0, "latest_check"))
	require.Equal(t, "check-1", cmdtest.Dig(t, waiting.Result, "branches", 0, "active_checks", 0, "name"))
	require.Equal(t, "checking", cmdtest.Dig(t, waiting.Result, "branches", 0, "check_standing"), "the verdict, not the runs to judge it from (R3-34)")
	require.Equal(t, map[string]any{"running": false, "queue": float64(1), "stopped": float64(0)}, listed.Result["serve_state"],
		"serve's state as fields a script reads (the hugo exercise's certigo run, finding 6)")

	waited, err := cmdtest.JSONOf(t, "wait", "check-1")
	require.NoError(t, err)
	require.Equal(t, "passed", cmdtest.Dig(t, waited.Result, "run", "state"))
	require.Equal(t, "passed", cmdtest.Dig(t, waited.Result, "targets", 0, "results", 0, "outcome"))
	require.Equal(t, true, cmdtest.Dig(t, waited.Result, "targets", 0, "passed"))

	status, err := cmdtest.JSONOf(t, "status")
	require.NoError(t, err)
	require.Equal(t, "status", status.Command)
	branch := cmdtest.Dig(t, status.Result, "branches", 0)
	require.Equal(t, "jq-update", cmdtest.Dig(t, branch, "name"))
	require.Equal(t, "dockhand/jq-update", cmdtest.Dig(t, branch, "git_branch"))
	require.Equal(t, []any{"textproc/jq"}, cmdtest.Dig(t, branch, "directories"))
	require.Equal(t, true, cmdtest.Dig(t, branch, "latest_check", "current"))
	require.Equal(t, "check-1", cmdtest.Dig(t, branch, "latest_check", "run", "name"))
	require.Equal(t, "passed", cmdtest.Dig(t, branch, "check_standing"))
	require.Nil(t, cmdtest.Dig(t, branch, "pull_request"))
	everything, err := cmdtest.JSONOf(t, "status", "--all")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"running": false, "queue": float64(0), "stopped": float64(0)}, everything.Result["serve_state"])
	require.Equal(t, []any{}, everything.Result["queue"], "an empty queue is said")

	attention, err := cmdtest.JSONOf(t, "status", "--attention")
	require.Error(t, err)
	require.Equal(t, 3, attention.ExitCode)
	require.Equal(t, "ready", cmdtest.Dig(t, attention.Result, "attention", 0, "kind"))
	require.Equal(t, "dockhand tidy --branch jq-update", cmdtest.Dig(t, attention.Result, "attention", 0, "next"))

	diff, err := cmdtest.JSONOf(t, "diff")
	require.NoError(t, err)
	require.Equal(t, "changed", cmdtest.Dig(t, diff.Result, "ports", 0, "change"))
	require.Contains(t, cmdtest.Dig(t, diff.Result, "patch"), "+version 1.8.1")

	path, err := cmdtest.JSONOf(t, "path")
	require.NoError(t, err)
	require.Contains(t, path.Result["path"], "macports-branches/jq-update")

	cmdtest.WithScript(t, w, "failed")
	failed, err := cmdtest.JSONOf(t, "check")
	require.Error(t, err)
	require.Equal(t, 2, failed.ExitCode)
	require.Contains(t, *failed.Error, "check-2 failed for snapshot 1")
	require.Equal(t, "failed", cmdtest.Dig(t, failed.Result, "targets", 0, "results", 0, "outcome"))
	require.Equal(t, "install", cmdtest.Dig(t, failed.Result, "targets", 0, "results", 0, "phase"))

	refused, err := cmdtest.JSONOf(t, "status", "--watch")
	require.Error(t, err)
	require.Equal(t, 1, refused.ExitCode)
	require.Equal(t, "--watch shows all current status and cannot be combined with a branch selector, --attention, --all, or --json", *refused.Error)
	require.Nil(t, refused.Result)

	unknown, err := cmdtest.JSONOf(t, "status", "--no-such-flag")
	require.Error(t, err)
	require.Contains(t, *unknown.Error, "unknown flag: --no-such-flag")
}

func TestJSONForTheWholeLoop(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	cmdtest.WithScript(t, w, "passed")

	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", cmdtest.Dig(t, started.Result, "branch", "git_branch"))
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))

	planned, err := cmdtest.JSONOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, false, planned.Result["applied"])
	require.Contains(t, planned.Result["diff"], "+version 1.8.1")
	updated, err := cmdtest.JSONOf(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, "1.7.1", cmdtest.Dig(t, updated.Result, "before", "version"))
	require.Equal(t, "1.8.1", cmdtest.Dig(t, updated.Result, "after", "version"))
	require.Equal(t, []any{"textproc/jq/Portfile"}, updated.Result["files"])
	require.Equal(t, "not-compared", cmdtest.Dig(t, updated.Result, "upstream", "coverage", 0, "policy"), "a port with no archives to compare says so")

	_, err = cmdtest.JSONOf(t, "check")
	require.NoError(t, err)
	logs, err := cmdtest.JSONOf(t, "logs", "check-1")
	require.NoError(t, err)
	require.Equal(t, "passed", cmdtest.Dig(t, logs.Result, "executions", 0, "results", 0, "outcome"))

	tidyPlan, err := cmdtest.JSONOf(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Equal(t, true, tidyPlan.Result["unambiguous"])
	require.Equal(t, "jq: update to 1.8.1", cmdtest.Dig(t, tidyPlan.Result, "commits", 0, "subject"))
	require.Nil(t, tidyPlan.Result["applied"])
	tidied, err := cmdtest.JSONOf(t, "tidy")
	require.NoError(t, err)
	require.Equal(t, "tidy-1", cmdtest.Dig(t, tidied.Result, "applied", "checkpoint"))

	refused, err := cmdtest.JSONOf(t, "submit")
	require.Error(t, err, "a --json command line never asks, so submit needs --yes")
	require.Contains(t, *refused.Error, "--yes submits exactly what is shown")
	require.Equal(t, "jq: update to 1.8.1", refused.Result["title"], "the preview is the result")
	require.Nil(t, refused.Result["pull_request"])
	submitted, err := cmdtest.JSONOf(t, "submit", "--yes")
	require.NoError(t, err)
	require.Equal(t, float64(34901), cmdtest.Dig(t, submitted.Result, "pull_request", "number"))
	require.Equal(t, true, cmdtest.Dig(t, submitted.Result, "pull_request", "created"))
	require.Len(t, g.PRs, 1)
	afterSubmit, err := cmdtest.JSONOf(t, "status")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/macports/macports-ports/pull/34901", cmdtest.Dig(t, afterSubmit.Result, "branches", 0, "pull_request", "url"))

	explained, err := cmdtest.JSONOf(t, "explain", "merge")
	require.NoError(t, err)
	require.Equal(t, "https://guide.macports.org/#project.github", cmdtest.Dig(t, explained.Result, "sources", 0, "url"))

	// clean reads the pull request itself, with no status --refresh
	// before it (field testing, 2026-10-02).
	g.PRs[34901].State = forge.PullRequestMerged
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	preview, err := cmdtest.JSONOf(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err), "a preview without --yes removes nothing, and refuses")
	require.Equal(t, false, preview.Result["applied"])
	require.Equal(t, false, cmdtest.Dig(t, preview.Result, "branches", 0, "steps", 0, "removed"))
	require.Equal(t, "worktree", cmdtest.Dig(t, preview.Result, "branches", 0, "steps", 0, "kind"), "what each step removes")
	cleaned, err := cmdtest.JSONOf(t, "clean", "--yes")
	require.NoError(t, err)
	require.Equal(t, true, cmdtest.Dig(t, cleaned.Result, "branches", 0, "steps", 0, "removed"))

	notJSON, err := cmdtest.JSONOf(t, "serve", "--drain")
	require.Error(t, err)
	require.Equal(t, "--json isn't available for dockhand serve yet; its output is text only", *notJSON.Error)
}

// update --json carries what comparing the upstream archives found, as
// update prints it. (The architecture review of 2026-09-27, finding 6.)
func TestUpdateJSONCarriesTheUpstreamComparison(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer {
		return cmdtest.UnfetchedPrevious{Bumper: cmdtest.Bumper{Repo: e.Repo}}
	}
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))

	updated, err := cmdtest.JSONOf(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"changes": []any{}, "problem": "the current version's archives could not be fetched: HTTP 404", "held": true}, updated.Result["upstream"],
		"archives it couldn't compare hold the update, as a finding would (D4)")
}

// A plan says what comparing the upstream archives found, as the update it
// previews does: what a reviewer would ask about is part of the look.
func TestAnUpdatesPlanSaysWhatUpstreamChanged(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer {
		return cmdtest.UnfetchedPrevious{Bumper: cmdtest.Bumper{Repo: e.Repo}}
	}
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Plan, nothing changed:")
	require.Contains(t, out, "! held: upstream archives not compared: the current version's archives could not be fetched: HTTP 404")
}

// ship's submission reports its result wherever it stops, with its check
// inside when it ran one: a failed check leaves the pull request out, and
// a refusal before checking reports what blocks it.
func TestShipReportsOneShape(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	next := cmdtest.WithOutcomeScript(t, w, "")
	committed := func(name string) {
		t.Helper()
		started, err := cmdtest.JSONOf(t, "start", name)
		require.NoError(t, err)
		cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
		_, _, err = cmdtest.Dockhand(t, "update", "jq")
		require.NoError(t, err)
		_, _, err = cmdtest.Dockhand(t, "tidy")
		require.NoError(t, err)
	}

	change := func(result map[string]any) map[string]any {
		t.Helper()
		require.Len(t, result["changes"], 1)
		return cmdtest.Dig(t, result, "changes", 0).(map[string]any)
	}
	committed("jq-failing")
	next("failed")
	failed, err := cmdtest.JSONOf(t, "ship", "--yes")
	require.Equal(t, 2, term.ExitCode(err))
	require.Equal(t, "jq: update to 1.8.1", cmdtest.Dig(t, change(failed.Result), "submit", "title"))
	require.Equal(t, "failed", cmdtest.Dig(t, change(failed.Result), "check", "run", "state"))
	require.Nil(t, cmdtest.Dig(t, change(failed.Result), "submit", "pull_request"))
	refused, err := cmdtest.JSONOf(t, "ship", "--yes")
	require.Error(t, err, "the failed check stands for the same commit")
	require.Equal(t, "jq: update to 1.8.1", cmdtest.Dig(t, change(refused.Result), "submit", "title"))
	require.NotEmpty(t, cmdtest.Dig(t, change(refused.Result), "submit", "blocking"))
	require.NotContains(t, change(refused.Result), "check", "no check ran")
	require.Empty(t, g.PRs)

	committed("jq-passing")
	next("passed")
	passed, err := cmdtest.JSONOf(t, "ship", "--yes")
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1", cmdtest.Dig(t, change(passed.Result), "submit", "title"))
	require.Equal(t, "passed", cmdtest.Dig(t, change(passed.Result), "check", "run", "state"))
	require.Equal(t, true, cmdtest.Dig(t, change(passed.Result), "submit", "pull_request", "created"))
}

// Under the JSON's upstream key, as under the text's Upstream heading, a
// change is worded without "upstream", which the key already says (the
// hugo exercise's certigo run, finding 3).
func TestUpstreamJSONWordsChangesUnderItsKey(t *testing.T) {
	t.Parallel()
	viewed := view.UpstreamView(model.UpstreamComparison{Changes: []model.UpstreamChange{
		{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod adds golang.org/x/net v0.44.0", Hold: true},
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true},
	}})
	require.Equal(t, "go.mod adds golang.org/x/net v0.44.0", viewed.Changes[0].Message)
	require.Equal(t, "LICENSE changed; the Portfile's license line may need to follow", viewed.Changes[1].Message)
	require.True(t, viewed.Held)
}

// Serve's state is fields, and its line is written from them: whether it
// runs, as which process, whether it opens pull requests, and the queue
// with what has stopped (the hugo exercise's certigo run, finding 6).
func TestServesStateIsFieldsAndItsLine(t *testing.T) {
	t.Parallel()
	require.Equal(t, "serve: not running · queue: empty", view.ServeWords(engine.ServeState{}))
	running := engine.ServeState{Running: true, PID: 34857, OpensPullRequests: true, Queue: 2, Stopped: 1}
	require.Equal(t, "serve: running (pid 34857) · opens PRs for passing updates · queue: 2 runs, 1 stopped", view.ServeWords(running))
	data, err := json.Marshal(view.ServeView(running))
	require.NoError(t, err)
	require.JSONEq(t, `{"running": true, "pid": 34857, "opens_pull_requests": true, "queue": 2, "stopped": 1}`, string(data))
	running.OpensPullRequests, running.Stopped = false, 0
	require.Equal(t, "serve: running (pid 34857) · queue: 2 runs", view.ServeWords(running))
	data, err = json.Marshal(view.ServeView(running))
	require.NoError(t, err)
	require.JSONEq(t, `{"running": true, "pid": 34857, "queue": 2, "stopped": 0}`, string(data))
}

// logs --json says what each environment was as its run began, as
// evidence compares it later, and nothing where its provider can't say.
func TestLogsSayWhatTheEnvironmentWas(t *testing.T) {
	t.Parallel()
	logs := engine.RunLogs{Executions: []engine.ExecutionLogs{
		{Execution: model.GuestExecution{ID: "tart_1", Attempt: 1, State: model.ExecutionFinished, Identity: "source sha256:a; setup 3"}},
		{Execution: model.GuestExecution{ID: "command_1", Attempt: 1, State: model.ExecutionFinished}},
	}}
	data, err := json.Marshal(view.LogsView(logs))
	require.NoError(t, err)
	var viewed map[string]any
	require.NoError(t, json.Unmarshal(data, &viewed))
	require.Equal(t, "source sha256:a; setup 3", cmdtest.Dig(t, viewed, "executions", 0, "identity"))
	require.NotContains(t, cmdtest.Dig(t, viewed, "executions", 1), "identity")
}

// logs says, of a Git-fetched target's build, the commit it fetched and
// the source it was to fetch, or that its provider didn't say (batch 20).
func TestLogsSayWhatAGitFetchedBuildFetched(t *testing.T) {
	t.Parallel()
	commit := model.ObjectID("1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b")
	source := model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	logs := engine.RunLogs{Run: model.Run{Number: 3, State: model.RunPassed}, Executions: []engine.ExecutionLogs{
		{Execution: model.GuestExecution{ID: "tart_1", Attempt: 1, State: model.ExecutionFinished},
			Results: []model.TargetResult{{Target: "libharbor", Outcome: model.OutcomePassed}, {Target: "harbor-cli", Outcome: model.OutcomePassed}},
			Git:     map[model.TargetID]engine.GitFetch{"libharbor": {Expected: source, Fetched: commit}}},
		{Execution: model.GuestExecution{ID: "github_1", Attempt: 1, State: model.ExecutionFinished},
			Results: []model.TargetResult{{Target: "libharbor", Outcome: model.OutcomePassed}},
			Git:     map[model.TargetID]engine.GitFetch{"libharbor": {Expected: source}}},
	}}
	var out bytes.Buffer
	require.NoError(t, view.WriteLogs(&out, cmdtest.TestPlaces(t), logs, false))
	require.Contains(t, out.String(), "    libharbor passed\n      fetched 1a2b3c4, the commit git.branch v4 named when the check was planned\n    harbor-cli passed\n")
	require.Contains(t, out.String(), "    libharbor passed\n      which commit of git.branch v4 it fetched isn't known: its provider didn't say; 1a2b3c4 was expected\n")

	data, err := json.Marshal(view.LogsView(logs))
	require.NoError(t, err)
	var viewed map[string]any
	require.NoError(t, json.Unmarshal(data, &viewed))
	require.Equal(t, string(commit), cmdtest.Dig(t, viewed, "executions", 0, "results", 0, "fetched"))
	require.Equal(t, "v4", cmdtest.Dig(t, viewed, "executions", 0, "results", 0, "git", "branch"))
	require.NotContains(t, cmdtest.Dig(t, viewed, "executions", 0, "results", 1), "git", "fetched otherwise")
	require.NotContains(t, cmdtest.Dig(t, viewed, "executions", 1, "results", 0), "fetched", "its provider didn't say")
}

// A plan's JSON carries what the person asked and what --only left out,
// as its text does: the changed targets submission still requires.
func TestAPlansJSONSaysWhatWasAskedAndLeftOut(t *testing.T) {
	t.Parallel()
	arm := model.Environment{Provider: "command"}
	plan := model.Plan{ID: "p1", Environments: []model.Environment{arm}, Tests: model.TestsDeclared, Only: []string{"jq"}, Also: []string{"oniguruma"},
		Targets: []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed},
			{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Directory: "textproc/oniguruma", Kind: model.Unchanged, Role: model.Also}},
		Omitted: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}},
		Builds:  []model.EnvironmentPlan{{Environment: arm, Order: []model.TargetID{"jq", "oniguruma"}}}}
	viewed := view.PlanView(plan)
	require.Equal(t, []string{"jq"}, viewed.Only)
	require.Equal(t, []string{"oniguruma"}, viewed.Also)
	require.Len(t, viewed.Omitted, 1)
	require.Equal(t, "libharbor", viewed.Omitted[0].Name)
}
