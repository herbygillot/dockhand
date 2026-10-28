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

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// withFork gives the clone a fork remote and the engine a fake GitHub.
func (f fixture) withFork(t *testing.T, e *Engine) *forgetest.GitHub {
	fork := filepath.Join(filepath.Dir(f.upstream), "fork.git")
	testsupport.Git(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, fork)
	testsupport.Git(t, f.clone, "remote", "add", "fork", fork)
	fake := forgetest.New(f.upstream, fork)
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

// A closed or merged pull request from a branch of the same name, which
// isn't this branch's, is an earlier one: a new one opens beside it, and
// the plan names it. rc3's closed test pull request had dead-ended rc5's
// update of go-reflex to the same version (the rc5 full stage, A4).
func TestAnEarlierClosedPullRequestOfTheSameNameIsntThisOnes(t *testing.T) {
	t.Parallel()
	for _, state := range []forge.PullRequestState{forge.PullRequestClosed, forge.PullRequestMerged} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		fake := f.withFork(t, e)
		branch := committedUpdate(t, e)
		fake.PRs[1] = &forge.PullRequest{Ref: forge.PullRequestRef{Forge: forge.GitHub, Repository: UpstreamRepository, Number: 1},
			HeadRepository: "ada/macports-ports", HeadBranch: "dockhand/jq-update", State: state, Title: "[testing] jq: update to 1.8.1"}

		plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
		require.NoError(t, err, state)
		require.Nil(t, plan.Existing)
		require.NotNil(t, plan.Earlier)
		require.Equal(t, 1, plan.Earlier.Ref.Number)
		submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
		require.NoError(t, err)
		require.True(t, submitted.Created, "a new pull request")
		require.NotEqual(t, 1, submitted.PullRequest.Ref.Number)
	}
}

func TestSubmitWithoutACheckSaysSoAndOpensThePullRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "no check has finished for this commit's files")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "can't submit yet")
	require.Empty(t, fake.Created)

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"enhancement"}})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Equal(t, "jq: update to 1.8.1", plan.Title)
	require.Equal(t, "ada/macports-ports:dockhand/jq-update", plan.Head())
	require.Equal(t, []string{"jq"}, plan.Ports)
	require.False(t, plan.Replaces)
	require.Contains(t, plan.Body, "#### Description\n\n###### Type(s)\n\n- [ ] bugfix\n- [x] enhancement\n- [ ] security fix\n")
	require.Contains(t, plan.Body, "Submitted with `dockhand submit --no-check`; no Dockhand check result is included.")
	require.Contains(t, plan.Body, "- [x] followed our [Commit Message Guidelines]")
	require.Contains(t, plan.Body, "- [x] checked that there aren't other open [pull requests]")
	require.Contains(t, plan.Body, "- [ ] checked your Portfile with `port lint`?")

	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.True(t, submitted.Pushed)
	require.Equal(t, 34901, submitted.PullRequest.Ref.Number)
	head := testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")
	require.Equal(t, model.ObjectID(head), fake.ForkHead("dockhand/jq-update"), "the fork has the commit")

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
	t.Parallel()
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
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: a person's change")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n", "a commit dockhand didn't write is the person's to type")
}

