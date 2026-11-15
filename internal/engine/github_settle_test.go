package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// settlingAPI lists no run unless the test gives it one, and completes a
// cancellation when asked, without asking GitHub (#241).
type settlingAPI struct {
	ghactions.API
	workflow func() (string, error)
	runs     func() ([]ghactions.Run, error)
	observe  func(branch, commit string)
	canceled bool
}

func (a *settlingAPI) WorkflowState(context.Context, string) (string, error) {
	if a.workflow != nil {
		return a.workflow()
	}
	return "active", nil
}
func (a *settlingAPI) Runs(_ context.Context, _, branch, commit string) ([]ghactions.Run, error) {
	if a.observe != nil {
		a.observe(branch, commit)
	}
	if a.runs != nil {
		return a.runs()
	}
	return nil, nil
}
func (a *settlingAPI) Cancel(context.Context, string, int64) error {
	a.canceled = true
	return nil
}

// singleGitHub takes the same provider through the runner's single
// environment path as well as its usual matrix path (#241).
type singleGitHub struct{ *ghactions.Provider }

func (singleGitHub) Matrix() bool { return false }

type githubCheck struct {
	e        *Engine
	p        *ghactions.Provider
	api      *settlingAPI
	run      model.Run
	fork     string
	branch   string
	commit   string
	checkout string
	settled  int
}

func newGitHubCheck(t *testing.T, matrix bool) *githubCheck {
	t.Helper()
	f := setup(t)
	e := f.open(t)
	fork := filepath.Join(t.TempDir(), "fork.git")
	testsupport.Git(t, f.clone, "init", "-q", "--bare", fork)
	api := &settlingAPI{}
	g := &githubCheck{e: e, api: api, fork: fork, checkout: f.clone}
	p := &ghactions.Provider{Repo: e.Repo, API: api, Fork: func(context.Context) (buildenv.Fork, error) {
		if g.run.ID != "" {
			require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
				run, err := r.Run(g.run.ID)
				if run.State.Terminal() {
					g.settled++
				}
				return err
			}))
		}
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
	}, Sleep: func(context.Context, time.Duration) error { return nil }}
	var provider buildenv.Provider = p
	if !matrix {
		provider = singleGitHub{p}
	}
	e.Providers = map[string]buildenv.Provider{buildenv.GitHub: provider}
	environment := tahoeArm
	environment.Provider = buildenv.GitHub
	environments := []model.Environment{environment}
	if matrix {
		other := tahoeX86
		other.Provider = buildenv.GitHub
		environments = append(environments, other)
	}
	g.run = queuedHarborRun(t, e, environments...)
	g.p = p
	api.observe = func(branch, commit string) { g.branch, g.commit = branch, commit }
	return g
}

func (g githubCheck) head(t *testing.T) git.RefValue {
	t.Helper()
	head, err := g.e.Repo.RemoteHead(t.Context(), g.fork, g.branch)
	require.NoError(t, err)
	return head
}

func (g githubCheck) said(t *testing.T, words string) int {
	t.Helper()
	count := 0
	require.NoError(t, g.e.Store.View(t.Context(), g.e.Repository, func(r store.Reader) error {
		events, err := r.Events(0, 10000, "")
		for _, event := range events {
			if event.Run == g.run.ID && strings.Contains(event.Message, words) {
				count++
			}
		}
		return err
	}))
	return count
}

// A push with no run keeps its branch for a retry, and loses it once the
// last attempt is recorded, in both runner paths (#241).
func TestGitHubSettlesTheLastAttemptWithoutARun(t *testing.T) {
	t.Parallel()
	for _, matrix := range []bool{false, true} {
		name := "single"
		if matrix {
			name = "matrix"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newGitHubCheck(t, matrix)
			attempt := 0
			g.api.workflow = func() (string, error) {
				attempt++
				if attempt > 1 {
					require.Equal(t, git.RefValue{Exists: true, Object: g.commit}, g.head(t), "the earlier attempt leaves its branch for the retry")
				}
				return "active", nil
			}
			run, err := g.e.Drive(t.Context(), session(t, g.e), g.run.ID)
			require.NoError(t, err)
			require.Equal(t, model.RunAttention, run.State)
			require.Equal(t, model.MaxAttempts, attempt)
			require.False(t, g.head(t).Exists)
			require.Equal(t, 1, g.settled, "the shared launch is settled once, after the verdict was stored")
			require.Equal(t, 1, g.said(t, "removed "+g.branch+" from ada/macports-ports"))
		})
	}
}

