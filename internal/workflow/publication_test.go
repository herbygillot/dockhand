package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

type publicationForge struct {
	f           *fixture
	remote      string
	observation forge.PullRequestObservation
	writes      int
	writeErr    error
}

func (p *publicationForge) Name() string { return "fixture" }
func (p *publicationForge) NameFromRemote(remote string) (string, error) {
	if remote != p.remote {
		return "", fmt.Errorf("unexpected remote")
	}
	return "author/ports", nil
}
func (p *publicationForge) RepositoryInfo(context.Context, string) (forge.RepositoryInfo, error) {
	return forge.RepositoryInfo{Name: "author/ports", DefaultBranch: "main", CloneURL: p.remote}, nil
}
func (p *publicationForge) Find(ctx context.Context, _ forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	// An independent write proves external calls hold no state transaction.
	if err := p.f.store.Update(ctx, p.f.repository, func(context.Context, state.Tx) error { return nil }); err != nil {
		return forge.PullRequestObservation{}, err
	}
	result := p.observation
	result.ObservedAt = p.f.now()
	if result.Found {
		head, err := p.f.repo.RemoteHead(ctx, p.remote, "candidate")
		if err != nil {
			return result, err
		}
		result.PullRequest.RemoteHead = record.ObjectID(head.Object)
		result.PullRequest.ObservedAt = result.ObservedAt
	}
	return result, nil
}
func (p *publicationForge) Observe(ctx context.Context, _ record.PullRequestRef) (forge.PullRequestObservation, error) {
	return p.Find(ctx, forge.PullRequestQuery{})
}
func (p *publicationForge) Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	p.writes++
	if errors.Is(p.writeErr, forge.ErrRejected) {
		return forge.PullRequestObservation{}, p.writeErr
	}
	p.observation = forge.PullRequestObservation{Found: true, ObservedAt: p.f.now(), PullRequest: record.PullRequest{Ref: record.PullRequestRef{Forge: "fixture", Repository: input.Repository, Number: 1, URL: "https://example.invalid/pr/1"}, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch, State: record.PullRequestOpen, RemoteHead: input.Desired.Head, Title: input.Desired.Title, Body: input.Desired.Body, ObservedAt: p.f.now()}}
	return p.observation, p.writeErr
}
func (p *publicationForge) Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return p.Create(ctx, input)
}

func publicationFixture(t *testing.T) (*fixture, *publicationForge) {
	t.Helper()
	f, _ := bindingFixture(t)
	base, _, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	tree := commitPort(t, f, "candidate", "version 2\n").Tree
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(tree), Parents: []string{base}, Message: "fixture: update to 2\n\nContribution details", Author: sig, Committer: sig})
	require.NoError(t, err)
	prior, err := f.repo.ReadRef(t.Context(), "refs/heads/candidate")
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: prior, Desired: git.RefValue{Exists: true, Object: commit}}}))
	f.source = record.Source{Commit: record.ObjectID(commit), Tree: tree, Base: record.ObjectID(base)}
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Branch, change.CurrentRevision, change.Targets = "candidate", "publication_revision", []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
		if err := tx.PutRevision(ctx, record.Revision{ID: change.CurrentRevision, ChangeID: change.ID, Previous: "revision", Source: f.source, CreatedAt: f.now()}); err != nil {
			return err
		}
		return tx.PutChange(ctx, change)
	}))
	request := reuseRequest(f, "passed")
	request.Spec.Targets = []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
	completeVerification(t, f, request, record.VerdictPassed)
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	command := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", remote)
	command.Dir = f.repo.Root
	out, err = command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: remote, Branch: "main", Commit: base}))
	provider := &publicationForge{f: f, remote: remote}
	f.engine.Publisher = &publish.Service{Repo: f.repo, Forge: provider, LockDirectory: filepath.Join(t.TempDir(), "locks")}
	f.engine.Provider = nil
	return f, provider
}
func bindPublication(t *testing.T, f *fixture, id string) workflow.Request {
	t.Helper()
	request, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: record.RequestID(id), Branch: "candidate"})
	require.NoError(t, err)
	return request
}
func submitPublication(t *testing.T, f *fixture, id string) record.JobID {
	t.Helper()
	receipt, err := f.engine.Submit(t.Context(), bindPublication(t, f, id))
	require.NoError(t, err)
	return receipt.JobID
}

