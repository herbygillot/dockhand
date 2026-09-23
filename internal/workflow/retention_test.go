package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/workflow/retention"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type pruningProvider struct {
	*scriptedProvider
	prune func(context.Context, record.ResourceHandle) error
}

func (p pruningProvider) PruneArtifacts(ctx context.Context, h record.ResourceHandle) error {
	if err := p.begin(ctx, "prune"); err != nil {
		return err
	}
	if p.prune != nil {
		return p.prune(ctx, h)
	}
	return nil
}
func retainedFixture(t *testing.T) (*fixture, record.JobID) {
	t.Helper()
	f := newFixture(t)
	request := f.request("retention")
	request.Spec.KeepFailed = true
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	id := receipt.JobID
	f.run(t, id)
	f.provider.observe = terminal(f, record.VerdictFailed)
	f.run(t, id)
	require.Equal(t, record.ResourceRetained, f.status(t, id).Resources[0].State)
	f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
	return f, id
}

func TestRetentionUsesSeparateJobAndReleaseAgesAndPreservesHistory(t *testing.T) {
	t.Parallel()
	f, id := retainedFixture(t)
	before := f.status(t, id)
	options := retention.Options{OlderThan: 7 * 24 * time.Hour, DryRun: true}
	result, err := f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Empty(t, result.Items)
	f.advance(8 * 24 * time.Hour)
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "release", result.Items[0].Action)
	require.Zero(t, f.provider.count("release"))
	require.Nil(t, f.status(t, id).Resources[0].RetainUntil)
	options.DryRun = false
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.True(t, result.Items[0].Completed)
	require.Equal(t, 1, f.provider.count("release"))
	require.Zero(t, f.provider.count("prune"))
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Empty(t, result.Items, "recently released logs must survive the VM cleanup")
	f.advance(8 * 24 * time.Hour)
	options.DryRun = true
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, "prune-artifacts", result.Items[0].Action)
	require.Zero(t, f.provider.count("prune"))
	options.DryRun = false
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.True(t, result.Items[0].Completed)
	after := f.status(t, id)
	require.NotNil(t, after.Resources[0].ArtifactsPrunedAt)
	require.Equal(t, before.Jobs, after.Jobs)
	require.Equal(t, before.Changes, after.Changes)
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.Equal(t, 1, f.provider.count("prune"))
	// The pruning marker cannot be erased or used to mutate released ownership.
	require.ErrorIs(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		r := after.Resources[0]
		r.ArtifactsPrunedAt = nil
		return tx.PutResource(ctx, r)
	}), workflow.ErrRequestConflict)
}

func TestRetentionSkipsLiveWorkClaimsAndExplicitRetention(t *testing.T) {
	t.Parallel()
	f, id := retainedFixture(t)
	f.advance(8 * 24 * time.Hour)
	resource := f.status(t, id).Resources[0]
	future := f.now().Add(time.Hour)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		resource.RetainUntil = &future
		return tx.PutResource(ctx, resource)
	}))
	result, err := f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		resource.RetainUntil = nil
		resource.ClaimGeneration++
		resource.Claim = &record.Claim{Owner: "other", Generation: resource.ClaimGeneration, ExpiresAt: future}
		return tx.PutResource(ctx, resource)
	}))
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	f.advance(2 * time.Hour)
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.True(t, result.Items[0].Completed)
	// New admitted work remains untouched even with an explicit zero age.
	req := f.request("active")
	req.Spec.FreshVerification = true
	receipt, err := f.engine.Submit(t.Context(), req)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	require.Equal(t, record.AttemptRunning, f.attempt(t, receipt.JobID).State)
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	for _, item := range result.Items {
		require.NotEqual(t, f.status(t, receipt.JobID).Resources[0].ID, item.ResourceID)
	}
}

type failPruneStore struct{ state.Scoped }
type failPruneTx struct{ state.Tx }

