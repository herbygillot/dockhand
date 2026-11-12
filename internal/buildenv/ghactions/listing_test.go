package ghactions

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// One poll of runs older than the listing's page costs one listing and
// each run's own read, where each missing run listed again (Codex's
// review of #197, finding 2).
func TestOldRunsShareOneListingAPoll(t *testing.T) {
	t.Parallel()
	api := &listingAPI{listed: []Run{{ID: 1000, Status: "completed"}}}
	p := &Provider{API: api}
	for id := int64(1); id <= 20; id++ {
		_, err := p.current(t.Context(), "ada/macports-ports", id)
		require.NoError(t, err)
	}
	require.Equal(t, 1, api.listings, "one listing for the poll")
	require.Equal(t, 20, api.runs, "and each run's own read")
}

// A listing is served only within the poll it was read in: past it, a run
// not yet read from it takes a new one.
func TestAListingIsServedOnlyWithinItsPoll(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	api := &listingAPI{listed: []Run{{ID: 1, Status: "in_progress"}, {ID: 2, Status: "in_progress"}}}
	p := &Provider{API: api, now: func() time.Time { return at }}
	_, err := p.current(t.Context(), "ada/macports-ports", 1)
	require.NoError(t, err)
	at = at.Add(poll)
	_, err = p.current(t.Context(), "ada/macports-ports", 2)
	require.NoError(t, err)
	require.Equal(t, 2, api.listings, "a listing a poll old isn't served")
}

// rerunAPI is a run whose attempt 2 failed, which a listing read for
// another check still shows at its successful attempt 1. Its rerun starts
// attempt 3, which completes at the second listing after it.
type rerunAPI struct {
	matrixAPI
	listed           []Run
	listings, reruns int
	since            int
	readAttempts     []int
}

func (a *rerunAPI) RecentRuns(context.Context, string) ([]Run, error) {
	a.listings++
	if a.reruns > 0 {
		a.since++
		if a.since >= 2 {
			return []Run{{ID: 1, Attempt: 3, Status: "completed", Conclusion: "success", URL: "https://github.com/ada/macports-ports/actions/runs/1"}}, nil
		}
	}
	return a.listed, nil
}
func (*rerunAPI) WorkflowState(context.Context, string) (string, error) { return "active", nil }
func (*rerunAPI) Runs(context.Context, string, string, string) ([]Run, error) {
	return []Run{{ID: 1, Attempt: 2, Status: "completed", Conclusion: "failure", URL: "https://github.com/ada/macports-ports/actions/runs/1"}}, nil
}
func (a *rerunAPI) Rerun(context.Context, string, int64) error {
	a.reruns++
	a.listed = []Run{{ID: 1, Attempt: 3, Status: "in_progress", URL: "https://github.com/ada/macports-ports/actions/runs/1"}}
	return nil
}
func (a *rerunAPI) Jobs(_ context.Context, _ string, _ int64, attempt int) ([]RunnerJob, error) {
	a.readAttempts = append(a.readAttempts, attempt)
	return []RunnerJob{{ID: 1, Name: "macos-15", Conclusion: "success", RunnerName: "runner", Labels: []string{"macos-15"}}}, nil
}

type rerunBuild struct{ recordingBuild }

func (*rerunBuild) Launch(_ string, start func(context.Context) (string, error)) error {
	_, err := start(context.Background())
	return err
}
func (*rerunBuild) Refer(string) error { return nil }
func (*rerunBuild) Canceled() bool     { return false }

