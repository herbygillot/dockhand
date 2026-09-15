package workflow_test

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
	"github.com/stretchr/testify/require"
)

func combinedFixture(t *testing.T, action record.Action) (*fixture, *publicationForge, workflow.Request) {
	t.Helper()
	f, request := preparationFixture(t, true)
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	out, err = exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "remote", "add", "origin", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: remote, Branch: "main", Commit: string(request.Spec.Source.Commit)}))
	hosting := &publicationForge{f: f, remote: remote}
	f.engine.Publisher = &publish.Service{Repo: f.repo, Forge: hosting, LockDirectory: filepath.Join(t.TempDir(), "locks")}
	destination, err := f.engine.Publisher.Destination(t.Context(), publish.Options{})
	require.NoError(t, err)
	request.Spec.Action = action
	request.Spec.PublishTo = &destination
	request.Spec.Destination = record.Published
	request.Spec.Build.VerifierDigest = "fixture:v1"
	if action == record.Bump {
		request.Spec.Version = "2.0"
		f.engine.Releases = resolveFunc(func(context.Context, preparation.Request) (record.Release, error) { return resolvedFixture(f), nil })
	}
	return f, hosting, request
}

func prepareCombined(t *testing.T, f *fixture, request workflow.Request) record.JobID {
	t.Helper()
	id := submitPreparation(t, f, request)
	if request.Spec.Action == record.Bump {
		f.run(t, id)
	}
	candidateJob(t, f, id)
	f.run(t, id)
	require.Equal(t, record.PhaseVerification, f.status(t, id).Jobs[0].Job.Phase)
	return id
}

func passCombined(t *testing.T, f *fixture, id record.JobID) {
	t.Helper()
	f.run(t, id)
	require.True(t, workflow.Reached(f.status(t, id), workflow.Admission))
	require.False(t, workflow.Reached(f.status(t, id), workflow.Completion))
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobActive, status.Jobs[0].Job.State)
	require.Equal(t, record.PhasePublication, status.Jobs[0].Job.Phase)
	require.Nil(t, status.Jobs[0].Job.FinishedAt)
	require.Empty(t, status.Jobs[0].Publications)
	require.Equal(t, record.ResourceReleased, status.Resources[0].State)
}

func TestCombinedPublicationKeepsOneJobAndResumesFrozenDestination(t *testing.T) {
	for _, action := range []record.Action{record.Bump, record.BumpRevision} {
		t.Run(string(action), func(t *testing.T) {
			f, hosting, request := combinedFixture(t, action)
			id := prepareCombined(t, f, request)
			passCombined(t, f, id)
			prepared := f.status(t, id).Jobs[0].Job.Prepared
			// Changing local remote configuration after acceptance must not redirect publication.
			out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "remote", "set-url", "origin", "/missing/other-remote").CombinedOutput()
			require.NoError(t, err, "%s", out)
			reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
			require.NoError(t, err)
			defer reopened.Close()
			engine := *f.engine
			engine.State = reopened
			engine.Preparer = nil
			engine.Releases = nil
			engine.Provider = nil
			f.engine = &engine
			f.run(t, id) // Freeze remote preconditions; no push in this pass.
			status := f.status(t, id)
			require.Len(t, status.Jobs[0].Publications, 1)
			publication := status.Jobs[0].Publications[0]
			require.Equal(t, *request.Spec.PublishTo, publication.Spec.Destination())
			require.Equal(t, prepared.Source.Commit, publication.Spec.Desired.Head)
			require.Equal(t, status.Jobs[0].Job.ResultRevision, publication.RevisionID)
			require.Equal(t, status.Jobs[0].Attempts[0].ID, publication.Spec.EvidenceAttempt)
			head, err := f.repo.RemoteHead(t.Context(), hosting.remote, prepared.Branch)
			require.NoError(t, err)
			require.False(t, head.Exists)
			f.run(t, id) // Push.
			hosting.writeErr = errors.New("response lost after PR creation")
			f.run(t, id) // One uncertain PR write.
			require.Equal(t, 1, hosting.writes)
			require.Equal(t, record.PublicationUncertain, f.status(t, id).Jobs[0].Publications[0].State)
			f.cancel(t, id) // Uncertainty still settles through observation.
			f.run(t, id)
			status = f.status(t, id)
			require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
			require.Equal(t, record.PublicationConfirmed, status.Jobs[0].Publications[0].State)
			require.Equal(t, request.Spec.Source, status.Jobs[0].Job.Spec.Source)
			require.Equal(t, status.Jobs[0].Job.ResultRevision, status.Changes[0].PublishedRevision)
			require.Equal(t, 1, hosting.writes)
			all, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
			require.NoError(t, err)
			require.Len(t, all.Jobs, 1)
			receipt, err := f.engine.Submit(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, id, receipt.JobID)
		})
	}
}

