package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

// fakeForge stands in for GitHub: the fork is a local bare repository, and
// pull requests live in memory.
type fakeForge struct {
	t        *testing.T
	upstream string
	fork     string
	prs      map[int]*record.PullRequest
	next     int
	others   []forge.PullRequestSummary
	created  []forge.PullRequestInput
	updated  []forge.PullRequestInput
	readied  []int
	// repos are other people's repositories, by name, as local paths.
	repos map[string]string
	// permission is the role Permission reports; reviews are those posted.
	permission  string
	reviews     []forge.ReviewInput
	rerequested []string
	// statuses are what Inspect reports, by number.
	statuses map[int]record.PullRequestStatus
}

func (f *fakeForge) AuthenticatedUser(context.Context) (string, error) { return "ada", nil }

func (f *fakeForge) NameFromRemote(url string) (string, error) {
	for name, path := range f.repos {
		if path == url {
			return name, nil
		}
	}
	switch url {
	case f.upstream:
		return UpstreamRepository, nil
	case f.fork:
		return "ada/macports-ports", nil
	}
	return "", errors.New("not a GitHub remote")
}

func (f *fakeForge) RepositoryInfo(_ context.Context, name string) (forge.RepositoryInfo, error) {
	if name == "ada/macports-ports" {
		return forge.RepositoryInfo{Name: name, DefaultBranch: "master", Parent: UpstreamRepository}, nil
	}
	return forge.RepositoryInfo{Name: name, DefaultBranch: "master"}, nil
}

// head is what GitHub reports as the pull request's head: the fork's branch.
func (f *fakeForge) head(branch string) record.ObjectID {
	out := run(f.t, f.fork, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch)
	return record.ObjectID(out)
}

func (f *fakeForge) observe(pr *record.PullRequest) forge.PullRequestObservation {
	copied := *pr
	copied.RemoteHead = f.head(pr.HeadBranch)
	if path, ok := f.repos[pr.HeadRepository]; ok {
		copied.RemoteHead = record.ObjectID(run(f.t, path, "for-each-ref", "--format=%(objectname)", "refs/heads/"+pr.HeadBranch))
	}
	return forge.PullRequestObservation{Found: true, PullRequest: copied}
}

func (f *fakeForge) Find(_ context.Context, q forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	for _, pr := range f.prs {
		if pr.HeadRepository == q.HeadRepository && pr.HeadBranch == q.HeadBranch {
			return f.observe(pr), nil
		}
	}
	return forge.PullRequestObservation{}, nil
}

func (f *fakeForge) Observe(_ context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	pr, ok := f.prs[ref.Number]
	if !ok {
		return forge.PullRequestObservation{}, forge.ErrNotFound
	}
	return f.observe(pr), nil
}

func (f *fakeForge) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	f.created = append(f.created, input)
	f.next++
	number := 34900 + f.next
	f.prs[number] = &record.PullRequest{Ref: record.PullRequestRef{Forge: forge.GitHub, Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch, State: record.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body}
	return f.observe(f.prs[number]), nil
}

func (f *fakeForge) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	f.updated = append(f.updated, input)
	pr := f.prs[input.ExistingPR.Number]
	pr.Title, pr.Body = input.Desired.Title, input.Desired.Body
	return f.observe(pr), nil
}

func (f *fakeForge) OpenPullRequests(context.Context, string, string) ([]forge.PullRequestSummary, error) {
	return f.others, nil
}

func (f *fakeForge) MarkReady(_ context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	f.readied = append(f.readied, ref.Number)
	return f.observe(f.prs[ref.Number]), nil
}

func (f *fakeForge) Permission(context.Context, string, string) (string, error) {
	return f.permission, nil
}

func (f *fakeForge) PostReview(_ context.Context, input forge.ReviewInput) (string, error) {
	f.reviews = append(f.reviews, input)
	return fmt.Sprintf("%s#pullrequestreview-%d", input.Ref.URL, len(f.reviews)), nil
}

func (f *fakeForge) RequestReviewers(_ context.Context, _ record.PullRequestRef, logins []string) error {
	f.rerequested = append(f.rerequested, logins...)
	return nil
}

func (f *fakeForge) Inspect(_ context.Context, ref record.PullRequestRef) (record.PullRequestStatus, error) {
	status, ok := f.statuses[ref.Number]
	if !ok {
		return record.PullRequestStatus{Review: "none"}, nil
	}
	return status, nil
}

// withFork gives the clone a fork remote and the engine a fake GitHub.
func (f fixture) withFork(t *testing.T, e *Engine) *fakeForge {
	fork := filepath.Join(filepath.Dir(f.upstream), "fork.git")
	run(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, fork)
	run(t, f.clone, "remote", "add", "fork", fork)
	fake := &fakeForge{t: t, upstream: f.upstream, fork: fork, prs: map[int]*record.PullRequest{}}
	e.Forge = fake
	return fake
}

