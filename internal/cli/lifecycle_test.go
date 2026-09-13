package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func queuedJob(t *testing.T) (app.Config, record.JobID) {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "candidate"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + os.DevNull, "commit", "-q", "--allow-empty", "-m", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	config := app.Config{Repository: root, DBPath: filepath.Join(t.TempDir(), "state.db"), Tart: tart.Config{Home: t.TempDir(), Executable: "/usr/bin/false"}}
	services, err := app.Build(t.Context(), config)
	require.NoError(t, err)
	defer services.Close()
	commit, tree, err := services.Workflow.Repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)}
	require.NoError(t, services.Workflow.State.Update(t.Context(), services.Workflow.Repository, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutChange(ctx, record.Change{ID: "change", Branch: "candidate", CurrentRevision: "revision", Disposition: record.ChangeOpen}); err != nil {
			return err
		}
		return tx.PutRevision(ctx, record.Revision{ID: "revision", ChangeID: "change", Source: source, CreatedAt: time.Now()})
	}))
	receipt, err := services.Workflow.Submit(t.Context(), workflow.Request{ID: "request", Spec: record.JobSpec{Action: record.Verify, InputRevision: "revision", Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, Verification: record.VerificationRequired, Destination: record.VerificationComplete}})
	require.NoError(t, err)
	return config, receipt.JobID
}
func TestWaitRecordsFailureWithoutSubmittingNewWork(t *testing.T) {
	config, id := queuedJob(t)
	var stdout, stderr bytes.Buffer
	err := Run(t.Context(), []string{"wait", string(id), "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorIs(t, err, ErrNeedsAttention)
	var result ActionResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, id, result.Status.Jobs[0].Job.ID)
	require.Equal(t, record.JobNeedsAttention, result.Status.Jobs[0].Job.State)
	status, err := app.Status(t.Context(), config)
	require.NoError(t, err)
	require.Len(t, status.Jobs, 1)
}
func TestCancelBeforeAdmissionPreservesBranchAndDoesNotNeedTart(t *testing.T) {
	config, id := queuedJob(t)
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"cancel", string(id), "--wait", "--reason", "fixture", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	var result ActionResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, record.JobCanceled, result.Status.Jobs[0].Job.State)
	require.Empty(t, result.Status.Jobs[0].Attempts)
	services, err := app.Build(t.Context(), config)
	require.NoError(t, err)
	defer services.Close()
	commit, _, err := services.Workflow.Repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, string(result.Status.Jobs[0].Job.Spec.Source.Commit), commit)
}
func TestForeignRepositoryJobCannotBeWaitedOrCanceled(t *testing.T) {
	config, id := queuedJob(t)
	other := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "-q", other).CombinedOutput()
	require.NoError(t, err, "%s", output)
	config.Repository = other
	for _, name := range []string{"wait", "cancel"} {
		var out bytes.Buffer
		err := Run(t.Context(), []string{name, string(id)}, Streams{Out: &out, Err: &out}, config)
		require.ErrorIs(t, err, state.ErrNotFound)
	}
}

func TestResidentDriverAppliesAcceptedControlAndStopsWithoutNewWork(t *testing.T) {
	config, id := queuedJob(t)
	services, err := app.Build(t.Context(), config)
	require.NoError(t, err)
	require.NoError(t, services.Workflow.Control(t.Context(), record.ControlRequest{ID: "cancel", Kind: record.Cancel, Jobs: []record.JobID{id}}))
	require.NoError(t, services.Close())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Run(ctx, []string{"start", "--json"}, Streams{Out: &stdout, Err: &stderr}, config) }()
	require.Eventually(t, func() bool {
		status, err := app.Status(t.Context(), config)
		return err == nil && len(status.Jobs) == 1 && status.Jobs[0].Job.State == record.JobCanceled
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	var result struct{ Stopped, Interrupted bool }
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.True(t, result.Stopped)
	require.True(t, result.Interrupted)
	status, err := app.Status(t.Context(), config)
	require.NoError(t, err)
	require.Len(t, status.Jobs, 1)
	require.Empty(t, status.Jobs[0].Attempts)
}