func TestSubmitUpdatesThePullRequestAndKeepsAPersonsDescription(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	first, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
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
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Len(t, fake.Created, 1)
	require.Empty(t, fake.Updated, "the push is the update; the title and description had nothing new")
	require.Equal(t, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")), fake.ForkHead("dockhand/jq-update"))

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1, reviewed"})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, first.PullRequest.Ref.Number, fake.Updated[0].ExistingPR.Number)
	require.Equal(t, "jq: update to 1.8.1, reviewed", fake.PRs[first.PullRequest.Ref.Number].Title, "--title retitles")

	// Someone edits the description on GitHub; dockhand leaves it be.
	fake.PRs[first.PullRequest.Ref.Number].Body = "My own words.\n"
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# reviewed twice\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: note the second review")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.True(t, plan.BodyKept)
	require.Equal(t, "My own words.\n", plan.Body)
	require.False(t, plan.Replaces, "a new commit on top adds to the history")

	// And someone else pushes to the branch before this submit runs.
	other := filepath.Join(t.TempDir(), "other")
	testsupport.Git(t, t.TempDir(), "clone", "-q", "-b", "dockhand/jq-update", fake.Fork, other)
	write(t, other, map[string]string{"README": "theirs\n"})
	testsupport.Git(t, other, "add", "README")
	testsupport.Git(t, other, "commit", "-q", "-m", "their change")
	testsupport.Git(t, other, "push", "-q", "origin", "dockhand/jq-update")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorIs(t, err, ErrStaleSubmit, "the push is conditional on the head submit saw")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "someone else pushed to #34901")
	require.NotEmpty(t, plan.OthersPushed, "the preview's Push line says it won't push (the rc6 full stage, D-S1)")

	// Built on what they pushed, the branch goes, their commit kept beneath
	// it (py-coremltools, #35220, field testing at 7a38bfff).
	theirs := strings.TrimSpace(testsupport.Git(t, other, "rev-parse", "HEAD"))
	testsupport.Git(t, branch.Worktree, "fetch", "-q", fake.Fork, "dockhand/jq-update")
	testsupport.Git(t, branch.Worktree, "reset", "-q", "--hard", theirs)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# on theirs\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: on their change")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Empty(t, plan.OthersPushed)
	require.False(t, plan.Replaces)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")), fake.ForkHead("dockhand/jq-update"))

	// Made from what they pushed, as theirs amended in turn, which
	// MacPorts' one commit a change asks for, it goes too: the branch was
	// at their commit (#35220, the Vx language port session at a0470412).
	testsupport.Git(t, other, "pull", "-q", "--rebase=false", "origin", "dockhand/jq-update")
	write(t, other, map[string]string{"README": "theirs, again\n"})
	testsupport.Git(t, other, "commit", "-q", "-am", "their second change")
	testsupport.Git(t, other, "push", "-q", "origin", "dockhand/jq-update")
	again := strings.TrimSpace(testsupport.Git(t, other, "rev-parse", "HEAD"))
	testsupport.Git(t, branch.Worktree, "fetch", "-q", fake.Fork, "dockhand/jq-update")
	testsupport.Git(t, branch.Worktree, "reset", "-q", "--hard", again)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# amended over theirs\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-a", "--amend", "-m", "jq: amended from theirs")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.True(t, plan.Replaces, "the amend replaces their commit")
	require.Empty(t, plan.Blocking, "made from it, as the branch's reflog shows")
	require.Equal(t, model.ObjectID(again), plan.Amended, "the preview names the commit only the reflog vouches for")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")), fake.ForkHead("dockhand/jq-update"))

	// Pulled, reset away, and replaced by an amend of the branch's own
	// commit, theirs is someone else's push still, though the reflog once
	// saw it: the replacement isn't theirs amended (the rc4 full stage,
	// D-S1, herbyg-test/macports-ports#84).
	ours := strings.TrimSpace(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"))
	testsupport.Git(t, other, "pull", "-q", "--rebase=false", "origin", "dockhand/jq-update")
	write(t, other, map[string]string{"README": "theirs, a third time\n"})
	testsupport.Git(t, other, "commit", "-q", "-am", "their third change")
	testsupport.Git(t, other, "push", "-q", "origin", "dockhand/jq-update")
	third := strings.TrimSpace(testsupport.Git(t, other, "rev-parse", "HEAD"))
	testsupport.Git(t, branch.Worktree, "fetch", "-q", fake.Fork, "dockhand/jq-update")
	testsupport.Git(t, branch.Worktree, "reset", "-q", "--hard", third)
	testsupport.Git(t, branch.Worktree, "reset", "-q", "--hard", ours)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# amended over ours\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-a", "--amend", "-m", "jq: amended from ours")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "someone else pushed to #34901")
	require.Equal(t, model.ObjectID(third), plan.OthersPushed)
}

func TestSubmitNeedsCommittedWorkAndYourFork(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	fake := forgetest.New(f.upstream, "")
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
	checkedWith(t, e, branch, jq, viewer, nil)
}

// checkedWith is checked, with its execution and jq's result as alter
// leaves them.
func checkedWith(t *testing.T, e *Engine, branch model.Branch, jq, viewer model.Outcome, alter func(execution *model.GuestExecution, jq *model.TargetResult)) {
	t.Helper()
	checkedAs(t, e, branch, false, jq, viewer, alter)
}

// checkedAs is checkedWith, jq a port the branch adds where added, as a
// new port's check is planned.
func checkedAs(t *testing.T, e *Engine, branch model.Branch, added bool, jq, viewer model.Outcome, alter func(execution *model.GuestExecution, jq *model.TargetResult)) {
	t.Helper()
	head := model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"))
	tree := model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}"))
	at := time.Now().UTC().Truncate(time.Millisecond)
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		revision := model.Revision{ID: model.RevisionID(store.NewID("rev")), Branch: branch.ID, Kind: model.RevisionCommit, Source: model.Source{Commit: head, Tree: tree, Base: branch.Base}, Head: head, CreatedAt: at}
		plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: []model.Environment{tahoe}, Tests: model.TestsDeclared, CreatedAt: at,
			Targets: []model.PlanTarget{
				{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed, Added: added},
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
		jqResult := model.TargetResult{Execution: execution.ID, Target: "jq", Outcome: jq, Tests: model.TestsPassed, RecordedAt: at}
		if jq == model.OutcomeFailed {
			jqResult.Phase, jqResult.Tests = model.PhaseInstall, model.TestsNone
		}
		if alter != nil {
			alter(&execution, &jqResult)
		}
		for _, step := range []func() error{
			func() error { return tx.AddRevision(revision) },
			func() error { return tx.AddPlan(plan) },
			func() error { return tx.AddRun(runRecord) },
			func() error { return tx.AddExecution(execution) },
			func() error { return tx.RecordResult(jqResult) },
			func() error {
				if viewer == "" {
					return nil // the check didn't reach it
				}
				// The viewer links oniguruma, which it doesn't declare: a
				// link hold where the provider reads links.
				result := model.TargetResult{Execution: execution.ID, Target: "harbor-viewer", Outcome: viewer, Tests: model.TestsNone, RecordedAt: at,
					Links: []model.Link{{Binary: "/opt/local/bin/viewer", Library: "/opt/local/lib/libonig.5.dylib", Port: "oniguruma"}}}
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

// submit --accept --standing takes a changed port whose every failure is a
// dependency failing to install, with no baseline, and records the
// acceptance for every branch: a later submission takes it unasked, until
// the dependency's version or the image changes (the person's choice,
// 2026-10-18).
func TestAStandingAcceptanceIsRecordedAndTaken(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	gcc := model.FailedDependency{Name: "libgcc15", Version: "15.2.0_1"}
	dependencyFailed := func(identity string) func(*model.GuestExecution, *model.TargetResult) {
		return func(execution *model.GuestExecution, jq *model.TargetResult) {
			execution.Identity = identity
			jq.Phase, jq.Dependency, jq.Detail = model.PhaseDependencies, gcc, "Failed to build libgcc15: command execution failed"
		}
	}
	checkedWith(t, e, branch, model.OutcomeFailed, model.OutcomePassed, dependencyFailed("image sha256:12; setup 3"))

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Blocking, "nothing accepts it yet")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Standing: true})
	require.ErrorContains(t, err, "--standing takes the ports --accept names")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}, Standing: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Equal(t, AcceptedStanding, plan.Accepted["jq"].Kind)
	require.Contains(t, plan.Body, "| jq | ✗ a dependency failed, accepted: standing: libgcc15 15.2.0_1 fails on this image¹ |")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)

	// Taken unasked by a later submission of the same failure; not once
	// the image is another.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.Equal(t, AcceptedStanding, plan.Accepted["jq"].Kind)
	checkedWith(t, e, branch, model.OutcomeFailed, model.OutcomePassed, dependencyFailed("image sha256:13; setup 3"))
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Blocking, "a new image lapses it")
}