func TestPublicationPushesConfirmsAndRetainsAssociationAcrossRestart(t *testing.T) {
	f, hosting := publicationFixture(t)
	request := bindPublication(t, f, "publish")
	require.Equal(t, "Contribution details", request.Spec.Publication.Desired.Body)
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	retry, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
	id := receipt.JobID
	f.run(t, id)
	require.Equal(t, 0, hosting.writes)
	require.True(t, f.status(t, id).Jobs[0].Publications[0].PushStarted)
	require.True(t, workflow.Reached(f.status(t, id), workflow.Admission))
	require.False(t, workflow.Reached(f.status(t, id), workflow.Completion))
	f.run(t, id)
	require.Equal(t, 1, hosting.writes)
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	engine := *f.engine
	engine.State = reopened
	f.engine = &engine
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Len(t, status.PullRequests, 1)
	require.Equal(t, record.PublicationConfirmed, status.Jobs[0].Publications[0].State)
	require.Equal(t, status.Jobs[0].Job.Spec.InputRevision, status.Changes[0].PublishedRevision)
	require.Equal(t, status.PullRequests[0].ID, status.Changes[0].PullRequestID)
	require.Equal(t, 2, f.provider.count("capabilities")) // Only the original verification used the provider.
	again := submitPublication(t, f, "repeat")
	f.run(t, again)
	require.Equal(t, record.JobCompleted, f.status(t, again).Jobs[0].Job.State)
	require.Equal(t, 1, hosting.writes)
}

func TestPublicationRecoversLostPRResponseWithoutAnotherWrite(t *testing.T) {
	f, hosting := publicationFixture(t)
	hosting.writeErr = errors.New("connection lost after server accepted PR")
	id := submitPublication(t, f, "publish")
	f.run(t, id)
	f.run(t, id)
	require.Equal(t, record.PublicationUncertain, f.status(t, id).Jobs[0].Publications[0].State)
	f.cancel(t, id)
	f.run(t, id)
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	require.Equal(t, 1, hosting.writes)
}

func TestPublicationUnknownRequestIsObservationOnlyEvenAfterCancellation(t *testing.T) {
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "publish")
	f.run(t, id)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		action, err := tx.PublicationForJob(ctx, id)
		if err != nil {
			return err
		}
		action.WriteStarted = true
		action.State = record.PublicationUncertain
		return tx.PutPublication(ctx, action)
	}))
	f.cancel(t, id)
	f.run(t, id)
	f.run(t, id)
	require.Equal(t, 0, hosting.writes)
	require.Equal(t, record.JobActive, f.status(t, id).Jobs[0].Job.State)
	_, err := f.engine.Submit(t.Context(), bindPublication(t, f, "duplicate"))
	require.ErrorIs(t, err, state.ErrConflict)
}

func TestPublicationRejectsMovedSourceRemoteAndNewNegativeEvidence(t *testing.T) {
	for _, mode := range []string{"source", "remote", "verification", "cancel", "cancel missing branch", "PR"} {
		t.Run(mode, func(t *testing.T) {
			f, hosting := publicationFixture(t)
			id := submitPublication(t, f, "publish")
			switch mode {
			case "source":
				commitPort(t, f, "candidate", "unverified changes")
			case "remote":
				require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "candidate", Commit: string(f.source.Base)}))
			case "verification":
				request := reuseRequest(f, "new-failure")
				request.Spec.Targets = []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
				request.Spec.FreshVerification = true
				f.engine.Provider = f.provider
				completeVerification(t, f, request, record.VerdictFailed)
				f.engine.Provider = nil
			case "cancel", "cancel missing branch":
				f.cancel(t, id)
				if mode == "cancel missing branch" {
					require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}}}))
				}
			case "PR":
				hosting.observation = forge.PullRequestObservation{Found: true, PullRequest: record.PullRequest{Ref: record.PullRequestRef{Forge: "fixture", Repository: "author/ports", Number: 1, URL: "https://example.invalid/pr/1"}, HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", State: record.PullRequestOpen, Title: "Human PR", Body: "human edits"}}
				require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "candidate", Commit: string(f.source.Commit)}))
			}
			f.run(t, id)
			job := f.status(t, id).Jobs[0].Job
			require.Contains(t, []record.JobState{record.JobNeedsAttention, record.JobSuperseded, record.JobCanceled}, job.State)
			if strings.HasPrefix(mode, "cancel") {
				require.Equal(t, record.JobCanceled, job.State)
			}
			require.Equal(t, 0, hosting.writes)
		})
	}
}