// A moved check branch belongs to whoever moved it, and is named once
// when the runner settles the environment (#241).
func TestGitHubSettleLeavesAMovedBranchAndSaysSo(t *testing.T) {
	t.Parallel()
	g := newGitHubCheck(t, true)
	later := testsupport.Git(t, g.checkout, "rev-parse", "master")
	moved := false
	g.p.Sleep = func(context.Context, time.Duration) error {
		if !moved {
			testsupport.Git(t, g.checkout, "push", "-q", "-f", g.fork, later+":refs/heads/"+g.branch)
			moved = true
		}
		return nil
	}
	// The next attempt refuses the disabled workflow rather than pushing
	// over the person's move.
	attempt := 0
	g.api.workflow = func() (string, error) {
		attempt++
		if attempt > 1 {
			return "disabled_manually", nil
		}
		return "active", nil
	}
	run, err := g.e.Drive(t.Context(), session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunAttention, run.State)
	require.Equal(t, later, g.head(t).Object)
	require.Equal(t, 1, g.settled)
	require.Equal(t, 1, g.said(t, "left "+g.branch+" on ada/macports-ports, since it has moved"))
}

// Cancellation first stops the run, then records the canceled check, and
// only then removes its branch (#241).
func TestGitHubSettleRemovesACanceledChecksBranch(t *testing.T) {
	t.Parallel()
	g := newGitHubCheck(t, true)
	listed := 0
	g.api.runs = func() ([]ghactions.Run, error) {
		listed++
		if listed == 1 {
			return nil, nil
		}
		status := "in_progress"
		if g.api.canceled {
			status = "completed"
		}
		return []ghactions.Run{{ID: 7, Attempt: 1, Status: status, URL: "https://github.com/ada/macports-ports/actions/runs/7"}}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	g.p.Sleep = func(context.Context, time.Duration) error {
		require.NoError(t, g.e.Store.Update(t.Context(), g.e.Repository, func(tx store.Tx) error {
			run, err := tx.Run(g.run.ID)
			if err != nil {
				return err
			}
			run.CancelRequested = &at
			return tx.UpdateRun(run)
		}))
		cancel()
		return ctx.Err()
	}
	// current uses the shared listing while Execute waits for the run.
	g.api.API = &settlingRecent{api: g.api}
	run, err := g.e.Drive(ctx, session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, run.State)
	require.True(t, g.api.canceled)
	require.Equal(t, 1, g.settled)
	require.False(t, g.head(t).Exists)
	require.Equal(t, 1, g.said(t, "removed "+g.branch+" from ada/macports-ports"))
}

type settlingRecent struct {
	ghactions.API
	api *settlingAPI
}

func (a *settlingRecent) RecentRuns(context.Context, string) ([]ghactions.Run, error) {
	return a.api.runs()
}

// A refused deletion cannot change the check's recorded result. Clean
// and the daily cleanup both finish it later, before its branch merges
// (#241).
func TestGitHubSettleFailureLeavesCleanupTheBranch(t *testing.T) {
	t.Parallel()
	for _, daily := range []bool{false, true} {
		name := "clean"
		if daily {
			name = "daily"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newGitHubCheck(t, true)
			hook := filepath.Join(g.fork, "hooks", "update")
			require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\ncase \"$3\" in 0000000000000000000000000000000000000000) echo 'delete refused' >&2; exit 1;; esac\n"), 0o755))
			run, err := g.e.Drive(t.Context(), session(t, g.e), g.run.ID)
			require.NoError(t, err)
			require.Equal(t, model.RunAttention, run.State)
			require.True(t, g.head(t).Exists)
			require.Equal(t, 1, g.settled, "a failure settles once and leaves the verdict recorded")
			require.Equal(t, 1, g.said(t, "couldn't remove "+g.branch))
			require.NoError(t, os.Remove(hook))
			cleaner := session(t, g.e)
			plan, err := g.e.PlanLeftovers(t.Context(), cleaner)
			require.NoError(t, err)
			require.Len(t, plan, 1)
			require.Empty(t, plan[0].Kept)
			require.Equal(t, g.run.ID, plan[0].Run.ID)
			if daily {
				report, err := g.e.Cleanup(t.Context(), cleaner, 7*24*time.Hour)
				require.NoError(t, err)
				require.Len(t, report.Leftovers, 1)
				require.True(t, report.Leftovers[0].Done)
			} else {
				done, err := g.e.RemoveLeftovers(t.Context(), cleaner, plan)
				require.NoError(t, err)
				require.True(t, done[0].Done)
			}
			require.False(t, g.head(t).Exists)
		})
	}
}