// committedUpdate starts a branch, updates jq, and tidies it into a commit.
func committedUpdate(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	return branch
}

func TestSubmitWithoutACheckSaysSoAndOpensThePullRequest(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "no check has finished for this commit's files")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.ErrorContains(t, err, "can't submit yet")
	require.Empty(t, fake.created)

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"enhancement"}})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Equal(t, "jq: update to 1.8.1", plan.Title)
	require.Equal(t, "ada/macports-ports:dockhand/jq-update", plan.Head())
	require.Equal(t, []string{"jq"}, plan.Ports)
	require.False(t, plan.Replaces)
	require.Contains(t, plan.Body, "#### Description\n\n###### Type(s)\n\n- [ ] bugfix\n- [x] enhancement\n- [ ] security fix\n")
	require.Contains(t, plan.Body, "Not built locally: submitted with `dockhand submit --no-check`")
	require.Contains(t, plan.Body, "- [x] followed our [Commit Message Guidelines]")
	require.Contains(t, plan.Body, "- [x] checked that there aren't other open [pull requests]")
	require.Contains(t, plan.Body, "- [ ] checked your Portfile with `port lint`?")

	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.True(t, submitted.Pushed)
	require.Equal(t, 34901, submitted.PullRequest.Ref.Number)
	head := run(t, branch.Worktree, "rev-parse", "HEAD")
	require.Equal(t, record.ObjectID(head), fake.head("dockhand/jq-update"), "the fork has the commit")

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		recorded, err := r.Branch(branch.ID)
		require.NoError(t, err)
		require.Equal(t, 34901, recorded.PullRequest.Number)
		require.Equal(t, model.ObjectID(head), recorded.PullRequest.Pushed)
		require.Equal(t, plan.Body, recorded.PullRequest.Body)
		return nil
	}))
}

func TestSubmitUpdatesThePullRequestAndKeepsAPersonsDescription(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	first, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	branch, err = e.Resolve(t.Context(), "jq-update")
	require.NoError(t, err)

	// Review asks for a change; it is edited in and tidied into the commit.
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# reviewed\n"})
	tidy, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), tidy)
	require.NoError(t, err)

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"bugfix"}})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.NotNil(t, plan.Existing)
	require.True(t, plan.Replaces, "the tidied commit replaces the pushed one")
	require.False(t, plan.BodyKept)
	require.Contains(t, plan.Body, "- [ ] bugfix", "Type(s) is the person's part once the pull request exists")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Len(t, fake.created, 1)
	require.Empty(t, fake.updated, "the push is the update; the title and description had nothing new")
	require.Equal(t, record.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD")), fake.head("dockhand/jq-update"))

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1, reviewed"})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, first.PullRequest.Ref.Number, fake.updated[0].ExistingPR.Number)
	require.Equal(t, "jq: update to 1.8.1, reviewed", fake.prs[first.PullRequest.Ref.Number].Title, "--title retitles")

	// Someone edits the description on GitHub; dockhand leaves it be.
	fake.prs[first.PullRequest.Ref.Number].Body = "My own words.\n"
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# reviewed twice\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: note the second review")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.True(t, plan.BodyKept)
	require.Equal(t, "My own words.\n", plan.Body)
	require.False(t, plan.Replaces, "a new commit on top adds to the history")

	// And someone else pushes to the branch before this submit runs.
	other := filepath.Join(t.TempDir(), "other")
	run(t, t.TempDir(), "clone", "-q", "-b", "dockhand/jq-update", fake.fork, other)
	write(t, other, map[string]string{"README": "theirs\n"})
	run(t, other, "add", "README")
	run(t, other, "commit", "-q", "-m", "their change")
	run(t, other, "push", "-q", "origin", "dockhand/jq-update")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.ErrorIs(t, err, ErrStaleSubmit, "the push is conditional on the head submit saw")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "someone else pushed to #34901")
}

func TestSubmitNeedsCommittedWorkAndYourFork(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	fake := &fakeForge{t: t, upstream: f.upstream, prs: map[int]*record.PullRequest{}}
	e.Forge = fake

	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.ErrorContains(t, err, "these edits are not committed: textproc/jq/Portfile")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Head: true})
	require.ErrorContains(t, err, "has no commits above master yet")

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.ErrorContains(t, err, "no Git remote pushes to a fork of macports/macports-ports that ada owns")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"update"}})
	require.ErrorContains(t, err, `--type "update" is not one of the template's`)
}

