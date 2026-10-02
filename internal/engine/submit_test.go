package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// fakeForge stands in for GitHub: the fork is a local bare repository, and
// pull requests live in memory.
type fakeForge struct {
	t        *testing.T
	upstream string
	fork     string
	prs      map[int]*forge.PullRequest
	next     int
	// others are the open pull requests a search for a port finds, and
	// searchErr why it fails; searches counts them.
	others    []forge.PullRequestSummary
	searchErr error
	searches  int
	created   []forge.PullRequestInput
	updated   []forge.PullRequestInput
	readied   []int
	// readyRefused is GitHub's refusal to mark a pull request ready.
	readyRefused error
	// repos are other people's repositories, by name, as local paths.
	repos map[string]string
	// permission is the role Permission reports; reviews are those posted.
	permission  string
	reviews     []forge.ReviewInput
	rerequested []string
	// statuses are what Inspect reports, by number.
	statuses map[int]forge.PullRequestStatus
	// createFails fails the next Create: after opening the pull request,
	// as a reply lost on the way back, when lost is set, or before.
	createFails, lost bool
	// dockhand is dockhand's own repository as GitHub has it: its commits
	// and tags, why asking it fails, and what was asked of it.
	dockhand fakeDockhand
}

// fakeDockhand is dockhand's own repository on GitHub.
type fakeDockhand struct {
	name    string
	commits []string
	tags    []string
	err     error
	asked   []string
}

func (f *fakeForge) Repository(_, name string) (forge.Repository, error) {
	f.dockhand.name = name
	return &f.dockhand, nil
}

func (d *fakeDockhand) Name() string { return d.name }

func (d *fakeDockhand) Tag(_ context.Context, name string) (forge.Tag, error) {
	d.asked = append(d.asked, name)
	switch {
	case d.err != nil:
		return forge.Tag{}, d.err
	case slices.Contains(d.tags, name):
		return forge.Tag{Name: name, Commit: strings.Repeat("d", 40)}, nil
	}
	return forge.Tag{}, forge.ErrNotFound
}

func (d *fakeDockhand) ListTags(context.Context) ([]forge.Tag, error) {
	return nil, errors.New("dockhand's tags aren't listed")
}

