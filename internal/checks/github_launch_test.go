package checks

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// ada is the GitHub account the tests write as.
type ada struct{}

func (ada) Current(context.Context) (github.Identity, error) {
	return github.Identity{Account: "ada"}, nil
}

// stoppedShortActions is a fork whose workflow runs, which lists no run
// before the push and then the push's run, completed and cancelled, so
// the provider asks to run it again; listed is called as it first lists
// that run.
type stoppedShortActions struct {
	ghactions.API
	mu       sync.Mutex
	lists    int
	listed   func()
	reruns   int
	canceled []int64
}

func (a *stoppedShortActions) WorkflowState(context.Context, string) (string, error) {
	return "active", nil
}

func (a *stoppedShortActions) Runs(context.Context, string, string, string) ([]ghactions.Run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lists++
	switch a.lists {
	case 1:
		return nil, nil
	case 2:
		if a.listed != nil {
			a.listed()
		}
	}
	return []ghactions.Run{{ID: 5, Attempt: 1, Status: "completed", Conclusion: "cancelled", URL: "https://github.com/ada/macports-ports/actions/runs/5"}}, nil
}

func (a *stoppedShortActions) Jobs(context.Context, string, int64, int) ([]ghactions.RunnerJob, error) {
	return nil, nil
}

func (a *stoppedShortActions) Rerun(context.Context, string, int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reruns++
	return nil
}

// RecentRuns is read only after a rerun started, which nothing here
// should.
func (a *stoppedShortActions) RecentRuns(context.Context, string) ([]ghactions.Run, error) {
	return nil, errors.New("a rerun was followed")
}

func (a *stoppedShortActions) Cancel(_ context.Context, _ string, id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.canceled = append(a.canceled, id)
	return nil
}

// A GitHub check whose run is to be run again once its cancel is recorded
// runs nothing again: the rerun goes through the launch call, which reads
// the cancel under the attempt's lock, and the attempt's note stays its
// push, whose run GitHub's stop finds completed, so the cancel finishes
// (the lifecycle design's "Running a finished GitHub run again starts
// work as a push does").
func TestAGitHubRerunAskedWhileACancelIsRecordedRunsNothingAgain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone, fork := filepath.Join(root, "clone"), filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, clone, "rev-parse", "HEAD")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)

	c := service(t)
	c.Poll = time.Hour
	api := &stoppedShortActions{}
	c.Providers = map[string]buildenv.Provider{buildenv.GitHub: &ghactions.Provider{Repo: repo, API: api, Identity: ada{},
		Sleep: func(context.Context, time.Duration) error { return nil },
		Fork: func(context.Context) (buildenv.Fork, error) {
			return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
		}}}
	environment := model.Environment{Provider: buildenv.GitHub, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	run := c.queuedAt(t, "jq-github-rerun", model.ObjectID(commit), environment)
	api.listed = func() {
		require.NoError(t, c.Store.Update(context.Background(), c.Repository, func(tx store.Tx) error {
			recorded, err := tx.Run(run.ID)
			if err != nil {
				return err
			}
			recorded.CancelRequested = &at
			return tx.UpdateRun(recorded)
		}))
	}
	canceled, err := c.Drive(t.Context(), c.session(t), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State, canceled.Detail)
	require.Zero(t, api.reruns, "nothing was run again")
	executions := c.executions(t, run)
	require.Len(t, executions, 1)
	require.Contains(t, executions[0].Launch, `"action":"push"`, "the note stays the push")
	require.NotContains(t, executions[0].Launch, `"nothing"`)
}
