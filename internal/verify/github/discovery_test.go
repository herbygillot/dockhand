package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"testing"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestMissingRunWorkflowDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state, want string
		missing, replaced bool
	}{
		{name: "active", state: "active", want: "main.yml is active"},
		{name: "disabled", state: "disabled_manually", want: "enable the workflow"},
		{name: "missing", missing: true, want: "missing or inaccessible"},
		{name: "replaced", state: "active", replaced: true, want: "original workflow ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			initial, err := f.provider.Submit(t.Context(), f.request)
			require.NoError(t, err)
			require.Equal(t, verify.SubmissionUncertain, initial.State)
			row, err := f.provider.read(t.Context(), f.request.ID)
			require.NoError(t, err)
			f.api.flow.State = gh.Ptr(tc.state)
			f.api.flow.HTMLURL = gh.Ptr("https://github.com/contributor/macports-ports/actions/workflows/main.yml")
			if tc.replaced {
				f.api.flow.ID = gh.Ptr(int64(8))
			}
			var api actionsAPI = f.api
			if tc.missing {
				api = workflowFailure{actionsAPI: f.api, err: &gh.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}}
			}
			restarted := *f.provider
			restarted.backend = func(context.Context, string) (actionsAPI, error) { return api, nil }
			result, err := restarted.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
			require.NoError(t, err)
			require.Equal(t, verify.RunUnknown, result.State)
			for _, want := range []string{tc.want, "push confirmed", string(f.request.Spec.Source.Commit), "candidate", row.CreatedAt.Format(time.RFC3339), "dockhand wait <job-id> --trace", "dockhand cancel <job-id> --wait"} {
				require.Contains(t, result.Submission.Detail, want)
			}
			after, err := restarted.read(t.Context(), f.request.ID)
			require.NoError(t, err)
			require.Equal(t, row, after, "diagnostics must not change immutable intent or settle missing evidence")
			// A delayed run still belongs to the accepted workflow, even if its current settings changed.
			f.ready()
			result, err = restarted.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
			require.NoError(t, err)
			require.Equal(t, verify.RunFound, result.State)
		})
	}
}

type workflowFailure struct {
	actionsAPI
	err error
}

func (a workflowFailure) Workflow(context.Context, string) (*gh.Workflow, error) { return nil, a.err }

func TestMissingRunRetainsTransientErrors(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	unavailable := errors.New("temporary Actions failure")
	f.provider.backend = func(context.Context, string) (actionsAPI, error) {
		return workflowFailure{actionsAPI: f.api, err: unavailable}, nil
	}
	_, err = f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.ErrorIs(t, err, unavailable)
	row, err := f.provider.read(t.Context(), f.request.ID)
	require.NoError(t, err)
	require.Equal(t, record.ExecutionReserved, row.State)
}

func TestMissingRunWithMovedRemoteKeepsObserving(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	base := string(f.request.Spec.Source.Base)
	// Move the remote to an unrelated observed head without changing the accepted local source.
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", f.remote, "update-ref", "refs/heads/candidate", base)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	result, err := f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RunUnknown, result.State)
	require.Contains(t, result.Submission.Detail, "no replacement push was made")
	head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
	require.NoError(t, err)
	require.Equal(t, base, head.Object)
	f.ready()
	result, err = f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RunFound, result.State)
}

func TestOldMissingRunHasNoAutomaticFailureDeadline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	var config Config
	require.NoError(t, json.Unmarshal(f.request.Spec.Config.ProviderConfig, &config))
	raw, err := json.Marshal(payload{Request: f.request, Config: config, Matrix: []string{"macos-14", "macos-15"}})
	require.NoError(t, err)
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, f.provider.locked(t.Context(), f.request.ID, func(ctx context.Context, e *ledger.Entry) error {
		return e.Put(ctx, record.ProviderExecution{ID: f.request.ID, RepositoryID: f.provider.Repository, AttemptID: f.request.AttemptID, Resource: string(f.request.ID), Payload: raw, State: record.ExecutionReserved, Occupied: true, CreatedAt: created})
	}))
	result, err := f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RunUnknown, result.State)
	require.Contains(t, result.Submission.Detail, created.Format(time.RFC3339))
}

func TestDriverRecordsMissingRunGuidanceAndCancelsOffline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	scope := workflow.Scope{Jobs: []record.JobID{f.job}}
	f.settle(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), scope)
		require.NoError(t, err)
		return status.Jobs[0].Attempts[0].State == record.AttemptUncertain
	})
	status, err := f.engine.Status(t.Context(), scope)
	require.NoError(t, err)
	require.Contains(t, status.Jobs[0].Job.Detail, "dockhand cancel <job-id> --wait")
	f.api.err = errors.New("offline")
	require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: "stop-missing-run", Kind: record.Cancel, Jobs: scope.Jobs}))
	f.settle(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), scope)
		require.NoError(t, err)
		return status.Jobs[0].Job.State == record.JobCanceled
	})
	head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
	require.NoError(t, err)
	require.Equal(t, string(f.request.Spec.Source.Commit), head.Object)
	f.ready()
	f.api.err = nil
	result, err := f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
	require.NoError(t, err)
	require.Equal(t, verify.RequestClosed, result.State, "late discovery must not revive canceled tracking")
}
