package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestVerifyCLIInfersTrackedBranchAndCurrentCheckout(t *testing.T) {
	t.Parallel()
	config, repo, _ := preparationCLI(t)
	configureReuseImage(t, &config)
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--no-verify", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	var prepared ActionResult
	decodeResult(t, stdout.Bytes(), &prepared)
	branch := prepared.Status.Jobs[0].Job.Prepared.Branch
	seedCLIVerification(t, config, branch)
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "checkout", "-f", branch).CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, args := range [][]string{{"verify", "--branch", branch, "--json"}, {"verify", "--trace", "--json"}} {
		stdout.Reset()
		stderr.Reset()
		require.NoError(t, Run(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
		var result ActionResult
		decodeResult(t, stdout.Bytes(), &result)
		job := result.Status.Jobs[0].Job
		require.Equal(t, record.JobCompleted, job.State)
		require.Equal(t, prepared.Status.Jobs[0].Job.ChangeID, job.ChangeID)
		require.Equal(t, record.AttemptID("original-attempt"), job.ReusedAttempt)
		require.Equal(t, prepared.Status.Jobs[0].Job.Spec.Targets, job.Spec.Targets)
		require.Contains(t, stderr.String(), "inferred from tracked contribution")
	}
	name := filepath.Join(repo.Root, "devel/fixture/Portfile")
	contents, err := os.ReadFile(name)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(name, append(contents, []byte("\n# human correction\nvariant debug description {Debug} {}\n")...), 0600))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	diagnostics := &detachOnAcceptance{cancel: cancel}
	stdout.Reset()
	err = Run(ctx, []string{"verify", "--working-tree", "--variant", "+debug", "--fresh", "--json", "-v"}, Streams{Out: &stdout, Err: diagnostics}, config)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, diagnostics.String(), "inferred from tracked contribution")
	require.Contains(t, diagnostics.String(), "Variants: +debug")
	status, err := app.Status(t.Context(), config)
	require.NoError(t, err)
	job := status.Jobs[len(status.Jobs)-1].Job
	require.Equal(t, record.JobQueued, job.State)
	require.Empty(t, job.Spec.Source.Commit)
	require.Equal(t, 1, job.Spec.Checkout.ModifiedFiles)
	require.Equal(t, map[string]bool{"debug": true}, job.Spec.Targets[0].Variants)
	require.True(t, job.Spec.FreshVerification)
	require.NotEqual(t, prepared.Status.Jobs[0].Job.Prepared.Source.Tree, job.Spec.Source.Tree)
}

func TestVerifyCLIRejectsUnknownInferenceWithoutAcceptingWork(t *testing.T) {
	t.Parallel()
	config, _, _ := preparationCLI(t)
	configureReuseImage(t, &config)
	var stdout, stderr bytes.Buffer
	err := Run(t.Context(), []string{"verify", "--branch", "candidate", "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, "without a tracked contribution; specify a port explicitly")
	var envelope Envelope
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Equal(t, "verify", envelope.Command)
	require.Equal(t, 1, envelope.ExitCode)
	require.Contains(t, envelope.Error, "specify a port explicitly")
	require.Nil(t, envelope.Result, "a refused command has an envelope with no result")
	status, err := app.Status(t.Context(), config)
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
	require.Empty(t, status.Changes)
	invalid := app.Config{Repository: "/missing/repo", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
	for _, args := range [][]string{{"verify", ""}, {"verify", "one", "two"}, {"verify", "--branch="}} {
		err := Run(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, invalid)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git ")
	}
	require.NoFileExists(t, invalid.DBPath)
}