func TestPublicationConcurrentAcceptanceAndRepositoryScope(t *testing.T) {
	f, _ := publicationFixture(t)
	first, second := bindPublication(t, f, "one"), bindPublication(t, f, "two")
	var receipts [2]workflow.Receipt
	var errs [2]error
	var wg sync.WaitGroup
	for i, request := range []workflow.Request{first, second} {
		wg.Add(1)
		go func() { defer wg.Done(); receipts[i], errs[i] = f.engine.Submit(t.Context(), request) }()
	}
	wg.Wait()
	if errs[0] != nil {
		receipts[0], receipts[1] = receipts[1], receipts[0]
		errs[0], errs[1] = errs[1], errs[0]
	}
	require.NoError(t, errs[0])
	require.ErrorIs(t, errs[1], state.ErrConflict)
	other, err := f.store.RegisterRepository(t.Context(), filepath.Join(t.TempDir(), ".git"))
	require.NoError(t, err)
	require.NoError(t, f.store.View(t.Context(), other.ID, func(ctx context.Context, r state.Reader) error {
		_, err := r.PublicationForJob(ctx, receipts[0].JobID)
		require.ErrorIs(t, err, state.ErrNotFound)
		return nil
	}))
}

func TestPublicationWaiterRechecksCancellationUnderLock(t *testing.T) {
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "publish")
	done := make(chan error, 1)
	require.NoError(t, f.repo.WithPushLock(t.Context(), f.engine.Publisher.LockDirectory, "fixture:author/ports:candidate", func(context.Context) error {
		go func() { _, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}}); done <- err }()
		require.Eventually(t, func() bool { return f.status(t, id).Jobs[0].Job.Claim != nil }, time.Second, 10*time.Millisecond)
		return f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
			job, err := tx.Job(ctx, id)
			if err != nil {
				return err
			}
			now := f.now()
			job.CancelRequestedAt = &now
			return tx.PutJob(ctx, job)
		})
	}))
	require.NoError(t, <-done)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
	require.Equal(t, 0, hosting.writes)
	remote, err := f.repo.RemoteHead(t.Context(), hosting.remote, "candidate")
	require.NoError(t, err)
	require.False(t, remote.Exists)
}

func TestPublicationUpdatesCorrectedCommitAndPreservesHumanPRBody(t *testing.T) {
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "first")
	f.run(t, id)
	f.run(t, id)
	f.run(t, id)
	first := f.status(t, id)
	hosting.observation.PullRequest.Body = "Human review context to preserve"
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.source.Tree), Parents: []string{string(f.source.Base)}, Message: "fixture: corrected title\n\nCommit-specific notes", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	request := bindPublication(t, f, "correction")
	require.Equal(t, "Human review context to preserve", request.Spec.Publication.Desired.Body)
	require.Equal(t, "fixture: corrected title", request.Spec.Publication.Desired.Title)
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	f.run(t, receipt.JobID)
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Equal(t, first.PullRequests[0].ID, status.PullRequests[0].ID)
	require.Equal(t, "Human review context to preserve", status.PullRequests[0].Body)
	require.Equal(t, record.ObjectID(commit), status.PullRequests[0].RemoteHead)
	require.NotEqual(t, first.Changes[0].PublishedRevision, status.Changes[0].PublishedRevision)
	require.Equal(t, 2, hosting.writes)
}

func TestPublicationDefinitiveRejectionSettlesAndAllowsANewRequest(t *testing.T) {
	f, hosting := publicationFixture(t)
	hosting.writeErr = fmt.Errorf("%w: credentials rejected", forge.ErrRejected)
	id := submitPublication(t, f, "denied")
	f.run(t, id)
	f.run(t, id)
	require.Equal(t, record.JobNeedsAttention, f.status(t, id).Jobs[0].Job.State)
	hosting.writeErr = nil
	next := submitPublication(t, f, "retry-with-credentials")
	f.run(t, next)
	f.run(t, next)
	require.Equal(t, record.JobCompleted, f.status(t, next).Jobs[0].Job.State)
	require.Equal(t, 2, hosting.writes)
}

func TestCompetingPublicationDriversUseOneWrite(t *testing.T) {
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "publication")
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	other := *f.engine
	other.State = reopened
	other.Owner = "other-driver"
	for range 3 {
		f.advance(2 * time.Second)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, engine := range []*workflow.Engine{f.engine, &other} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
	}
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	require.Equal(t, 1, hosting.writes)
}
