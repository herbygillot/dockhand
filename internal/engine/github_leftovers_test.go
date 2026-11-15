package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func githubLeftBehind(t *testing.T) *githubCheck {
	t.Helper()
	g := newGitHubCheck(t, true)
	g.e.Providers[buildenv.GitHub] = withoutSettlement{g.p}
	_, err := g.e.Drive(t.Context(), session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.True(t, g.head(t).Exists)
	g.e.Providers[buildenv.GitHub] = g.p
	return g
}

type tartLeavingProvider struct{ leavingProvider }

func (*tartLeavingProvider) Name() string { return buildenv.Tart }

func leaveTartBesideGitHub(t *testing.T, g *githubCheck) *tartLeavingProvider {
	t.Helper()
	p := &tartLeavingProvider{}
	g.e.Providers[buildenv.Tart] = p
	environment := tahoeArm
	environment.Provider = buildenv.Tart
	var revision model.Revision
	var branch model.Branch
	require.NoError(t, g.e.Store.View(t.Context(), g.e.Repository, func(r store.Reader) error {
		var err error
		if revision, err = r.Revision(g.run.Revision); err != nil {
			return err
		}
		branch, err = r.Branch(g.run.Branch)
		return err
	}))
	plan, err := g.e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{environment}})
	require.NoError(t, err)
	run, err := g.e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	_, err = g.e.Drive(t.Context(), session(t, g.e), run.ID)
	require.NoError(t, err)
	require.Len(t, p.left, 1)
	return p
}

func TestGitHubListingFailureDoesNotBlockTartCleanup(t *testing.T) {
	t.Parallel()
	for _, daily := range []bool{false, true} {
		t.Run(fmt.Sprint("daily ", daily), func(t *testing.T) {
			t.Parallel()
			g := githubLeftBehind(t)
			tart := leaveTartBesideGitHub(t, g)
			clone := tart.left[0]
			g.p.Repo.Hook = func(_ context.Context, args []string, run func() error) error {
				if args[0] == "ls-remote" {
					return errors.New("GitHub unreachable\ntry later")
				}
				return run()
			}
			cleaner := session(t, g.e)
			if daily {
				report, err := g.e.Cleanup(t.Context(), cleaner, 7*24*time.Hour)
				require.NoError(t, err)
				words := strings.Join(report.Words(7*24*time.Hour), "\n")
				require.Equal(t, 1, strings.Count(words, "GitHub unreachable"))
				require.NotContains(t, words, "unreachable\n", "the refusal is one line")
			} else {
				plan, err := g.e.PlanLeftovers(t.Context(), cleaner)
				require.NoError(t, err)
				problems := 0
				for _, leftover := range plan {
					if strings.Contains(leftover.Kept, "GitHub unreachable") {
						problems++
						require.NotContains(t, leftover.Kept, "\n")
					}
				}
				require.Equal(t, 1, problems)
				_, err = g.e.RemoveLeftovers(t.Context(), cleaner, plan)
				require.NoError(t, err)
			}
			require.Equal(t, []string{clone}, tart.removed)
		})
	}
}

func TestKeptClonesNeverAsksGitHubForLeftovers(t *testing.T) {
	t.Parallel()
	g := githubLeftBehind(t)
	calls := 0
	g.p.Fork = func(context.Context) (buildenv.Fork, error) {
		calls++
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: g.fork}, nil
	}
	kept, err := g.e.KeptClones(t.Context())
	require.NoError(t, err)
	require.Empty(t, kept)
	require.Zero(t, calls)
}

func TestGitHubLeftoversWithoutAForkAreQuiet(t *testing.T) {
	t.Parallel()
	g := githubLeftBehind(t)
	// The engine's normal resolver finds no fork for this checkout.
	g.p.Fork = func(ctx context.Context) (buildenv.Fork, error) { return g.e.Fork(ctx, "") }
	plan, err := g.e.PlanLeftovers(t.Context(), session(t, g.e))
	require.NoError(t, err)
	require.Empty(t, plan)
}

func TestCleanListsAMergedChecksBranchOnce(t *testing.T) {
	t.Parallel()
	g := githubLeftBehind(t)
	testsupport.Git(t, g.checkout, "remote", "add", "fork", g.fork)
	g.e.Forge = forgetest.New("", g.fork)
	require.NoError(t, g.e.Store.Update(t.Context(), g.e.Repository, func(tx store.Tx) error {
		branch, err := tx.Branch(g.run.Branch)
		if err != nil {
			return err
		}
		branch.State = model.BranchMerged
		branch.EndedAt = at
		branch.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 1, Head: "ada/macports-ports:dockhand/libharbor-2", Pushed: model.ObjectID(g.commit)}
		return tx.UpdateBranch(branch)
	}))
	branches, err := g.e.PlanClean(t.Context())
	require.NoError(t, err)
	leftovers, err := g.e.PlanLeftovers(t.Context(), session(t, g.e))
	require.NoError(t, err)
	listed := 0
	for _, branch := range branches {
		for _, step := range branch.Steps {
			if strings.Contains(step.What, g.branch) {
				listed++
			}
		}
	}
	for _, leftover := range leftovers {
		if strings.Contains(leftover.What, g.branch) {
			listed++
		}
	}
	require.Equal(t, 1, listed)
	require.True(t, slices.ContainsFunc(leftovers, func(l Leftover) bool { return strings.Contains(l.What, g.branch) }), "the launch-note listing owns this branch after its merge too")
}
