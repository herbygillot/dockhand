package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCommandProgressStaysOnStderrAndEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	db := filepath.Join(t.TempDir(), "state.db")
	root, err := NewRoot(app.Config{DBPath: db})
	require.NoError(t, err)
	root.AddCommand(&cobra.Command{Use: "fixture", RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := progress.WithScope(cmd.Context(), "attempt_one")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(progress.WithScope(ctx, "attempt_two"), "packing source")
		return nil
	}})
	var out, diagnostics bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&diagnostics)
	root.SetArgs([]string{"--json", "fixture"})
	require.NoError(t, root.ExecuteContext(t.Context()))
	require.Empty(t, out.String(), "a fixture command without a result writes nothing; run writes the envelope")
	require.Equal(t, "{\"level\":\"info\",\"scope\":\"attempt_one\",\"message\":\"staging\\n\\u001b[31m\"}\n{\"level\":\"info\",\"scope\":\"attempt_two\",\"message\":\"packing source\"}\n", diagnostics.String(), "JSON mode reports one object per line on stderr")
	require.NoFileExists(t, db)
}

func TestCohortProgressCountsQueuedTargets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	reporter := newReporter(&out, nil, false, progress.Verbose, false)
	status := workflow.Status{Jobs: []view.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}, Attempts: []record.Attempt{{State: record.AttemptQueued}, {State: record.AttemptRunning}, {State: record.AttemptQueued}}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, 1, strings.Count(out.String(), "waiting for provider admission"))
	require.Contains(t, out.String(), "2 targets waiting")
}

// At the info level an attached command tells the story of its job in the
// status table's words, one line per milestone, and repeats nothing.
func TestAttachedCommandNarratesMilestonesAtInfo(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	var out bytes.Buffer
	reporter := newReporter(&out, nil, false, progress.Info, false)
	entry := view.JobStatus{Job: record.Job{ID: "job_1", ChangeID: "change_1", State: record.JobActive, Phase: record.PhasePreparation, Detail: "Prepared candidate; awaiting branch integration",
		Spec: record.JobSpec{Action: record.Bump, Destination: record.Published, Targets: []record.Target{{Name: "jq"}}}}}
	status := workflow.Status{Jobs: []view.JobStatus{entry}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Empty(t, out.String(), "nothing to say before the release is resolved")
	status.Jobs[0].Job.ResolvedRelease = &record.Release{Selection: record.Selection{CurrentVersion: "1.7"}, Version: "1.8.1"}
	require.NoError(t, reporter.status(t.Context(), status))
	status.Jobs[0].Job.Prepared = &record.PreparedChange{Branch: "dockhand/bump/jq-abc"}
	status.Jobs[0].Job.ResultRevision = "revision"
	status.Jobs[0].Job.Phase = record.PhaseVerification
	status.Jobs[0].Job.Detail = "Prepared branch dockhand/bump/jq-abc; verification pending"
	status.Jobs[0].Attempts = []record.Attempt{{ID: "attempt_1", State: record.AttemptQueued, Spec: record.BuildSpec{Target: record.Target{Name: "jq"}, Config: record.BuildConfig{Platform: platform}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.NoError(t, reporter.status(t.Context(), status), "a repeated snapshot adds no lines")
	status.Jobs[0].Attempts[0].State = record.AttemptRunning
	require.NoError(t, reporter.status(t.Context(), status))
	status.Jobs[0].Attempts[0].State = record.AttemptFinished
	status.Jobs[0].Attempts[0].Evidence = &record.Evidence{Verdict: record.VerdictPassed}
	status.Jobs[0].Job.Phase = record.PhasePublication
	status.Jobs[0].Job.Detail = "Pushed verified branch author/ports:dockhand/bump/jq-abc; checking the remote before publishing"
	require.NoError(t, reporter.status(t.Context(), status))
	status.Jobs[0].Job.State = record.JobCompleted
	status.Jobs[0].Job.Detail = "Published https://github.com/macports/macports-ports/pull/1"
	status.Jobs[0].Publications = []record.PublicationAction{{ID: "publication_1", State: record.PublicationConfirmed}}
	status.PullRequests = []record.PullRequest{{ChangeID: "change_1", State: record.PullRequestOpen, Ref: record.PullRequestRef{URL: "https://github.com/macports/macports-ports/pull/1"}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, "jq: 1.7 -> 1.8.1\njq: branch dockhand/bump/jq-abc prepared\njq: waiting for capacity\njq: building on macOS 26\njq: passed on macOS 26 arm64\njq: publishing\njq: PR https://github.com/macports/macports-ports/pull/1 (created)\n", out.String())
	require.NotContains(t, out.String(), "job_1")
	require.NotContains(t, out.String(), "awaiting branch integration", "the driver's detail stays at -v")

	out.Reset()
	failed := view.JobStatus{Job: record.Job{ID: "job_2", State: record.JobFailed, Phase: record.PhaseVerification, Detail: "verification failed", Spec: record.JobSpec{Action: record.Verify, Targets: []record.Target{{Name: "deno"}}}},
		Attempts: []record.Attempt{{ID: "attempt_2", State: record.AttemptFinished, Spec: record.BuildSpec{Target: record.Target{Name: "deno"}, Config: record.BuildConfig{Platform: platform}}, Evidence: &record.Evidence{Verdict: record.VerdictFailed, Failure: &record.Failure{Phase: "build", Detail: "linking failed"}, Logs: []record.Artifact{{Location: "/tmp/build.log"}}}}}}
	require.NoError(t, reporter.status(t.Context(), workflow.Status{Jobs: []view.JobStatus{failed}}))
	require.Equal(t, "deno: failed on macOS 26 arm64; build phase: linking failed; log: /tmp/build.log\ndeno: failed; verification failed\n", out.String())
}