func (d *fakeDockhand) HasCommit(_ context.Context, commit string) (bool, error) {
	d.asked = append(d.asked, commit)
	if d.err != nil {
		return false, d.err
	}
	return slices.ContainsFunc(d.commits, func(c string) bool { return strings.HasPrefix(c, commit) }), nil
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
func (f *fakeForge) head(branch string) model.ObjectID {
	out := run(f.t, f.fork, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch)
	return model.ObjectID(out)
}

func (f *fakeForge) observe(pr *forge.PullRequest) forge.PullRequestObservation {
	copied := *pr
	copied.RemoteHead = f.head(pr.HeadBranch)
	if path, ok := f.repos[pr.HeadRepository]; ok {
		copied.RemoteHead = model.ObjectID(run(f.t, path, "for-each-ref", "--format=%(objectname)", "refs/heads/"+pr.HeadBranch))
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

func (f *fakeForge) Observe(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	pr, ok := f.prs[ref.Number]
	if !ok {
		return forge.PullRequestObservation{}, forge.ErrNotFound
	}
	return f.observe(pr), nil
}

func (f *fakeForge) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if f.createFails && !f.lost {
		f.createFails = false
		return forge.PullRequestObservation{}, errors.New("github: connection reset before the request was sent")
	}
	// GitHub refuses a second pull request from one head to one base.
	for _, pr := range f.prs {
		if pr.HeadRepository == input.HeadRepository && pr.HeadBranch == input.HeadBranch && pr.BaseBranch == input.BaseBranch && pr.State == forge.PullRequestOpen {
			return forge.PullRequestObservation{}, fmt.Errorf("%w: a pull request already exists for %s:%s", forge.ErrRejected, input.HeadRepository, input.HeadBranch)
		}
	}
	f.created = append(f.created, input)
	f.next++
	number := 34900 + f.next
	f.prs[number] = &forge.PullRequest{Ref: forge.PullRequestRef{Forge: forge.GitHub, Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch, State: forge.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body}
	if f.createFails {
		f.createFails = false
		return forge.PullRequestObservation{}, errors.New("github: connection reset after the request was sent")
	}
	return f.observe(f.prs[number]), nil
}

func (f *fakeForge) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	f.updated = append(f.updated, input)
	pr := f.prs[input.ExistingPR.Number]
	pr.Title, pr.Body = input.Desired.Title, input.Desired.Body
	return f.observe(pr), nil
}

func (f *fakeForge) OpenPullRequests(context.Context, string, string) ([]forge.PullRequestSummary, error) {
	f.searches++
	return f.others, f.searchErr
}

func (f *fakeForge) MarkReady(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if f.readyRefused != nil {
		return forge.PullRequestObservation{}, f.readyRefused
	}
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

func (f *fakeForge) RequestReviewers(_ context.Context, _ forge.PullRequestRef, logins []string) error {
	f.rerequested = append(f.rerequested, logins...)
	return nil
}

func (f *fakeForge) Inspect(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestStatus, error) {
	status, ok := f.statuses[ref.Number]
	if !ok {
		return forge.PullRequestStatus{Review: "none"}, nil
	}
	return status, nil
}

// withFork gives the clone a fork remote and the engine a fake GitHub.
func (f fixture) withFork(t *testing.T, e *Engine) *fakeForge {
	fork := filepath.Join(filepath.Dir(f.upstream), "fork.git")
	run(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, fork)
	run(t, f.clone, "remote", "add", "fork", fork)
	fake := &fakeForge{t: t, upstream: f.upstream, fork: fork, prs: map[int]*forge.PullRequest{}}
	e.Forge = fake
	return fake
}

// committedUpdate starts a branch, updates jq, and tidies it into a commit.
func committedUpdate(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
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
	require.Equal(t, model.ObjectID(head), fake.head("dockhand/jq-update"), "the fork has the commit")

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		recorded, err := r.Branch(branch.ID)
		require.NoError(t, err)
		require.Equal(t, 34901, recorded.PullRequest.Number)
		require.Equal(t, model.ObjectID(head), recorded.PullRequest.Pushed)
		require.Equal(t, plan.Body, recorded.PullRequest.Body)
		return nil
	}))
}