// withoutSettlement stands in for a driver killed after recording its
// last attempt and before asking the provider to settle it (#241).
type withoutSettlement struct{ buildenv.Provider }

func (withoutSettlement) Matrix() bool { return true }

func TestGitHubCleanFindsABranchWhoseDriverNeverSettledIt(t *testing.T) {
	t.Parallel()
	g := newGitHubCheck(t, true)
	g.e.Providers[buildenv.GitHub] = withoutSettlement{g.p}
	run, err := g.e.Drive(t.Context(), session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunAttention, run.State)
	require.True(t, g.head(t).Exists)
	g.e.Providers[buildenv.GitHub] = g.p
	cleaner := session(t, g.e)
	plan, err := g.e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	require.Len(t, plan, 1, "the push's note identifies it without a run URL")
	require.Empty(t, plan[0].Kept)
	require.Equal(t, run.ID, plan[0].Run.ID)
	// A move after the listing cannot change clean's expected value.
	later := testsupport.Git(t, g.checkout, "rev-parse", "master")
	testsupport.Git(t, g.checkout, "push", "-q", "-f", g.fork, later+":refs/heads/"+g.branch)
	done, err := g.e.RemoveLeftovers(t.Context(), cleaner, plan)
	require.NoError(t, err)
	require.False(t, done[0].Done)
	require.Contains(t, done[0].Kept, "moved")
	require.Equal(t, later, g.head(t).Object)
	plan, err = g.e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	require.Contains(t, plan[0].Kept, "moved", "the next listing says why it stays")
	testsupport.Git(t, g.checkout, "push", "-q", "-f", g.fork, g.commit+":refs/heads/"+g.branch)
	plan, err = g.e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	done, err = g.e.RemoveLeftovers(t.Context(), cleaner, plan)
	require.NoError(t, err)
	require.True(t, done[0].Done)
	require.False(t, g.head(t).Exists)
	// A death after the delete but before its event leaves nothing for
	// the next listing to repeat.
	plan, err = g.e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	require.Empty(t, plan)
}

// A resumable driver stopping, and a stop that cannot answer a launch,
// both leave the branch. Only a finished cancel settles it (#241).
func TestGitHubSettleWaitsForAResumedChecksCancelToFinish(t *testing.T) {
	t.Parallel()
	g := newGitHubCheck(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	g.p.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	run, err := g.e.Resume(ctx, session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, run.State)
	require.True(t, g.head(t).Exists)
	cleaner := session(t, g.e)
	plan, err := g.e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	require.Len(t, plan, 1)
	require.Contains(t, plan[0].Kept, "can still be resumed")
	g.p.Sleep = func(context.Context, time.Duration) error { return nil }
	run, err = g.e.RequestCancel(t.Context(), session(t, g.e), g.run.ID)
	require.ErrorContains(t, err, "cancel")
	require.Equal(t, model.RunRunning, run.State)
	require.True(t, g.head(t).Exists, "Settle never stops an unanswered push")
	g.api.runs = func() ([]ghactions.Run, error) {
		status := "in_progress"
		if g.api.canceled {
			status = "completed"
		}
		return []ghactions.Run{{ID: 7, Attempt: 1, Status: status, URL: "https://github.com/ada/macports-ports/actions/runs/7"}}, nil
	}
	run, err = g.e.RequestCancel(t.Context(), session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, run.State)
	require.False(t, g.head(t).Exists)
	require.Equal(t, 1, g.said(t, "removed "+g.branch+" from ada/macports-ports"))
	_, err = g.e.RequestCancel(t.Context(), session(t, g.e), g.run.ID)
	require.NoError(t, err)
	require.Equal(t, 1, g.said(t, "removed "+g.branch+" from ada/macports-ports"), "a finished cancel never settles twice")
}