func TestSubmitFollowsThePublicationRule(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)

	checked(t, e, branch, model.OutcomePassed, model.OutcomeFailed)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "harbor-viewer (an extra from --also) did not pass in check-1. If the failure was not caused by this branch, acknowledge it with --accept harbor-viewer")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}})
	require.ErrorContains(t, err, "this port passed in check-1; there is no failure to accept")

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
	require.Contains(t, plan.Body, "- [x] tried a full install with `sudo port -vst install`? (Dockhand builds from source, as MacPorts CI does, without trace mode)")
	require.Contains(t, plan.Body, "- [x] tried existing tests with `sudo port test`?")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		accepted, err := r.Decisions(branch.ID, model.FindingFailure, testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"))
		require.NoError(t, err)
		require.Len(t, accepted, 1)
		return nil
	}))
	require.Len(t, fake.Created, 1)

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
	require.ErrorContains(t, err, "--accept harbor-viewer: no check of these files built this port, so there is no failure to accept")

	// A changed port that fails goes out only as a draft.
	g := setup(t)
	e2, _ := g.withPreparer(t)
	fake2 := g.withFork(t, e2)
	branch2 := committedUpdate(t, e2)
	checked(t, e2, branch2, model.OutcomeFailed, model.OutcomePassed)
	_, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2, Accept: []string{"jq"}})
	require.ErrorContains(t, err, "can be accepted only if the baseline fails the same way")
	plan, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2})
	require.NoError(t, err)
	require.Contains(t, strings.Join(plan.Blocking, "\n"), "jq did not pass in check-1. Fix the failure, submit a draft with --draft, or run dockhand check --baseline to see whether master fails in the same way")
	plan, err = e2.PlanSubmit(t.Context(), SubmitRequest{Branch: branch2, Draft: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	_, err = e2.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.True(t, fake2.Created[0].Draft)
}

// A changed port whose baseline fails it the same way is no regression,
// the usual case for a MacPorts pull request: submit names it, a person
// accepts it with --accept, and the pull request says the base fails it
// too (pgdog 0.1.62 on macOS 12, field testing; the person's word,
// 2026-10-17). A baseline that fails it otherwise, or passes, isn't one.
func TestAFailureTheBaseHasTooIsAccepted(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		outcome model.Outcome
		phase   model.Phase
		alike   bool
	}{
		{"alike", model.OutcomeFailed, model.PhaseInstall, true},
		{"at another phase", model.OutcomeFailed, model.PhaseTest, false},
		{"passing at the base", model.OutcomePassed, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			g := setup(t)
			e, _ := g.withPreparer(t)
			g.withFork(t, e)
			branch := committedUpdate(t, e)
			checked(t, e, branch, model.OutcomeFailed, model.OutcomePassed)
			baselined(t, e, branch, test.outcome, test.phase)
			plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
			require.NoError(t, err)
			blocking := strings.Join(plan.Blocking, "\n")
			_, acceptErr := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}})
			if !test.alike {
				require.Contains(t, blocking, "jq did not pass in check-1. Fix the failure")
				require.ErrorContains(t, acceptErr, "can be accepted only if the baseline fails the same way")
				return
			}
			require.Contains(t, blocking, "jq failed in check-1 and has the same failure at the base (check-2). Use --accept jq to submit with this existing failure disclosed")
			require.NoError(t, acceptErr)
			plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}})
			require.NoError(t, err)
			require.Empty(t, plan.Blocking)
			require.Contains(t, plan.Body, "| jq | ✗ failed at install, accepted: fails at the base too (check-2) |")
		})
	}
}