func TestCombinedPublicationStopsBeforeRemoteEffects(t *testing.T) {
	for _, scenario := range []string{"failed", "canceled", "moved", "deleted", "negative-before-plan", "negative-after-plan"} {
		t.Run(scenario, func(t *testing.T) {
			f, hosting, request := combinedFixture(t, record.BumpRevision)
			id := prepareCombined(t, f, request)
			if scenario == "failed" {
				f.run(t, id)
				f.provider.observe = terminal(f, record.VerdictFailed)
				f.run(t, id)
				require.Equal(t, record.JobFailed, f.status(t, id).Jobs[0].Job.State)
			} else {
				passCombined(t, f, id)
				if scenario == "negative-after-plan" {
					f.run(t, id)
				}
				job := f.status(t, id).Jobs[0].Job
				switch scenario {
				case "canceled":
					f.cancel(t, id)
				case "moved":
					commitPort(t, f, job.Prepared.Branch, "version 9\n")
				case "deleted":
					require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/" + job.Prepared.Branch, Expected: git.RefValue{Exists: true, Object: string(job.Prepared.Source.Commit)}}}))
				default:
					negative := reuseRequest(f, "new-negative")
					negative.Spec.Source = job.Prepared.Source
					negative.Spec.Targets = job.Spec.Targets
					negative.Spec.Build = job.Spec.Build
					negative.Spec.FreshVerification = true
					completeVerification(t, f, negative, record.VerdictFailed)
				}
				f.run(t, id)
				current := f.status(t, id).Jobs[0].Job
				if scenario == "canceled" {
					require.Equal(t, record.JobCanceled, current.State)
				} else if scenario == "moved" {
					require.Equal(t, record.JobSuperseded, current.State)
				} else {
					require.Equal(t, record.JobNeedsAttention, current.State)
				}
			}
			job := f.status(t, id).Jobs[0].Job
			head, err := f.repo.RemoteHead(t.Context(), hosting.remote, job.Prepared.Branch)
			require.NoError(t, err)
			require.False(t, head.Exists)
			require.Zero(t, hosting.writes)
		})
	}
}

func TestCombinedPublicationCanReuseEvidenceWithoutPretendingProviderAdmission(t *testing.T) {
	f, hosting, request := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, request)
	job := f.status(t, id).Jobs[0].Job
	verified := reuseRequest(f, "already-verified")
	verified.Spec.Source = job.Prepared.Source
	verified.Spec.Targets = job.Spec.Targets
	verified.Spec.Build = job.Spec.Build
	original := completeVerification(t, f, verified, record.VerdictPassed)
	f.engine.Provider = nil
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobActive, status.Jobs[0].Job.State)
	require.Equal(t, original.ID, status.Jobs[0].Job.ReusedAttempt)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Nil(t, status.Jobs[0].Job.AdmittedAt)
	require.True(t, workflow.Reached(status, workflow.Admission))
	require.False(t, workflow.Reached(status, workflow.Completion))
	for range 4 {
		f.run(t, id)
	}
	status = f.status(t, id)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Equal(t, original.ID, status.Jobs[0].Publications[0].Spec.EvidenceAttempt)
	require.Equal(t, 1, hosting.writes)
}

func TestCombinedPublicationClaimsPlanningAndRechecksCancellation(t *testing.T) {
	f, hosting, request := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, request)
	passCombined(t, f, id)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	defer release()
	hosting.onFind = func() { close(entered); <-proceed }
	done := make(chan error, 1)
	go func() { _, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}}); done <- err }()
	<-entered
	other, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer other.Close()
	engine := *f.engine
	engine.State = other
	require.NoError(t, engine.Control(t.Context(), record.ControlRequest{ID: "cancel-during-plan", Kind: record.Cancel, Jobs: []record.JobID{id}}))
	_, err = engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	release()
	require.NoError(t, <-done)
	status := f.status(t, id)
	require.Equal(t, record.JobCanceled, status.Jobs[0].Job.State)
	require.Empty(t, status.Jobs[0].Publications)
	require.Zero(t, hosting.writes)
}

type failPublicationStore struct{ state.Store }
type failPublicationTx struct{ state.Tx }

func (s failPublicationStore) Update(ctx context.Context, repo record.RepositoryID, fn func(context.Context, state.Tx) error) error {
	return s.Store.Update(ctx, repo, func(ctx context.Context, tx state.Tx) error { return fn(ctx, failPublicationTx{tx}) })
}
func (tx failPublicationTx) PutPublication(context.Context, record.PublicationAction) error {
	return errors.New("publication checkpoint failed")
}

