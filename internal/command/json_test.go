package command

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
)

// decoded is a --json envelope, read back loosely.
type decoded struct {
	Version  int            `json:"version"`
	Command  string         `json:"command"`
	ExitCode int            `json:"exit_code"`
	Error    *string        `json:"error"`
	Result   map[string]any `json:"result"`
}

// jsonOf runs a command line with --json and decodes all of standard
// output as its one envelope.
func jsonOf(t *testing.T, args ...string) (decoded, error) {
	t.Helper()
	out, _, err := dockhand(t, append(args, "--json")...)
	var envelope decoded
	require.NoError(t, json.Unmarshal([]byte(out), &envelope), "standard output is only the envelope: %s", out)
	require.Equal(t, 1, envelope.Version)
	if err != nil {
		require.Equal(t, ExitCode(err), envelope.ExitCode)
		require.Empty(t, err.Error(), "the envelope carries the error, not standard error")
	}
	return envelope, err
}

// dig walks a decoded result by keys and indexes.
func dig(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := value.(map[string]any)
			require.True(t, ok, "%v is not an object at %q", value, key)
			value = object[key]
		case int:
			list, ok := value.([]any)
			require.True(t, ok && key < len(list), "%v has no item %d", value, key)
			value = list[key]
		}
	}
	return value
}

func TestJSONEnvelopes(t *testing.T) {
	w := checkedBranch(t)

	planned, err := jsonOf(t, "check", "--plan")
	require.NoError(t, err)
	require.Equal(t, "check", planned.Command)
	require.Nil(t, planned.Error)
	require.Equal(t, "jq", dig(t, planned.Result, "plan", "targets", 0, "name"))
	require.Equal(t, "substantive", dig(t, planned.Result, "plan", "targets", 0, "kind"))
	require.Equal(t, "command", dig(t, planned.Result, "plan", "builds", 0, "environment", "provider"))
	require.Equal(t, []any{"jq"}, dig(t, planned.Result, "plan", "builds", 0, "order"), "each environment's own order")
	require.Equal(t, "a new snapshot", dig(t, planned.Result, "revision", "description"), "a plan records and numbers nothing")
	require.Nil(t, planned.Result["run"])
	require.Equal(t, []any{}, dig(t, planned.Result, "plan", "only"))
	require.Equal(t, []any{}, dig(t, planned.Result, "plan", "omitted"))
	require.Equal(t, false, dig(t, planned.Result, "plan", "fresh"))
	narrowed, err := jsonOf(t, "check", "--plan", "--only", "jq", "--fresh")
	require.NoError(t, err)
	require.Equal(t, []any{"jq"}, dig(t, narrowed.Result, "plan", "only"), "what was asked, as the text says it")
	require.Equal(t, true, dig(t, narrowed.Result, "plan", "fresh"))

	queued, err := jsonOf(t, "check", "-d")
	require.NoError(t, err)
	require.Equal(t, "queued", dig(t, queued.Result, "run", "state"))
	listed, err := jsonOf(t, "queue")
	require.NoError(t, err)
	require.Equal(t, "check-1", dig(t, listed.Result, "runs", 0, "name"))
	require.Equal(t, "jq-update", dig(t, listed.Result, "runs", 0, "branch"))
	require.Equal(t, "serve: not running · queue: 1 run", listed.Result["serve"])
	require.Equal(t, map[string]any{"running": false, "queue": float64(1), "stopped": float64(0)}, listed.Result["serve_state"],
		"serve's state as fields a script reads (the hugo exercise's certigo run, finding 6)")

	waited, err := jsonOf(t, "wait", "check-1")
	require.NoError(t, err)
	require.Equal(t, "passed", dig(t, waited.Result, "run", "state"))
	require.Equal(t, "passed", dig(t, waited.Result, "targets", 0, "results", 0, "outcome"))
	require.Equal(t, true, dig(t, waited.Result, "targets", 0, "passed"))

	status, err := jsonOf(t, "status")
	require.NoError(t, err)
	require.Equal(t, "status", status.Command)
	branch := dig(t, status.Result, "branches", 0)
	require.Equal(t, "jq-update", dig(t, branch, "name"))
	require.Equal(t, "dockhand/jq-update", dig(t, branch, "git_branch"))
	require.Equal(t, []any{"textproc/jq"}, dig(t, branch, "directories"))
	require.Equal(t, true, dig(t, branch, "latest_check", "current"))
	require.Equal(t, "check-1", dig(t, branch, "latest_check", "run", "name"))
	require.Nil(t, dig(t, branch, "pull_request"))
	everything, err := jsonOf(t, "status", "--all")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"running": false, "queue": float64(0), "stopped": float64(0)}, everything.Result["serve_state"])

	attention, err := jsonOf(t, "status", "--attention")
	require.Error(t, err)
	require.Equal(t, 3, attention.ExitCode)
	require.Equal(t, "ready", dig(t, attention.Result, "attention", 0, "kind"))
	require.Equal(t, "dockhand tidy --branch jq-update", dig(t, attention.Result, "attention", 0, "next"))

	diff, err := jsonOf(t, "diff")
	require.NoError(t, err)
	require.Equal(t, "changed", dig(t, diff.Result, "ports", 0, "change"))
	require.Contains(t, dig(t, diff.Result, "patch"), "+version 1.8.1")

	path, err := jsonOf(t, "path")
	require.NoError(t, err)
	require.Contains(t, path.Result["path"], "macports-branches/jq-update")

	withScript(t, w, "failed")
	failed, err := jsonOf(t, "check")
	require.Error(t, err)
	require.Equal(t, 2, failed.ExitCode)
	require.Contains(t, *failed.Error, "check-2 failed for snapshot 1")
	require.Equal(t, "failed", dig(t, failed.Result, "targets", 0, "results", 0, "outcome"))
	require.Equal(t, "install", dig(t, failed.Result, "targets", 0, "results", 0, "phase"))

	refused, err := jsonOf(t, "watch")
	require.Error(t, err)
	require.Equal(t, 1, refused.ExitCode)
	require.Equal(t, "--json isn't available for dockhand watch yet; its output is text only", *refused.Error)
	require.Nil(t, refused.Result)

	unknown, err := jsonOf(t, "status", "--no-such-flag")
	require.Error(t, err)
	require.Contains(t, *unknown.Error, "unknown flag: --no-such-flag")
}