// baselined records a finished baseline of the branch's newest check, its
// jq built at the base with outcome, at phase where it failed.
func baselined(t *testing.T, e *Engine, branch model.Branch, outcome model.Outcome, phase model.Phase) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		runs, err := tx.Runs(store.RunFilter{Branch: branch.ID, Limit: 1})
		if err != nil {
			return err
		}
		of := runs[0]
		plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: of.Revision, Environments: []model.Environment{tahoe}, Tests: model.TestsDeclared, CreatedAt: at,
			Targets: []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed}},
			Builds:  []model.EnvironmentPlan{{Environment: tahoe, Order: []model.TargetID{"jq"}}}}
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		run := model.Run{ID: model.RunID(store.NewID("run")), Branch: branch.ID, Revision: of.Revision, Plan: plan.ID, Number: number, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: at, BaselineOf: of.ID}
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID("tart")), Run: run.ID, Environment: tahoe, Attempt: 1, State: model.ExecutionWaiting, CreatedAt: at}
		result := model.TargetResult{Execution: execution.ID, Target: "jq", Outcome: outcome, Phase: phase, Tests: model.TestsNone, RecordedAt: at}
		for _, step := range []func() error{
			func() error { return tx.AddPlan(plan) },
			func() error { return tx.AddRun(run) },
			func() error { return tx.AddExecution(execution) },
			func() error { return tx.RecordResult(result) },
		} {
			if err := step(); err != nil {
				return err
			}
		}
		state := model.RunFailed
		if outcome == model.OutcomePassed {
			state = model.RunPassed
		}
		for _, next := range []model.RunState{model.RunRunning, state} {
			run.State = next
			if next == state {
				run.FinishedAt = &at
			}
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
		}
		return nil
	}))
}

