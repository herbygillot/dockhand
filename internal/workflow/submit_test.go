package workflow_test

import (
	"context"
	"github.com/herbygillot/dockhand/internal/state"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestSubmitCanonicalRetriesAndFrozenInputs(t *testing.T) {
	f := newFixture(t)
	request := f.request("request")
	request.Spec.Targets = append(request.Spec.Targets, record.Target{Name: "other", Portfile: "other/Portfile"})
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	initial, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, record.JobQueued, initial.State.Jobs[receipt.JobID].State, "acceptance should only queue work")
	require.Nil(t, initial.State.Jobs[receipt.JobID].AdmittedAt, "acceptance contacted provider or implied admission")
	require.Zero(t, f.provider.count("submit"), "acceptance contacted provider or implied admission")
	request.Spec.Targets[0], request.Spec.Targets[1] = request.Spec.Targets[1], request.Spec.Targets[0]
	request.Spec.Targets[0].Variants = map[string]bool{}
	retry, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, receipt, retry, "equivalent retry changed receipt or state")
	require.Equal(t, initial.Version, after.Version, "equivalent retry changed receipt or state")
	request.Spec.Build.EnvironmentDigest = "different"
	request.Spec.Targets[1].Variants["debug"] = true
	_, err = f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, workflow.ErrRequestConflict, "different intent: %v", err)
	stored := f.status(t, receipt.JobID).Jobs[0].Job.Spec
	require.Equal(t, "sha256:fixture", stored.Build.EnvironmentDigest, "caller mutated accepted inputs")
	require.False(t, stored.Targets[0].Variants["debug"], "caller mutated accepted inputs")
	request = f.request("request")
	request.Spec.Targets = append(request.Spec.Targets, record.Target{Name: "other", Portfile: "other/Portfile"})
	require.NoError(t, f.mutate(t.Context(), func(_ context.Context, tx *fixtureTx) error {
		change := tx.State.Changes["change"]
		change.CurrentRevision = "revision"
		change.Disposition = record.ChangeClosed
		tx.State.Changes[change.ID] = change
		return nil
	}))
	retry, err = f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry, "change advancement broke retry identity")
}

func TestSubmitRejectsInvalidIntentWithoutPersisting(t *testing.T) {
	f := newFixture(t)
	cases := map[string]func(*workflow.Request){
		"empty identity":       func(r *workflow.Request) { r.ID = "" },
		"implicit policy":      func(r *workflow.Request) { r.Spec.Verification = "" },
		"conflicting source":   func(r *workflow.Request) { r.Spec.Source = f.source },
		"target traversal":     func(r *workflow.Request) { r.Spec.Targets[0].Portfile = "../Portfile" },
		"duplicate target":     func(r *workflow.Request) { r.Spec.Targets = append(r.Spec.Targets, r.Spec.Targets[0]) },
		"invalid variant":      func(r *workflow.Request) { r.Spec.Targets[0].Variants = map[string]bool{"+debug": true} },
		"missing provider":     func(r *workflow.Request) { r.Spec.Build.Provider = "" },
		"missing tests policy": func(r *workflow.Request) { r.Spec.Build.Tests = "" },
	}
	before, err := f.snapshot(t.Context())
	require.NoError(t, err)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			request := f.request("invalid")
			mutate(&request)
			receipt, err := f.engine.Submit(t.Context(), request)
			require.ErrorIs(t, err, workflow.ErrInvalidRequest, "got receipt %+v, error %v", receipt, err)
			require.Equal(t, workflow.Receipt{}, receipt, "got receipt %+v, error %v", receipt, err)
		})
	}
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version, "rejected request changed state")
}

func TestSubmitRevisionConstraints(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.mutate(t.Context(), func(_ context.Context, tx *fixtureTx) error {
		r := tx.State.Revisions["revision"]
		r.ID = "newer"
		r.Previous = "revision"
		tx.State.Revisions[r.ID] = r
		c := tx.State.Changes["change"]
		c.CurrentRevision = r.ID
		tx.State.Changes[c.ID] = c
		return nil
	}))
	_, err := f.engine.Submit(t.Context(), f.request("old-verification"))
	require.NoError(t, err)
	request := f.request("stale-edit")
	request.Spec.Action = record.Bump
	_, err = f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, workflow.ErrStaleRevision, "stale edit accepted: %v", err)
	request = f.request("missing")
	request.Spec.InputRevision = "missing"
	_, err = f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, state.ErrNotFound, "missing revision accepted: %v", err)
	request = f.request("future-action")
	request.Spec.Action = record.Rebase
	_, err = f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, workflow.ErrInvalidRequest, "correction without bound intent accepted: %v", err)
}

