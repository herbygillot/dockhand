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