func (s failPruneStore) Update(ctx context.Context, fn func(context.Context, state.Tx) error) error {
	return s.Scoped.Update(ctx, func(ctx context.Context, tx state.Tx) error { return fn(ctx, failPruneTx{tx}) })
}
func (tx failPruneTx) PutResource(ctx context.Context, r record.Resource) error {
	if r.ArtifactsPrunedAt != nil {
		return errors.New("lost prune checkpoint")
	}
	return tx.Tx.PutResource(ctx, r)
}

func TestRetentionRetriesFailedEffectsAndLostPruneCheckpoints(t *testing.T) {
	t.Parallel()
	f, id := retainedFixture(t)
	f.provider.release = func(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
		return verify.ReleaseResult{}, errors.New("delete interrupted")
	}
	result, err := f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.False(t, result.Items[0].Completed)
	require.Contains(t, result.Items[0].Detail, "delete interrupted")
	require.Equal(t, record.ResourceUncertain, f.status(t, id).Resources[0].State)
	f.provider.release = nil
	f.advance(2 * time.Second)
	f.run(t, id) // The ordinary cycle can finish a release requested by gc.
	require.Equal(t, record.ResourceReleased, f.status(t, id).Resources[0].State)
	f.engine.Provider = pruningProvider{f.provider, func(context.Context, record.ResourceHandle) error { return errors.New("files busy") }}
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Contains(t, result.Items[0].Detail, "files busy")
	require.Nil(t, f.status(t, id).Resources[0].ArtifactsPrunedAt)
	f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
	f.engine.State = failPruneStore{f.scoped()}
	_, err = f.engine.Collect(t.Context(), retention.Options{})
	require.ErrorContains(t, err, "lost prune checkpoint")
	require.Nil(t, f.status(t, id).Resources[0].ArtifactsPrunedAt)
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	f.engine.State = state.Bind(reopened, f.registration)
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.True(t, result.Items[0].Completed)
	require.NotNil(t, f.status(t, id).Resources[0].ArtifactsPrunedAt)
}

func TestCompetingCollectorsUseReleaseClaimAndKeepRepositoryScope(t *testing.T) {
	t.Parallel()
	f, id := retainedFixture(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	defer release()
	f.provider.release = func(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
		close(entered)
		<-proceed
		return verify.ReleaseResult{Confirmed: true}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := f.engine.Collect(t.Context(), retention.Options{}); done <- err }()
	<-entered
	other, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer other.Close()
	engine := *f.engine
	engine.State = state.Bind(other, f.engine.State.Repository())
	result, err := engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	foreign, err := other.RegisterRepository(t.Context(), "/foreign/repository")
	require.NoError(t, err)
	engine.State = state.Bind(other, foreign)
	result, err = engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	release()
	require.NoError(t, <-done)
	require.Equal(t, 1, f.provider.count("release"))
	require.Equal(t, record.ResourceReleased, f.status(t, id).Resources[0].State)
}

func TestRetentionPaginatesWhileRemovingCandidates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
		result := admitted(r.ID)
		result.Resources = nil
		for i := range 70 {
			result.Resources = append(result.Resources, record.ResourceHandle{Provider: "scripted", ID: fmt.Sprintf("resource-%d", i)})
		}
		return result, nil
	}
	id := f.submit(t, "many-resources")
	f.run(t, id)
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.run(t, id)
	f.run(t, id)
	require.Len(t, f.status(t, id).Resources, 70)
	f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
	f.advance(8 * 24 * time.Hour)
	result, err := f.engine.Collect(t.Context(), retention.Options{OlderThan: 7 * 24 * time.Hour})
	require.NoError(t, err)
	require.Len(t, result.Items, 70)
	for _, item := range result.Items {
		require.True(t, item.Completed)
	}
	require.Equal(t, 70, f.provider.count("prune"))
	result, err = f.engine.Collect(t.Context(), retention.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestCycleReleasesFailureAndPrunesDiagnosticsAfterRetention(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.submit(t, "automatic-cleanup")
	f.run(t, id)
	f.provider.observe = terminal(f, record.VerdictFailed)
	f.run(t, id)
	before := f.status(t, id)
	require.Equal(t, record.ResourceReleased, before.Resources[0].State)
	require.NotNil(t, before.Jobs[0].Attempts[0].Evidence)
	f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
	f.advance(6 * 24 * time.Hour)
	f.run(t, id)
	require.Zero(t, f.provider.count("prune"))
	f.advance(2 * 24 * time.Hour)
	other, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer other.Close()
	restarted := *f.engine
	restarted.State = state.Bind(other, f.registration)
	_, err = restarted.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	after := f.status(t, id)
	require.NotNil(t, after.Resources[0].ArtifactsPrunedAt)
	require.Equal(t, before.Jobs, after.Jobs)
	require.Equal(t, before.Changes, after.Changes)
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("prune"))
}

