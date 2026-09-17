package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCollectReleasedTartArtifactsWithMultipleProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init", "-q")
	repo, err := git.Open(t.Context(), repoRoot, "")
	require.NoError(t, err)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	config := app.Config{Repository: repoRoot, DBPath: filepath.Join(t.TempDir(), "state.db"), Tart: tart.Config{Home: home, ArtifactDirectory: directory, Executable: "/missing/tart"}}
	store, err := sqlite.Open(t.Context(), config.DBPath, sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	repository, err := store.RegisterRepository(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Millisecond)
	source := record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}
	target := record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}
	require.NoError(t, store.Update(t.Context(), repository.ID, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutRequest(ctx, record.AcceptedRequest{ID: "request", Kind: record.JobRequest, Payload: []byte("{}"), AcceptedAt: old}); err != nil {
			return err
		}
		if err := tx.PutJob(ctx, record.Job{ID: "job", RequestID: "request", State: record.JobCompleted, Phase: record.PhaseVerification, AcceptedAt: old, FinishedAt: &old, Spec: record.JobSpec{Action: record.Verify, Source: source, Targets: []record.Target{target}, Destination: record.VerificationComplete, Verification: record.VerificationRequired}}); err != nil {
			return err
		}
		if err := tx.PutAttempt(ctx, record.Attempt{ID: "attempt", JobID: "job", TargetID: "target", State: record.AttemptFinished, CreatedAt: old, Spec: record.BuildSpec{Source: source, Target: target, Config: record.BuildConfig{Provider: "tart"}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: old}}); err != nil {
			return err
		}
		if err := tx.PutSubmission(ctx, record.Submission{ID: "submission", AttemptID: "attempt", Sequence: 1, Provider: "tart", CreatedAt: old}); err != nil {
			return err
		}
		return tx.PutResource(ctx, record.Resource{ID: "resource", AttemptID: "attempt", SubmissionID: "submission", Handle: record.ResourceHandle{Provider: "tart", ID: "submission"}, State: record.ResourceReleased, ReleasedAt: &old})
	}))
	hash := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	poolID := "tart_" + hash(home)
	_, err = store.RegisterProviderPool(t.Context(), record.ProviderPool{ID: poolID, Scope: "tart:" + home, Directory: directory, Capacity: 2})
	require.NoError(t, err)
	vm := "dockhand2-" + hash(poolID + "/submission")[:24]
	raw, err := json.Marshal(struct{ Config tart.Config }{config.Tart})
	require.NoError(t, err)
	execution := record.ProviderExecution{ID: "submission", RepositoryID: repository.ID, AttemptID: "attempt", Resource: vm, Payload: raw, State: record.ExecutionReserved, Occupied: true, CreatedAt: old}
	require.NoError(t, store.ProviderUpdate(t.Context(), poolID, func(ctx context.Context, tx state.ProviderTx) error {
		if err := tx.PutExecution(ctx, execution); err != nil {
			return err
		}
		execution.State = record.ExecutionReleased
		execution.Occupied = false
		return tx.PutExecution(ctx, execution)
	}))
	path := filepath.Join(directory, vm)
	require.NoError(t, os.MkdirAll(path, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "build.log"), []byte("old diagnostics"), 0600))
	for _, dry := range []bool{true, false} {
		result, err := app.Collect(t.Context(), config, app.CollectOptions{Retention: workflow.RetentionOptions{OlderThan: 7 * 24 * time.Hour, DryRun: dry}})
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		require.Equal(t, "prune-artifacts", result.Items[0].Action)
		require.Equal(t, !dry, result.Items[0].Completed, result.Items[0].Detail)
		if dry {
			require.DirExists(t, path)
		} else {
			require.NoDirExists(t, path)
		}
	}
	require.NoError(t, store.View(t.Context(), repository.ID, func(ctx context.Context, r state.Reader) error {
		resource, err := r.Resource(ctx, "resource")
		if err != nil {
			return err
		}
		require.NotNil(t, resource.ArtifactsPrunedAt)
		attempt, err := r.Attempt(ctx, "attempt")
		if err != nil {
			return err
		}
		require.Equal(t, record.VerdictPassed, attempt.Evidence.Verdict)
		return nil
	}))
}