// checked records a finished check of the branch's head tree: jq changed,
// harbor-viewer an extra from --also.
func checked(t *testing.T, e *Engine, branch model.Branch, jq, viewer model.Outcome) {
	t.Helper()
	head := model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD"))
	tree := model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD^{tree}"))
	at := time.Now().UTC().Truncate(time.Millisecond)
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "26", Architecture: "arm64"}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		revision := model.Revision{ID: model.RevisionID(store.NewID("rev")), Branch: branch.ID, Kind: model.RevisionCommit, Source: model.Source{Commit: head, Tree: tree, Base: branch.Base}, Head: head, CreatedAt: at}
		plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: []model.Environment{tahoe}, Tests: model.TestsDeclared, CreatedAt: at,
			Targets: []model.PlanTarget{
				{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed},
				{ID: "harbor-viewer", Target: model.Target{Name: "harbor-viewer"}, Directory: "graphics/harbor-viewer", Kind: model.Unchanged, Role: model.Also},
			}}
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		state := model.RunPassed
		if jq != model.OutcomePassed || viewer != model.OutcomePassed {
			state = model.RunFailed
		}
		runRecord := model.Run{ID: model.RunID(store.NewID("run")), Branch: branch.ID, Revision: revision.ID, Plan: plan.ID, Number: number, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at}
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID("ex")), Run: runRecord.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
		for _, step := range []func() error{
			func() error { return tx.AddRevision(revision) },
			func() error { return tx.AddPlan(plan) },
			func() error { return tx.AddRun(runRecord) },
			func() error { return tx.AddExecution(execution) },
			func() error {
				result := model.TargetResult{Execution: execution.ID, Target: "jq", Outcome: jq, Tests: model.TestsPassed, RecordedAt: at}
				if jq == model.OutcomeFailed {
					result.Phase, result.Tests = model.PhaseInstall, model.TestsNone
				}
				return tx.RecordResult(result)
			},
			func() error {
				result := model.TargetResult{Execution: execution.ID, Target: "harbor-viewer", Outcome: viewer, Tests: model.TestsNone, RecordedAt: at}
				if viewer == model.OutcomeFailed {
					result.Phase = model.PhaseInstall
				}
				return tx.RecordResult(result)
			},
		} {
			if err := step(); err != nil {
				return err
			}
		}
		for _, next := range []model.RunState{model.RunRunning, state} {
			runRecord.State = next
			if next == state {
				runRecord.FinishedAt = &at
			}
			if err := tx.UpdateRun(runRecord); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestSubmitFollowsThePublicationRule(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)

	checked(t, e, branch, model.OutcomePassed, model.OutcomeFailed)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "harbor-viewer (an extra from --also) did not pass in check-1; acknowledge it with --accept harbor-viewer")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}})
	require.ErrorContains(t, err, "it passed in check-1")

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"harbor-viewer"}})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Contains(t, plan.Body, "macOS 26 arm64\nDeveloper tools not recorded · tart: built in a clean VM\n")
	require.Contains(t, plan.Body, "| jq | ✓ |\n| harbor-viewer | ✗ failed, accepted: cause not established |\n")
	require.Contains(t, plan.Body, "- [x] tried a full install with `sudo port -vst install`? (dockhand builds from source as MacPorts CI does, without trace mode)")
	require.Contains(t, plan.Body, "- [x] tried existing tests with `sudo port test`?")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		accepted, err := r.Acceptances(branch.ID, model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD")))
		require.NoError(t, err)
		require.Len(t, accepted, 1)
		return nil
	}))
	require.Len(t, fake.created, 1)

	// A changed port that fails goes out only as a draft.
	g := setup(t)
	e2, _ := g.withPreparer(t)
	fake2 := g.withFork(t, e2)
	branch2 := committedUpdate(t, e2)
	checked(t, e2, branch2, model.OutcomeFailed, model.OutcomePassed)
	_, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2, Accept: []string{"jq"}})
	require.ErrorContains(t, err, "never accepted")
	plan, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2})
	require.NoError(t, err)
	require.Contains(t, strings.Join(plan.Blocking, "\n"), "jq did not pass in check-1; fix it, or share it as a draft (--draft)")
	plan, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2, Draft: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	_, err = e2.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.True(t, fake2.created[0].Draft)
}

func TestTheMergedDescriptionKeepsOnlyWhatDockhandWrote(t *testing.T) {
	fresh := "#### Description\n\nnew\n\n###### Tested on\n\nnew evidence\n"
	last := "#### Description\n\nold\n\n###### Tested on\n\nold evidence\n"
	merged, ok := mergeBody("#### Description\n\nmine\n\n###### Tested on\r\n\r\nold evidence\r\n", last, fresh)
	require.True(t, ok)
	require.Equal(t, "#### Description\n\nmine\n\n###### Tested on\n\nnew evidence\n", merged, "the description stays the person's")
	_, ok = mergeBody("#### Description\n\nmine\n\n###### Tested on\n\nI built it myself\n", last, fresh)
	require.False(t, ok)
}