func TestConcurrentEquivalentSubmissionsConverge(t *testing.T) {
	f := newFixture(t)
	request := f.request("same-request")
	const callers = 4
	receipts := make([]workflow.Receipt, callers)
	errs := make([]error, callers)
	var group sync.WaitGroup
	for i := range callers {
		group.Go(func() { receipts[i], errs[i] = f.engine.Submit(t.Context(), request) })
	}
	group.Wait()
	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, receipts[0], receipts[i], "callers received different jobs")
	}
	state, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Len(t, state.State.Jobs, 1, "duplicate jobs persisted")
	require.Len(t, state.State.Requests, 1, "duplicate jobs persisted")
}

func TestControlsAreIdempotentAndScoped(t *testing.T) {
	f := newFixture(t)
	a, b := f.submit(t, "a"), f.submit(t, "b")
	request := record.ControlRequest{ID: "cancel-both", Kind: record.Cancel, Jobs: []record.JobID{b, a, b}, Reason: "stop"}
	require.NoError(t, f.engine.Control(t.Context(), request))
	f.run(t, a)
	state, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, record.JobCanceled, state.State.Jobs[a].State, "scope affected other job: %+v", state.State.Controls[request.ID])
	require.Nil(t, state.State.Jobs[b].CancelRequestedAt, "scope affected other job: %+v", state.State.Controls[request.ID])
	require.Nil(t, state.State.Controls[request.ID].AppliedAt, "scope affected other job: %+v", state.State.Controls[request.ID])
	f.run(t, b)
	state, err = f.snapshot(t.Context())
	require.NoError(t, err)
	require.NotNil(t, state.State.Controls[request.ID].AppliedAt, "fully applied control remains pending")
	request.Jobs = []record.JobID{a, b}
	require.NoError(t, f.engine.Control(t.Context(), request))
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, state.Version, after.Version, "equivalent control retry rewrote state")
	request.Reason = "different"
	require.ErrorIs(t, f.engine.Control(t.Context(), request), workflow.ErrRequestConflict, "conflicting control accepted")
	collision := f.request(string(request.ID))
	_, err = f.engine.Submit(t.Context(), collision)
	require.ErrorIs(t, err, workflow.ErrRequestConflict, "job reused control identity")
}

func TestStatusIsAnIndependentReadOnlyProjection(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "status")
	f.run(t, id)
	require.NoError(t, f.mutate(t.Context(), func(_ context.Context, tx *fixtureTx) error {
		tx.State.Changes["unrelated"] = record.Change{ID: "unrelated", Disposition: record.ChangeOpen}
		return nil
	}))
	before, err := f.snapshot(t.Context())
	require.NoError(t, err)
	scoped := f.status(t, id)
	all, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, scoped.Resources, 1, "status associations are wrong")
	require.Len(t, all.Resources, 1, "status associations are wrong")
	require.Len(t, scoped.Changes, 1, "status associations are wrong")
	require.Len(t, all.Changes, 2, "status associations are wrong")
	scoped.Jobs[0].Job.Spec.Targets[0].Variants["debug"] = true
	require.False(t, f.status(t, id).Jobs[0].Job.Spec.Targets[0].Variants["debug"], "status mutation escaped its snapshot")
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version, "status changed state")
	require.Equal(t, before.State, after.State, "status changed state")
	require.Zero(t, f.provider.count("observe"), "status polled the provider")
	for _, scope := range []workflow.Scope{{}, {All: true, Jobs: []record.JobID{id}}, {Jobs: []record.JobID{""}}} {
		_, err := f.engine.Status(t.Context(), scope)
		require.ErrorIs(t, err, workflow.ErrInvalidScope, "invalid scope accepted: %+v", scope)
	}
	_, err = f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{"missing"}})
	require.ErrorIs(t, err, state.ErrNotFound, "unknown job accepted: %v", err)
}

func TestRetryAcceptsRedundantMatchingChangeIdentity(t *testing.T) {
	f := newFixture(t)
	request := f.request("retry-change")
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	request.Spec.ChangeID = "change"
	again, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	request.Spec.ChangeID = "wrong"
	_, err = f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, workflow.ErrRequestConflict)
}