func TestCombinedPublicationCannotPushBeforeCheckpointCommits(t *testing.T) {
	f, hosting, request := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, request)
	passCombined(t, f, id)
	f.engine.State = failPublicationStore{f.store}
	_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.ErrorContains(t, err, "publication checkpoint failed")
	require.Empty(t, f.status(t, id).Jobs[0].Publications)
	head, err := f.repo.RemoteHead(t.Context(), hosting.remote, f.status(t, id).Jobs[0].Job.Prepared.Branch)
	require.NoError(t, err)
	require.False(t, head.Exists)
	require.Zero(t, hosting.writes)
	f.engine.State = f.store
	f.advance(2 * time.Minute)
	for range 4 {
		f.run(t, id)
	}
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	require.Equal(t, 1, hosting.writes)
}

func TestCombinedPublicationRequiresVerifiedPreparationAndImmutableDestination(t *testing.T) {
	f, _, request := combinedFixture(t, record.BumpRevision)
	for _, mutate := range []func(*record.JobSpec){
		func(s *record.JobSpec) { s.PublishTo = nil },
		func(s *record.JobSpec) { s.Preparation = nil },
		func(s *record.JobSpec) { s.Verification = record.VerificationSkipped },
		func(s *record.JobSpec) { s.Destination = record.VerificationComplete },
		func(s *record.JobSpec) { s.Action = record.Verify },
		func(s *record.JobSpec) { s.Publication = &record.PublicationSpec{} },
		func(s *record.JobSpec) { s.PublishTo = &record.PublicationDestination{} },
		func(s *record.JobSpec) {
			s.BuildRequirements = &record.BuildRequirements{Provider: s.Build.Provider, Platform: s.Build.Platform, FromSource: s.Build.FromSource, Tests: s.Build.Tests}
		},
		func(s *record.JobSpec) { s.Build, s.BuildRequirements = nil, &record.BuildRequirements{} },
	} {
		invalid := request
		mutate(&invalid.Spec)
		_, err := f.engine.Submit(t.Context(), invalid)
		require.ErrorIs(t, err, workflow.ErrInvalidRequest)
	}
	id := submitPreparation(t, f, request)
	accepted := f.status(t, id).Jobs[0].Job
	request.Spec.PublishTo.BaseBranch = "redirected"
	_, err := f.engine.Submit(t.Context(), request)
	require.ErrorIs(t, err, workflow.ErrRequestConflict)
	require.Equal(t, "main", f.status(t, id).Jobs[0].Job.Spec.PublishTo.BaseBranch)
	accepted.Spec.PublishTo.BaseBranch = "redirected"
	err = f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error { return tx.PutJob(ctx, accepted) })
	require.ErrorIs(t, err, state.ErrConflict)
}

func TestAlreadyCurrentCombinedBumpCreatesNoPublication(t *testing.T) {
	f, hosting, request := combinedFixture(t, record.Bump)
	request.Spec.Version = ""
	release := resolvedFixture(f)
	release.Requested, release.CurrentVersion, release.NoUpdate = "", "2.0", true
	f.engine.Releases = resolveFunc(func(context.Context, preparation.Request) (record.Release, error) { return release, nil })
	id := submitPreparation(t, f, request)
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.True(t, status.Jobs[0].Job.ResolvedRelease.NoUpdate)
	require.Nil(t, status.Jobs[0].Job.Prepared)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Empty(t, status.Jobs[0].Publications)
	require.Empty(t, status.Changes)
	require.Zero(t, hosting.writes)
	require.Zero(t, f.provider.count("submit"))
}

func TestCombinedPublicationWithReusedEvidenceCanBeCanceled(t *testing.T) {
	f, hosting, request := combinedFixture(t, record.BumpRevision)
	id := prepareCombined(t, f, request)
	job := f.status(t, id).Jobs[0].Job
	verified := reuseRequest(f, "already-verified")
	verified.Spec.Source, verified.Spec.Targets, verified.Spec.Build = job.Prepared.Source, job.Spec.Targets, job.Spec.Build
	original := completeVerification(t, f, verified, record.VerdictPassed)
	f.run(t, id)
	f.cancel(t, id)
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobCanceled, status.Jobs[0].Job.State)
	require.Equal(t, original.ID, status.Jobs[0].Job.ReusedAttempt)
	require.Empty(t, status.Jobs[0].Publications)
	require.Zero(t, hosting.writes)
}