// Someone's repository is pushed to by a remote that already pushes there,
// else at its GitHub address, over SSH where your remotes push over it.
func TestTheirRemoteIsOneThatPushesThereOrTheirAddress(t *testing.T) {
	t.Parallel()
	e := &Engine{Forge: &forgetest.GitHub{Repos: map[string]string{"bo/macports-ports": "/remotes/bo"}}}
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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	fake.ReadyRefused = restricted{errors.New("github: the `macports` organization has enabled OAuth App access restrictions")}

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
	events, err := e.Events(t.Context(), 0, "")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("marked #%d ready for review with the GitHub CLI", readied.PullRequest.Number), events[len(events)-1].Message)
}

// A submission waiting on a check of its files that hasn't finished names
// it and the wait, not a new check to run (the sshuttle run).
func TestASubmissionNamesTheCheckItWaitsOn(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	checkPlan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, checkPlan, model.OriginPerson)
	require.NoError(t, err)

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, []string{queued.Name() + " checks this commit’s files and is queued. Run dockhand wait " + queued.Name() + ", then submit; or use --draft to submit now"}, plan.Blocking)
}

// The search for a port's other open pull requests leaves out the branch's
// own, and finds each once however many of the ports it's for.
func TestOtherOpenPullRequestsLeaveOutTheBranchsOwn(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	fake.Others = []forge.PullRequestSummary{{Number: 35044, Title: "flatbuffers: update"}, {Number: 34620, Title: "libsigmf, flatbuffers: rebuild"}, {Number: 35000, Title: "libuv: update for flatbuffers"}}
	others, problem := e.openPullRequests(t.Context(), []string{"flatbuffers", "libsigmf"}, 35044)
	require.Empty(t, problem)
	require.Equal(t, []forge.PullRequestSummary{{Number: 34620, Title: "libsigmf, flatbuffers: rebuild"}}, others, "a title naming the port as a subject does, once")
}