func TestJSONForTheWholeLoop(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	withScript(t, w, "passed")

	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", dig(t, started.Result, "branch", "git_branch"))
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))

	planned, err := jsonOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, false, planned.Result["applied"])
	require.Contains(t, planned.Result["diff"], "+version 1.8.1")
	updated, err := jsonOf(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, "1.7.1", dig(t, updated.Result, "before", "version"))
	require.Equal(t, "1.8.1", dig(t, updated.Result, "after", "version"))
	require.Equal(t, []any{"textproc/jq/Portfile"}, updated.Result["files"])
	require.Equal(t, "not-compared", dig(t, updated.Result, "upstream", "coverage", 0, "policy"), "a port with no archives to compare says so")

	_, err = jsonOf(t, "check")
	require.NoError(t, err)
	logs, err := jsonOf(t, "logs", "check-1")
	require.NoError(t, err)
	require.Equal(t, "passed", dig(t, logs.Result, "executions", 0, "results", 0, "outcome"))

	tidyPlan, err := jsonOf(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Equal(t, true, tidyPlan.Result["unambiguous"])
	require.Equal(t, "jq: update to 1.8.1", dig(t, tidyPlan.Result, "commits", 0, "subject"))
	require.Nil(t, tidyPlan.Result["applied"])
	tidied, err := jsonOf(t, "tidy")
	require.NoError(t, err)
	require.Equal(t, "tidy-1", dig(t, tidied.Result, "applied", "checkpoint"))

	refused, err := jsonOf(t, "submit")
	require.Error(t, err, "a --json command line never asks, so submit needs --yes")
	require.Contains(t, *refused.Error, "--yes submits exactly what is shown")
	require.Equal(t, "jq: update to 1.8.1", refused.Result["title"], "the preview is the result")
	require.Nil(t, refused.Result["pull_request"])
	submitted, err := jsonOf(t, "submit", "--yes")
	require.NoError(t, err)
	require.Equal(t, float64(34901), dig(t, submitted.Result, "pull_request", "number"))
	require.Equal(t, true, dig(t, submitted.Result, "pull_request", "created"))
	require.Len(t, g.prs, 1)
	afterSubmit, err := jsonOf(t, "status")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/macports/macports-ports/pull/34901", dig(t, afterSubmit.Result, "branches", 0, "pull_request", "url"))

	explained, err := jsonOf(t, "explain", "merge")
	require.NoError(t, err)
	require.Equal(t, "https://guide.macports.org/#project.github", dig(t, explained.Result, "sources", 0, "url"))

	// clean reads the pull request itself, with no status --refresh
	// before it (field testing, 2026-10-02).
	g.prs[0].State = forge.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	preview, err := jsonOf(t, "clean")
	require.NoError(t, err)
	require.Equal(t, false, preview.Result["applied"])
	require.Equal(t, false, dig(t, preview.Result, "branches", 0, "steps", 0, "removed"))
	cleaned, err := jsonOf(t, "clean", "--yes")
	require.NoError(t, err)
	require.Equal(t, true, dig(t, cleaned.Result, "branches", 0, "steps", 0, "removed"))

	notJSON, err := jsonOf(t, "serve", "--drain")
	require.Error(t, err)
	require.Equal(t, "--json isn't available for dockhand serve yet; its output is text only", *notJSON.Error)
}