func TestAnUpdateDockhandMadeIsAnEnhancement(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "###### Type(s)\n\n- [ ] bugfix\n- [x] enhancement\n- [ ] security fix\n")

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"bugfix"}})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "###### Type(s)\n\n- [x] bugfix\n- [ ] enhancement\n- [ ] security fix\n", "--type says what it is")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# a person's change\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: a person's change")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n", "a commit dockhand didn't write is the person's to type")
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

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.NotNil(t, plan.Existing)
	require.True(t, plan.Replaces, "the tidied commit replaces the pushed one")
	require.False(t, plan.BodyKept)
	require.Equal(t, DescriptionSections{Description: SectionCurrent, Types: SectionCurrent, TestedOn: SectionCurrent}, plan.Sections)
	// --type names the Type(s) of a pull request already open, as of a new
	// one: the person named them.
	typed, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"bugfix"}})
	require.NoError(t, err)
	require.Contains(t, typed.Body, "- [x] bugfix")
	require.Equal(t, DescriptionSections{Description: SectionCurrent, Types: SectionRefreshed, TestedOn: SectionCurrent}, typed.Sections)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Len(t, fake.created, 1)
	require.Empty(t, fake.updated, "the push is the update; the title and description had nothing new")
	require.Equal(t, model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD")), fake.head("dockhand/jq-update"))

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
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	fake := &fakeForge{t: t, upstream: f.upstream, prs: map[int]*forge.PullRequest{}}
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
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		revision := model.Revision{ID: model.RevisionID(store.NewID("rev")), Branch: branch.ID, Kind: model.RevisionCommit, Source: model.Source{Commit: head, Tree: tree, Base: branch.Base}, Head: head, CreatedAt: at}
		plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: []model.Environment{tahoe}, Tests: model.TestsDeclared, CreatedAt: at,
			Targets: []model.PlanTarget{
				{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed},
				{ID: "harbor-viewer", Target: model.Target{Name: "harbor-viewer"}, Directory: "graphics/harbor-viewer", Kind: model.Unchanged, Role: model.Also},
			},
			Builds: []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"jq", "harbor-viewer"}}}}
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		state := model.RunPassed
		switch {
		case jq != model.OutcomePassed || viewer != model.OutcomePassed && viewer != "":
			state = model.RunFailed
		case viewer == "":
			state = model.RunAttention
		}
		runRecord := model.Run{ID: model.RunID(store.NewID("run")), Branch: branch.ID, Revision: revision.ID, Plan: plan.ID, Number: number, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at}
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID("tart")), Run: runRecord.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at,
			Observed: model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", Tools: "26.6.0.0.1781586589"}}
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
				if viewer == "" {
					return nil // the check didn't reach it
				}
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
	require.Regexp(t, `###### Tested on\n\nmacOS 26\.6\.2 25G71 arm64\nXcode 26\.6 17F42 · tart: built in a clean VM \(Run ID: tart_[a-z0-9]{16} - checked in check-1\)\n\n\| Port \| macOS 26 \|\n`, plan.Body,
		"what the guest reported, in the template's words, and the run and check it was in")
	require.NotContains(t, plan.Body, "Checked by dockhand", "each run names its check")
	require.True(t, strings.HasPrefix(plan.Body, "Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**\n\n#### Description\n\n"), "dockhand's line opens the description")
	require.Regexp(t, "\n\n- \\[dockhand\\]\\(https://github\\.com/herbygillot/dockhand\\) ver\\. \\S+\n$", plan.Body, "and its version closes it")
	require.Regexp(t, "\n- \\[ \\] checked that the Portfile's most important .+\n\n<!-- dockhand -->\n\n- \\[dockhand\\]", plan.Body,
		"a comment GitHub doesn't show ends the checklist, so dockhand's line isn't taken for its last item")
	require.Contains(t, plan.Body, "| jq | ✓ tests passed |\n| harbor-viewer | ✗ failed at install, accepted: cause not established |\n")
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

	// An extra no check reached asks nothing, and there is nothing of it
	// to accept (TargetEvidence.Extra).
	h := setup(t)
	e3, _ := h.withPreparer(t)
	h.withFork(t, e3)
	branch3 := committedUpdate(t, e3)
	checked(t, e3, branch3, model.OutcomePassed, "")
	plan, err = e3.PlanSubmit(t.Context(), SubmitRequest{Branch: branch3})
	require.NoError(t, err)
	require.NotContains(t, strings.Join(plan.Blocking, "\n"), "harbor-viewer")
	_, err = e3.PlanSubmit(t.Context(), SubmitRequest{Branch: branch3, Accept: []string{"harbor-viewer"}})
	require.ErrorContains(t, err, "--accept harbor-viewer: no check of these files built it, so there is no failure to accept")

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