// A Portfile the branch adds, which its base doesn't have, is a new port,
// said in the pull request as the submitted files evaluate it; a port the
// base already has is none (the txt run's finding 4).
func TestSubmitSaysTheNewPortsTheBranchAdds(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	write(t, branch.Worktree, map[string]string{"devel/harbor/Portfile": "name harbor\nversion 1.0\n"})
	testsupport.Git(t, branch.Worktree, "add", "--sparse", "devel/harbor/Portfile")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "harbor: new port, version 1.0")
	harbor := macports.PortInfo{Name: "harbor", Version: "1.0", Options: map[string]string{"description": "{Harbor tools for the command line}", "homepage": "https://harbor.example/", "license": "{MIT Apache-2}"}}
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"devel/harbor": {harbor}, "textproc/jq": {port("jq")}}})
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
	t.Parallel()
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
	// The commit's body is its attribution alone, so the note is the
	// Description itself, unquoted (field testing, 2026-10-20).
	quoted := "rust's tests didn't run: bootstrap panicked on a permission error.\n\nThe environment's, not the tests'.\n\n"
	require.Contains(t, plan.Body, "#### Description\n\n"+quoted+"###### Type(s)\n")
	require.Empty(t, recordedNote(), "a plan records nothing")
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.Equal(t, "rust's tests didn't run: bootstrap panicked on a permission error.\n\nThe environment's, not the tests'.", recordedNote())

	// Submitting again, without --note, keeps it.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, quoted)
	require.Equal(t, DescriptionSections{Description: SectionCurrent, Types: SectionCurrent, TestedOn: SectionCurrent}, plan.Sections)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Empty(t, fake.Updated, "nothing new")

	// A new note replaces it, and the pull request is updated for it alone.
	// One with a line that would begin a heading has its # written as
	// &#35;, and so stays within the Description when replaced again.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream in\n#35090.")})
	require.NoError(t, err)
	require.Equal(t, SectionRefreshed, plan.Sections.Description)
	require.Contains(t, plan.Body, "#### Description\n\nFixed upstream in\n&#35;35090.\n\n###### Type(s)\n")
	require.NotContains(t, plan.Body, "permission error")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Len(t, fake.Updated, 1, "the note's change is the pull request's")
	require.Equal(t, plan.Body, fake.PRs[number].Body)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream.")})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "#### Description\n\nFixed upstream.\n\n###### Type(s)\n")
	require.NotContains(t, plan.Body, "#35090")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)

	// An empty note takes it out.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("")})
	require.NoError(t, err)
	require.Equal(t, SectionRefreshed, plan.Sections.Description)
	require.Contains(t, plan.Body, "#### Description\n\n###### Type(s)\n")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Len(t, fake.Updated, 3)
	require.NotContains(t, fake.PRs[number].Body, "Author's note")
	require.Empty(t, recordedNote())

	// A Description a person edited on GitHub is theirs, and the plan says
	// the note isn't in it.
	fake.PRs[number].Body = strings.Replace(fake.PRs[number].Body, "#### Description\n\n", "#### Description\n\nMy own words.\n\n", 1)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Note: new("Fixed upstream.")})
	require.NoError(t, err)
	require.Equal(t, SectionKept, plan.Sections.Description)
	require.True(t, plan.NoteLeftOut)
	require.NotContains(t, plan.Body, "Author's note")
}

// archive says a pull request is still open as GitHub has it now, not as
// last recorded: one closed with gh was said to be still open (the rc6
// full stage).
func TestArchiveReadsThePullRequestsState(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)

	fake.PRs[submitted.PullRequest.Ref.Number].State = forge.PullRequestClosed
	archived, err := e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: branch, KeepWorktree: true})
	require.NoError(t, err)
	require.False(t, archived.PullRequestOpen, "closed on GitHub since it was recorded open")

	_, err = e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: archived.Branch, Undo: true})
	require.NoError(t, err)
	fake.PRs[submitted.PullRequest.Ref.Number].State = forge.PullRequestOpen
	archived, err = e.ArchiveBranch(t.Context(), ArchiveRequest{Branch: archived.Branch, KeepWorktree: true})
	require.NoError(t, err)
	require.True(t, archived.PullRequestOpen)
}

// A commit no check passed, on a branch whose earlier check passed, as
// after a rebase, says the check it needs reuses that check's builds (the
// rc6 full stage, B5).
func TestSubmitSaysACheckWouldReuseTheEarlierOne(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	checked(t, e, branch, model.OutcomePassed, model.OutcomePassed)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": read(t, portfile) + "# a later commit\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: a later commit")

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Blocking)
	require.Contains(t, plan.Blocking[0], "no completed check covers this commit’s files. Run dockhand check; it reuses eligible builds from check-1 and builds only what changed")
}

// What create marked unconfirmed holds a submission until a person
// confirms it (the rc6 full stage, B8: serie said ready with six marks).
func TestSubmitHoldsWhatsMarkedUnconfirmed(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": read(t, portfile) + "# dockhand: unconfirmed, from the forge's license detection\nlicense MIT\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: license")

	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Blocking, `textproc/jq/Portfile contains 1 guess marked unconfirmed (license). Verify each item, remove its "# dockhand: unconfirmed," line, and run dockhand tidy`)
}