func TestAutomaticPruningBacksOffWithoutLosingReleasedIdentity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.submit(t, "prune-retry")
	f.run(t, id)
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.run(t, id)
	f.advance(8 * 24 * time.Hour)
	f.engine.Provider = pruningProvider{f.provider, func(context.Context, record.ResourceHandle) error { return errors.New("directory busy") }}
	result := f.run(t, id)
	require.NotEmpty(t, result.Problems)
	resource := f.status(t, id).Resources[0]
	require.Equal(t, record.ResourceReleased, resource.State)
	require.Nil(t, resource.ArtifactsPrunedAt)
	require.NotNil(t, resource.RetryAt)
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("prune"))
	f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
	f.advance(25 * time.Hour)
	f.run(t, id)
	resource = f.status(t, id).Resources[0]
	require.NotNil(t, resource.ArtifactsPrunedAt)
	require.Nil(t, resource.RetryAt)
	require.Empty(t, resource.LastError)
}

func TestAutomaticPruningIsBoundedAndSkipsBuildOutputs(t *testing.T) {
	t.Parallel()
	for _, outputs := range []bool{false, true} {
		t.Run(fmt.Sprint(outputs), func(t *testing.T) {
			f := newFixture(t)
			f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
				result := admitted(r.ID)
				result.Resources = nil
				for i := range 17 {
					result.Resources = append(result.Resources, record.ResourceHandle{Provider: "scripted", ID: fmt.Sprintf("bounded-%d", i)})
				}
				return result, nil
			}
			id := f.submit(t, "bounded")
			f.run(t, id)
			observe := terminal(f, record.VerdictPassed)
			f.provider.observe = func(ctx context.Context, run record.ProviderRun) (verify.Observation, error) {
				v, err := observe(ctx, run)
				if outputs {
					v.Artifacts = []record.Artifact{{Name: "output", Location: "/keep/output"}}
				}
				return v, err
			}
			f.run(t, id)
			f.engine.Provider = pruningProvider{scriptedProvider: f.provider}
			f.advance(8 * 24 * time.Hour)
			for i := range 3 {
				f.run(t, id)
				want := min(17, (i+1)*8)
				if outputs {
					want = 0
				}
				require.Equal(t, want, f.provider.count("prune"))
			}
		})
	}
}

func TestKeepFailedSurvivesDriverRestartWithoutChangingEvidenceInputs(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	request := f.request("keep-failed")
	request.Spec.KeepFailed = true
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	other, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer other.Close()
	restarted := *f.engine
	restarted.State = state.Bind(other, f.registration)
	f.advance(2 * time.Second)
	f.provider.observe = terminal(f, record.VerdictFailed)
	_, err = restarted.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	status := f.status(t, receipt.JobID)
	require.True(t, status.Jobs[0].Job.Spec.KeepFailed)
	require.Equal(t, record.ResourceRetained, status.Resources[0].State)
	require.Equal(t, *request.Spec.Build, status.Jobs[0].Attempts[0].Spec.Config)
	f.advance(30 * 24 * time.Hour)
	f.run(t, receipt.JobID)
	require.Zero(t, f.provider.count("release"))
}