// A rerun is answered only by an attempt past the one it was launched
// from: a listing read before it, for another check, that still holds the
// run's older attempt is dropped, and the older attempt, a pass, is never
// read as the rerun's result (Codex's review of #197, finding 1, which
// recorded attempt 1's pass for a rerun of attempt 2).
func TestARerunIsAnsweredOnlyByTheAttemptItStarted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone, fork := filepath.Join(root, "clone"), filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, clone, "rev-parse", "HEAD")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 3}
	branch := buildenv.CheckBranch(commit, run.ID)
	testsupport.Git(t, clone, "push", "-q", fork, commit+":refs/heads/"+branch)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)
	api := &rerunAPI{listed: []Run{
		{ID: 1, Attempt: 1, Status: "completed", Conclusion: "success", URL: "https://github.com/ada/macports-ports/actions/runs/1"},
		{ID: 2, Attempt: 1, Status: "in_progress"},
	}}
	p := &Provider{Repo: repo, API: api,
		Fork: func(context.Context) (buildenv.Fork, error) {
			return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
		},
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	// Another check read a shared listing while run 1 was at attempt 1.
	_, err = p.current(t.Context(), "ada/macports-ports", 2)
	require.NoError(t, err)
	pushed := newLaunch("ada/macports-ports", branch, buildenv.Job{Run: run, Commit: commit}, launchPush)
	job := buildenv.Job{Run: run, Directory: t.TempDir(), Commit: commit, Launched: []string{pushed.String()},
		Execution: model.GuestExecution{Attempt: 2},
		Targets:   []buildenv.Target{{PlanTarget: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}}},
	}
	require.NoError(t, p.Execute(t.Context(), job, &rerunBuild{}))
	require.Equal(t, 1, api.reruns)
	require.Equal(t, []int{3}, api.readAttempts, "the attempt the rerun started, never the older one listed before it")
}

// queuedMatrixAPI is a run in progress whose macOS 15 job built and whose
// macOS 26 job never got a runner, until it's canceled.
type queuedMatrixAPI struct {
	rerunAPI
	cancelled bool
}

func (a *queuedMatrixAPI) Runs(ctx context.Context, repository, _, _ string) ([]Run, error) {
	return a.RecentRuns(ctx, repository)
}
func (a *queuedMatrixAPI) RecentRuns(context.Context, string) ([]Run, error) {
	run := Run{ID: 1, Attempt: 1, Status: "in_progress", URL: "https://github.com/ada/macports-ports/actions/runs/1"}
	if a.cancelled {
		run.Status, run.Conclusion = "completed", "cancelled"
	}
	return []Run{run}, nil
}
func (a *queuedMatrixAPI) Cancel(context.Context, string, int64) error {
	a.cancelled = true
	return nil
}
func (*queuedMatrixAPI) Jobs(context.Context, string, int64, int) ([]RunnerJob, error) {
	return matrixAPI{}.Jobs(context.Background(), "", 0, 0)
}

// A run's time counts from its first job, as the usage page says: a matrix
// job still waiting for a runner counts while another builds, so the run
// is canceled at the bound, and the job that never started is named
// (Codex's review of #197, finding 3, the counting kept and said).
func TestAMatrixJobWaitingForARunnerCountsAgainstTheBound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone, fork := filepath.Join(root, "clone"), filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, clone, "rev-parse", "HEAD")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 3}
	branch := buildenv.CheckBranch(commit, run.ID)
	testsupport.Git(t, clone, "push", "-q", fork, commit+":refs/heads/"+branch)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)
	api := &queuedMatrixAPI{}
	sleeps := 0
	p := &Provider{Repo: repo, API: api, BuildTimeout: 2 * poll,
		Fork: func(context.Context) (buildenv.Fork, error) {
			return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
		},
		Sleep: func(context.Context, time.Duration) error { sleeps++; return nil },
	}
	pushed := newLaunch("ada/macports-ports", branch, buildenv.Job{Run: run, Commit: commit}, launchPush)
	job := buildenv.Job{Run: run, Launched: []string{pushed.String()}, Directory: t.TempDir(), Commit: commit,
		Targets: []buildenv.Target{{PlanTarget: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}}},
	}
	err = p.Execute(t.Context(), job, &rerunBuild{})
	require.True(t, api.cancelled, "the waiting job's time counted, and the run was canceled at the bound")
	require.Equal(t, 3, sleeps)
	require.ErrorContains(t, err, "the run exceeded the 1m0s build timeout")
	require.ErrorContains(t, err, "macos-26's job before any runner took it", "the job that never started is named")
	require.ErrorContains(t, err, "the matrix is incomplete")
}
