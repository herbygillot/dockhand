package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

type writeCountingStore struct {
	store.Store
	writes int
}

func (s *writeCountingStore) Update(ctx context.Context, repository model.RepositoryID, fn func(store.Tx) error) error {
	s.writes++
	return s.Store.Update(ctx, repository, fn)
}

func TestPlanAdoptDoesNotWriteStoreOrChangeRefs(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	testsupport.Git(t, f.clone, "switch", "-q", "-c", "plan-jq")
	testsupport.Git(t, f.clone, "commit", "--allow-empty", "-q", "-m", "empty")
	refs := testsupport.Git(t, f.clone, "show-ref")
	writes := &writeCountingStore{Store: e.Store}
	e.Store = writes
	plan, err := e.PlanAdopt(t.Context(), AdoptRequest{Branch: "plan-jq"}, 0)
	require.NoError(t, err)
	require.Equal(t, "plan-jq", plan.Branch)
	require.Zero(t, writes.writes)
	require.Equal(t, refs, testsupport.Git(t, f.clone, "show-ref"))

	g := forgetest.New(f.upstream, "")
	g.Theirs = map[int]forge.PullRequest{35174: {
		Ref:            forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 35174},
		HeadRepository: "ada/macports-ports", HeadBranch: "review", BaseBranch: "master", RemoteHead: model.ObjectID("1234567890123456789012345678901234567890"),
		Commits: 2, State: forge.PullRequestOpen,
	}}
	e.Forge = g
	plan, err = e.PlanAdopt(t.Context(), AdoptRequest{}, 35174)
	require.NoError(t, err)
	require.Equal(t, "pr-35174", plan.Branch)
	require.False(t, plan.ScopeKnown)
	require.Zero(t, writes.writes)
	require.Equal(t, refs, testsupport.Git(t, f.clone, "show-ref"))
}

func TestPlanAdoptUsesTheSameLocalBranchJudgementAsAdopt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	_, err := e.PlanAdopt(t.Context(), AdoptRequest{Branch: "missing"}, 0)
	_, applyErr := e.Adopt(t.Context(), AdoptRequest{Branch: "missing"})
	require.EqualError(t, err, applyErr.Error())
	require.EqualError(t, err, "there is no local branch missing")

	branch, err := e.Start(t.Context(), StartRequest{Name: "renamed-old"})
	require.NoError(t, err)
	head, _, err := e.Repo.Branch(t.Context(), branch.Name)
	require.NoError(t, err)
	err = e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		branch.Base = model.ObjectID(head)
		return tx.UpdateBranch(branch)
	})
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "branch", "-m", "renamed-new")
	plan, err := e.PlanAdopt(t.Context(), AdoptRequest{Branch: "renamed-new"}, 0)
	require.NoError(t, err)
	require.True(t, plan.Already)
	require.Equal(t, "renamed-old", plan.Renamed)
}