// Someone's repository is pushed to by a remote that already pushes there,
// else at its GitHub address, over SSH where your remotes push over it.
func TestTheirRemoteIsOneThatPushesThereOrTheirAddress(t *testing.T) {
	e := &Engine{Forge: &fakeForge{t: t, repos: map[string]string{"bo/macports-ports": "/remotes/bo"}}}
	remotes := []git.Remote{{Name: "fork", PushURL: "git@github.com:ada/macports-ports.git"}}
	name, push, err := e.theirRemote(t.Context(), remotes, "bo/macports-ports")
	require.NoError(t, err)
	require.Equal(t, "git@github.com:bo/macports-ports.git", name)
	require.Equal(t, name, push)
	remotes[0].PushURL = "https://github.com/ada/macports-ports.git"
	name, push, err = e.theirRemote(t.Context(), remotes, "bo/macports-ports")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/bo/macports-ports.git", name)
	require.Equal(t, name, push)
	remotes = append(remotes, git.Remote{Name: "bo", PushURL: "/remotes/bo"})
	name, push, err = e.theirRemote(t.Context(), remotes, "bo/macports-ports")
	require.NoError(t, err)
	require.Equal(t, "bo", name)
	require.Equal(t, "/remotes/bo", push)
}

// restricted is GitHub's refusal of an app an organization hasn't
// approved, as the forge's client reports it.
type restricted struct{ error }

func (restricted) Is(target error) bool { return target == forge.ErrAppRestricted }

// signedInCLI is a GitHub CLI signed in as login, recording what it marks
// ready.
type signedInCLI struct {
	login   string
	readied []int
}

func (c *signedInCLI) Login(context.Context) (string, error) { return c.login, nil }

func (c *signedInCLI) MarkReady(_ context.Context, ref forge.PullRequestRef) error {
	c.readied = append(c.readied, ref.Number)
	return nil
}

// Where an organization refuses dockhand's app, the GitHub CLI marks the
// draft ready when it's signed in as dockhand is, and the journal says it
// did; with none, the refusal says what to do (D8).
func TestAReadyTheOrganizationRefusesGoesThroughTheGitHubCLI(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	fake.readyRefused = restricted{errors.New("github: the `macports` organization has enabled OAuth App access restrictions")}

	_, byCLI, err := e.Ready(t.Context(), branch)
	require.ErrorIs(t, err, forge.ErrAppRestricted)
	require.False(t, byCLI)
	require.NotContains(t, err.Error(), "GitHub CLI wasn't used", "with no CLI, nothing more to say")

	cli := &signedInCLI{login: "ada"}
	e.GitHubCLI = cli
	readied, byCLI, err := e.Ready(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, byCLI)
	require.False(t, readied.PullRequest.Draft)
	require.Equal(t, []int{readied.PullRequest.Number}, cli.readied)
	events, err := e.Events(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("marked #%d ready for review with the GitHub CLI", readied.PullRequest.Number), events[len(events)-1].Message)
}

// A submission waiting on a check of its files that hasn't finished names
// it and the wait, not a new check to run (the sshuttle run).
func TestASubmissionNamesTheCheckItWaitsOn(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	checkPlan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, checkPlan, model.OriginPerson)
	require.NoError(t, err)

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{queued.Name() + ", of this commit's files, is queued; dockhand wait " + queued.Name() + ", then submit, or submit a draft with --draft"}, plan.Blocking)
}

// The search for a port's other open pull requests leaves out the branch's
// own, and finds each once however many of the ports it's for.
func TestOtherOpenPullRequestsLeaveOutTheBranchsOwn(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.others = []forge.PullRequestSummary{{Number: 35044, Title: "flatbuffers: update"}, {Number: 34620, Title: "libuv: update"}}
	others, problem := e.openPullRequests(t.Context(), []string{"flatbuffers", "libsigmf"}, 35044)
	require.Empty(t, problem)
	require.Equal(t, []forge.PullRequestSummary{{Number: 34620, Title: "libuv: update"}}, others)
}