// A standing acceptance is one eligibility: what submit takes, serve's
// candidates and ship --passing take, and status says it waits to be
// submitted, accepted, where they judged without it; and where its
// evidence lapses, none takes it (Codex's review of 8c007979, C8-3, its
// probe).
func TestAStandingAcceptanceIsOneEligibility(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	gcc := model.FailedDependency{Name: "libgcc15", Version: "15.2.0_1"}
	failed := func(dependency model.FailedDependency) {
		checkedWith(t, e, branch, model.OutcomeFailed, model.OutcomePassed, func(execution *model.GuestExecution, jq *model.TargetResult) {
			execution.Identity = "image sha256:12; setup 3"
			jq.Phase, jq.Dependency, jq.Detail = model.PhaseDependencies, dependency, "Failed to build "+dependency.Name
		})
	}
	failed(gcc)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}, Standing: true})
	require.NoError(t, err)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for _, decision := range plan.Standing {
			decision.At = e.now()
			if err := tx.AddDecision(decision); err != nil {
				return err
			}
		}
		return nil
	}))
	agree := func(eligible bool, why string) {
		t.Helper()
		plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
		require.NoError(t, err)
		require.Equal(t, eligible, len(plan.Blocking) == 0, "submit: %s", why)
		passing, err := e.PassingBranches(t.Context())
		require.NoError(t, err)
		require.Equal(t, eligible, slices.ContainsFunc(passing.Ready, func(s BranchStatus) bool { return s.Branch.ID == branch.ID }), "serve and ship --passing: %s", why)
		status, err := e.BranchStatus(t.Context(), branch)
		require.NoError(t, err)
		rows := branchstate.Attend(status.Facts())
		require.Len(t, rows, 1)
		// Accepted, it goes on as a pass does, here to what holds it
		// unattended; not, it's the failure to look at.
		require.Equal(t, !eligible, rows[0].Kind == branchstate.Failed, "status and Next: %s, got %s", why, rows[0].Reason)
		require.Equal(t, branchstate.CheckFailed, status.CheckStanding(), "what happened stays failed")
	}
	agree(true, "the standing acceptance takes libgcc15 15.2.0_1's failure")
	// A newer libgcc15 lapses it.
	failed(model.FailedDependency{Name: "libgcc15", Version: "15.3.0_0"})
	agree(false, "libgcc15 15.3.0_0 is no failure it accepted")
}

// The link guard reads a tree's combined evidence, where a narrower check
// after a full one keeps the full one's results: the viewer's undeclared
// oniguruma, which the first check found, still holds after a check of jq
// alone, where the narrower run's results dropped it (Codex's review of
// 8e314b96, finding 4).
func TestALinkHoldStandsAfterANarrowerCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}, "graphics/harbor-viewer": {port("harbor-viewer")}}})
	e.Providers = map[string]buildenv.Provider{"tart": &scriptedProvider{readsLinks: true}}
	tree := model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}"))
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, nil)
	first, found, err := e.EvidenceFor(t.Context(), branch.ID, tree)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, e.linkHolds(t.Context(), &first), "the full check finds the viewer's undeclared link")

	checkedWith(t, e, branch, model.OutcomePassed, "", nil)
	combined, found, err := e.EvidenceFor(t.Context(), branch.ID, tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, combined.Failed(), "the viewer's earlier result still stands")
	held := e.linkHolds(t.Context(), &combined)
	require.Len(t, held, 1, "a narrower check keeps the earlier result's hold")
	require.Equal(t, "oniguruma", held[0].Subject)
}

// Status loads the decisions a standing reads, a new port's lint
// acceptance among them, where it loaded them only when a target failed:
// submit took a passed new port whose lint warnings were accepted, and
// status, serve, and ship --passing said it wasn't ready (Codex's review
// of 8e314b96, finding 6).
func TestStatusReadsALintAcceptanceAsSubmitDoes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	checkedAs(t, e, branch, true, model.OutcomePassed, model.OutcomePassed, func(_ *model.GuestExecution, jq *model.TargetResult) {
		jq.Lint, jq.Nitpicked = []string{"warning: review new port"}, true
	})
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.False(t, status.Check().Publishable(), "unaccepted, a new port's lint warning holds")
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Accept: []string{"jq"}})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		return tx.AddDecision(model.Decision{Branch: branch.ID, Finding: model.FindingFailure, Subject: "jq", Evidence: plan.Commit, Answer: model.Accepted, At: e.now()})
	}))
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking, "submit reads the acceptance")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, status.Check().Publishable(), "status reads it too: %#v", status.Accepted)
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(passing.Ready, func(s BranchStatus) bool { return s.Branch.ID == branch.ID }), "serve and ship --passing take it")
}