// unfetchedPrevious is the bumper, where the current version's archives
// couldn't be fetched to compare with the new one's.
type unfetchedPrevious struct{ bumper }

func (b unfetchedPrevious) Prepare(ctx context.Context, r preparation.Request) (preparation.Result, error) {
	result, err := b.bumper.Prepare(ctx, r)
	result.PreviousProblem = "HTTP 404"
	return result, err
}

// update --json carries what comparing the upstream archives found, as
// update prints it. (The architecture review of 2026-09-27, finding 6.)
func TestUpdateJSONCarriesTheUpstreamComparison(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return unfetchedPrevious{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))

	updated, err := jsonOf(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"changes": []any{}, "problem": "the current version's archives could not be fetched: HTTP 404", "held": true}, updated.Result["upstream"],
		"archives it couldn't compare hold the update, as a finding would (D4)")
}

// A plan says what comparing the upstream archives found, as the update it
// previews does: what a reviewer would ask about is part of the look.
func TestAnUpdatesPlanSaysWhatUpstreamChanged(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return unfetchedPrevious{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	out, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Plan, nothing changed:")
	require.Contains(t, out, "! Upstream archives not compared: the current version's archives could not be fetched: HTTP 404")
}

// submit --check reports the submission's result wherever it stops, with
// its check inside when it ran one: a failed check leaves the pull request
// out, and a refusal before checking reports what blocks it.
func TestSubmitCheckReportsOneShape(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	next := withOutcomeScript(t, w, "")
	committed := func(name string) {
		t.Helper()
		started, err := jsonOf(t, "start", name)
		require.NoError(t, err)
		t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
		_, _, err = dockhand(t, "update", "jq")
		require.NoError(t, err)
		_, _, err = dockhand(t, "tidy")
		require.NoError(t, err)
	}

	committed("jq-failing")
	next("failed")
	failed, err := jsonOf(t, "submit", "--check")
	require.Equal(t, 2, ExitCode(err))
	require.Equal(t, "jq: update to 1.8.1", failed.Result["title"])
	require.Equal(t, "failed", dig(t, failed.Result, "check", "run", "state"))
	require.Nil(t, failed.Result["pull_request"])
	refused, err := jsonOf(t, "submit", "--check")
	require.Error(t, err, "the failed check stands for the same commit")
	require.Equal(t, "jq: update to 1.8.1", refused.Result["title"])
	require.NotEmpty(t, refused.Result["blocking"])
	require.NotContains(t, refused.Result, "check", "no check ran")
	require.Empty(t, g.prs)

	committed("jq-passing")
	next("passed")
	passed, err := jsonOf(t, "submit", "--check")
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1", passed.Result["title"])
	require.Equal(t, "passed", dig(t, passed.Result, "check", "run", "state"))
	require.Equal(t, true, dig(t, passed.Result, "pull_request", "created"))
}

// Under the JSON's upstream key, as under the text's Upstream heading, a
// change is worded without "upstream", which the key already says (the
// hugo exercise's certigo run, finding 3).
func TestUpstreamJSONWordsChangesUnderItsKey(t *testing.T) {
	view := upstreamView(model.UpstreamComparison{Changes: []model.UpstreamChange{
		{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod adds golang.org/x/net v0.44.0", Hold: true},
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true},
	}})
	require.Equal(t, "go.mod adds golang.org/x/net v0.44.0", view.Changes[0].Message)
	require.Equal(t, "LICENSE changed; the Portfile's license line may need to follow", view.Changes[1].Message)
	require.True(t, view.Held)
}

// Serve's state is fields, and its line is written from them: whether it
// runs, as which process, whether it opens pull requests, and the queue
// with what has stopped (the hugo exercise's certigo run, finding 6).
func TestServesStateIsFieldsAndItsLine(t *testing.T) {
	require.Equal(t, "serve: not running · queue: empty", serveWords(engine.ServeState{}))
	running := engine.ServeState{Running: true, PID: 34857, OpensPullRequests: true, Queue: 2, Stopped: 1}
	require.Equal(t, "serve: running (pid 34857) · opens PRs for passing updates · queue: 2 runs, 1 stopped", serveWords(running))
	data, err := json.Marshal(serveView(running))
	require.NoError(t, err)
	require.JSONEq(t, `{"running": true, "pid": 34857, "opens_pull_requests": true, "queue": 2, "stopped": 1}`, string(data))
	running.OpensPullRequests, running.Stopped = false, 0
	require.Equal(t, "serve: running (pid 34857) · queue: 2 runs", serveWords(running))
	data, err = json.Marshal(serveView(running))
	require.NoError(t, err)
	require.JSONEq(t, `{"running": true, "pid": 34857, "queue": 2, "stopped": 0}`, string(data))
}

// logs --json says what each environment was as its run began, as
// evidence compares it later, and nothing where its provider can't say.
func TestLogsSayWhatTheEnvironmentWas(t *testing.T) {
	logs := engine.RunLogs{Executions: []engine.ExecutionLogs{
		{Execution: model.GuestExecution{ID: "tart_1", Attempt: 1, State: model.ExecutionFinished, Identity: "source sha256:a; setup 3"}},
		{Execution: model.GuestExecution{ID: "command_1", Attempt: 1, State: model.ExecutionFinished}},
	}}
	data, err := json.Marshal(logsView(logs))
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(data, &view))
	require.Equal(t, "source sha256:a; setup 3", dig(t, view, "executions", 0, "identity"))
	require.NotContains(t, dig(t, view, "executions", 1), "identity")
}