// A Portfile the branch adds, which its base doesn't have, is a new port,
// said in the pull request as the submitted files evaluate it; a port the
// base already has is none (the txt run's finding 4).
func TestSubmitSaysTheNewPortsTheBranchAdds(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	write(t, branch.Worktree, map[string]string{"devel/harbor/Portfile": "name harbor\nversion 1.0\n"})
	run(t, branch.Worktree, "add", "--sparse", "devel/harbor/Portfile")
	run(t, branch.Worktree, "commit", "-q", "-m", "harbor: new port, version 1.0")
	harbor := macports.PortInfo{Name: "harbor", Version: "1.0", Options: map[string]string{"description": "{Harbor tools for the command line}", "homepage": "https://harbor.example/", "license": "{MIT Apache-2}"}}
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"devel/harbor": {harbor}, "textproc/jq": {port("jq")}}}
	checked(t, e, branch, model.OutcomePassed, model.OutcomePassed)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq: update to 1.8.1; harbor: new port"})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "#### Description\n\nNew port **harbor** 1.0: Harbor tools for the command line\n\n- homepage: https://harbor.example/\n- license: MIT or Apache-2\n\n")
	require.NotContains(t, plan.Body, "New port **jq**", "jq is in the base")
}

// A person's note is the branch's, and the description gives it under
// Description each time dockhand writes it: submitting again keeps it,
// --note replaces it, which counts as a change to the pull request, and an
// empty one takes it out. dockhand writes the whole description, so a
// person had no way to say in it why rust's tests failed (the rust run,
// #35084). A plan records nothing; a Description a person edited on
// GitHub stays theirs, and the plan says the note isn't in it.
func TestANoteIsKeptInTheDescription(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	recordedNote := func() string {
		t.Helper()
		var note string
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			recorded, err := r.Branch(branch.ID)
			note = recorded.Note
			return err
		}))
		return note
	}

	note := "  rust's tests didn't run: bootstrap panicked on a permission error.\r\n\r\nThe environment's, not the tests'.\n"
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new(note)})
	require.NoError(t, err)
	quoted := "> **Author's note:** rust's tests didn't run: bootstrap panicked on a permission error.\n>\n> The environment's, not the tests'.\n\n"
	require.Contains(t, plan.Body, "#### Description\n\n"+quoted+"###### Type(s)\n")
	require.Empty(t, recordedNote(), "a plan records nothing")
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.Equal(t, "rust's tests didn't run: bootstrap panicked on a permission error.\n\nThe environment's, not the tests'.", recordedNote())

	// Submitting again, without --note, keeps it.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, quoted)
	require.Equal(t, DescriptionSections{Description: SectionCurrent, Types: SectionCurrent, TestedOn: SectionCurrent}, plan.Sections)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Empty(t, fake.updated, "nothing new")

	// A new note replaces it, and the pull request is updated for it alone.
	// One with a line that would begin a heading is quoted, and so stays
	// within the Description when replaced again.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream in\n#35090.")})
	require.NoError(t, err)
	require.Equal(t, SectionRefreshed, plan.Sections.Description)
	require.Contains(t, plan.Body, "#### Description\n\n> **Author's note:** Fixed upstream in\n> #35090.\n\n###### Type(s)\n")
	require.NotContains(t, plan.Body, "permission error")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Len(t, fake.updated, 1, "the note's change is the pull request's")
	require.Equal(t, plan.Body, fake.prs[number].Body)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream.")})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "#### Description\n\n> **Author's note:** Fixed upstream.\n\n###### Type(s)\n")
	require.NotContains(t, plan.Body, "#35090")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)

	// An empty note takes it out.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("")})
	require.NoError(t, err)
	require.Equal(t, SectionRefreshed, plan.Sections.Description)
	require.Contains(t, plan.Body, "#### Description\n\n###### Type(s)\n")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Len(t, fake.updated, 3)
	require.NotContains(t, fake.prs[number].Body, "Author's note")
	require.Empty(t, recordedNote())

	// A Description a person edited on GitHub is theirs, and the plan says
	// the note isn't in it.
	fake.prs[number].Body = strings.Replace(fake.prs[number].Body, "#### Description\n\n", "#### Description\n\nMy own words.\n\n", 1)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream.")})
	require.NoError(t, err)
	require.Equal(t, SectionKept, plan.Sections.Description)
	require.True(t, plan.NoteLeftOut)
	require.NotContains(t, plan.Body, "Author's note")
}
