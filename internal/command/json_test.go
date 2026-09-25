package command

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
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
	require.Equal(t, "snapshot 1", dig(t, planned.Result, "revision", "description"))
	require.Nil(t, planned.Result["run"])

	queued, err := jsonOf(t, "check", "-d")
	require.NoError(t, err)
	require.Equal(t, "queued", dig(t, queued.Result, "run", "state"))
	listed, err := jsonOf(t, "queue")
	require.NoError(t, err)
	require.Equal(t, "check-1", dig(t, listed.Result, "runs", 0, "name"))
	require.Equal(t, "jq-update", dig(t, listed.Result, "runs", 0, "branch"))
	require.Equal(t, "serve: not running · queue: 1 run", listed.Result["serve"])

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

	explained, err := jsonOf(t, "explain", "merge")
	require.NoError(t, err)
	require.Equal(t, "https://guide.macports.org/#project.github", dig(t, explained.Result, "sources", 0, "url"))

	g.prs[0].State = record.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)
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