// logs says, of a Git-fetched target's build, the commit it fetched and
// the source it was to fetch, or that its provider didn't say (batch 20).
func TestLogsSayWhatAGitFetchedBuildFetched(t *testing.T) {
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
	require.NoError(t, writeLogs(&out, logs))
	require.Contains(t, out.String(), "    libharbor passed\n      fetched 1a2b3c4, the commit git.branch v4 named when the check was planned\n    harbor-cli passed\n")
	require.Contains(t, out.String(), "    libharbor passed\n      which commit of git.branch v4 it fetched isn't known: its provider didn't say; 1a2b3c4 was expected\n")

	data, err := json.Marshal(logsView(logs))
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(data, &view))
	require.Equal(t, string(commit), dig(t, view, "executions", 0, "results", 0, "fetched"))
	require.Equal(t, "v4", dig(t, view, "executions", 0, "results", 0, "git", "branch"))
	require.NotContains(t, dig(t, view, "executions", 0, "results", 1), "git", "fetched otherwise")
	require.NotContains(t, dig(t, view, "executions", 1, "results", 0), "fetched", "its provider didn't say")
}

// A plan's JSON carries what the person asked and what --only left out,
// as its text does: the changed targets submission still requires.
func TestAPlansJSONSaysWhatWasAskedAndLeftOut(t *testing.T) {
	arm := model.Environment{Provider: "command"}
	plan := model.Plan{ID: "p1", Environments: []model.Environment{arm}, Tests: model.TestsDeclared, Only: []string{"jq"}, Also: []string{"oniguruma"},
		Targets: []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed},
			{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Directory: "textproc/oniguruma", Kind: model.Unchanged, Role: model.Also}},
		Omitted: []model.PlanTarget{{ID: "libharbor", Target: model.Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: model.Substantive, Role: model.Changed}},
		Builds:  []model.EnvironmentPlan{{Environment: arm, Order: []model.TargetID{"jq", "oniguruma"}}}}
	view := planView(plan)
	require.Equal(t, []string{"jq"}, view.Only)
	require.Equal(t, []string{"oniguruma"}, view.Also)
	require.Len(t, view.Omitted, 1)
	require.Equal(t, "libharbor", view.Omitted[0].Name)
}
