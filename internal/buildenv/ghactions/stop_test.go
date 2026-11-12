package ghactions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// stopAPI is GitHub as a stop meets it: the runs listed on the check's
// branch, each queued, running or completed. A canceled run completes
// after holding more lists, or never where hold is negative.
type stopAPI struct {
	API
	mu       sync.Mutex
	runs     []Run
	canceled []int64
	hold     int
	held     map[int64]int
}

func (s *stopAPI) Runs(_ context.Context, _, _, _ string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, run := range s.runs {
		if run.Status == "cancelling" {
			if s.held[run.ID] >= s.hold && s.hold >= 0 {
				s.runs[i].Status, s.runs[i].Conclusion = "completed", "cancelled"
			}
			s.held[run.ID]++
		}
	}
	return append([]Run(nil), s.runs...), nil
}

func (s *stopAPI) Run(ctx context.Context, repository string, id int64) (Run, error) {
	runs, _ := s.Runs(ctx, repository, "", "")
	for _, run := range runs {
		if run.ID == id {
			return run, nil
		}
	}
	return Run{}, fmt.Errorf("no run %d", id)
}

func (s *stopAPI) Cancel(_ context.Context, _ string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil {
		s.held = map[int64]int{}
	}
	s.canceled = append(s.canceled, id)
	for i, run := range s.runs {
		if run.ID == id && run.Status != "completed" {
			s.runs[i].Status = "cancelling"
		}
	}
	return nil
}

func (s *stopAPI) add(id int64, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, Run{ID: id, Attempt: 1, Status: status, URL: fmt.Sprintf("https://github.com/ada/macports-ports/actions/runs/%d", id)})
}

// freeLock is a launch lock nothing holds; held one a stop can't have.
type freeLock struct{ held error }

func (l freeLock) Hold(context.Context, time.Duration) (func(), error) {
	if l.held != nil {
		return nil, l.held
	}
	return func() {}, nil
}

const stopBranch = "dockhand-check/1111111111aa-abcdefghijklmnop"

func push(id string, before int64) launchNote {
	return launchNote{ID: id, Repository: "ada/macports-ports", Workflow: Workflow, Branch: stopBranch, Commit: "1111111111aa1111111111111111111111111111", Check: "run_abcdefghijklmnop", Action: launchPush, Before: before}
}

func attempts(notes ...launchNote) []model.GuestExecution {
	var executions []model.GuestExecution
	for i, note := range notes {
		executions = append(executions, model.GuestExecution{ID: model.ExecutionID(fmt.Sprint("github_", i)), Attempt: i + 1, Launch: note.String()})
	}
	return executions
}

func stopper(api *stopAPI) *Provider {
	return &Provider{API: api, Sleep: func(context.Context, time.Duration) error { return nil },
		Fork: func(context.Context) (buildenv.Fork, error) {
			return buildenv.Fork{Repository: "ada/macports-ports", PushURL: "https://github.com/ada/macports-ports.git"}, nil
		}}
}

func stop(t *testing.T, api *stopAPI, executions []model.GuestExecution) error {
	t.Helper()
	request := buildenv.StopRequest{Attempts: executions, Lock: freeLock{}}
	if len(executions) > 0 {
		request.Execution = executions[0]
	}
	left, err := stopper(api).Stop(t.Context(), request)
	require.Empty(t, left)
	return err
}

// A cancel of a GitHub check is ended only when every run listed on its
// branch has completed and every launch of the check is answered for,
// each push and each rerun counted once by its own note (the lifecycle
// design's "GitHub", with Codex's A7).
func TestAGitHubStopAnswersForEveryLaunch(t *testing.T) {
	t.Parallel()

	// Nothing noted and nothing recorded: nothing was pushed.
	require.NoError(t, stop(t, &stopAPI{}, []model.GuestExecution{{ID: "github_1"}}))

	// A push with no run listed is not known, however long ago.
	api := &stopAPI{}
	require.ErrorContains(t, stop(t, api, attempts(push("p1", 0))), "GitHub lists no run yet for 1 push of "+stopBranch+" it accepted, and one can still start")

	// A run that appears is canceled, read until it completes, and ends it.
	api.add(7, "in_progress")
	require.NoError(t, stop(t, api, attempts(push("p1", 0))))
	require.Equal(t, []int64{7}, api.canceled)

	// A run that doesn't complete within the bound is not known, named.
	stuck := &stopAPI{hold: -1}
	stuck.add(9, "queued")
	require.ErrorContains(t, stop(t, stuck, attempts(push("p1", 0))), "GitHub was asked to cancel https://github.com/ada/macports-ports/actions/runs/9, and it is still cancelling")

	// Two pushes that noted the same newest run need two runs newer
	// than it: one completed run answers for one (A7).
	two := &stopAPI{}
	two.add(11, "completed")
	require.ErrorContains(t, stop(t, two, attempts(push("p1", 10), push("p2", 10))), "no run yet for 1 push")
	two.add(12, "completed")
	require.NoError(t, stop(t, two, attempts(push("p1", 10), push("p2", 10))))

	// An earlier run, completed, says nothing of a later push that noted
	// it: the later push's own run isn't listed yet.
	later := &stopAPI{}
	later.add(5, "completed")
	require.Error(t, stop(t, later, attempts(push("p1", 0), push("p2", 5))))

	// A matrix's cells share one launch, which one run answers for.
	shared := &stopAPI{}
	shared.add(3, "completed")
	require.NoError(t, stop(t, shared, attempts(push("p1", 0), push("p1", 0))))

	// A push known to have started nothing needs no run.
	refused := push("p1", 0)
	refused.Nothing = "git push reported the branch rejected: ! ... [remote rejected]"
	require.NoError(t, stop(t, &stopAPI{}, attempts(refused)))

	// Two reruns that noted the same attempt need the run two attempts on.
	rerun := func(id string, attempt int) launchNote {
		note := push(id, 0)
		note.Action, note.Run, note.Attempt, note.Before = launchRerun, 3, attempt, 0
		return note
	}
	reruns := &stopAPI{}
	reruns.add(3, "completed")
	reruns.runs[0].Attempt = 2
	require.ErrorContains(t, stop(t, reruns, attempts(push("p1", 0), rerun("r1", 1), rerun("r2", 1))), "run 3 hasn't shown the 2 attempts asked of it")
	reruns.runs[0].Attempt = 3
	require.NoError(t, stop(t, reruns, attempts(push("p1", 0), rerun("r1", 1), rerun("r2", 1))))

	// Attaches started nothing: several can point at one run, and at the
	// run a push answers for.
	attach := func(id string) launchNote {
		note := push(id, 0)
		note.Action, note.Run, note.Before = launchAttach, 3, 0
		return note
	}
	require.NoError(t, stop(t, reruns, attempts(push("p1", 0), attach("a1"), attach("a2"))))
}

// What a stop can't read or mayn't touch is said, never taken as ended:
// a lock still held, another repository, a reference that isn't a run's,
// and an attempt from before notes, whose run is canceled and read.
func TestAGitHubStopSaysWhatItCantSeeEnd(t *testing.T) {
	t.Parallel()
	api := &stopAPI{}
	_, err := stopper(api).Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(push("p1", 0)), Lock: freeLock{held: errors.New("its launch lock is still held")}})
	require.ErrorContains(t, err, "its launch lock is still held")

	elsewhere := push("p1", 0)
	elsewhere.Repository = "someone/macports-ports"
	require.ErrorContains(t, stop(t, api, attempts(elsewhere)), "pushed to someone/macports-ports, not your fork ada/macports-ports")

	require.ErrorContains(t, stop(t, api, []model.GuestExecution{{ID: "github_1", ProviderRef: "a clone"}}), `"a clone" isn't a workflow run's URL`)
	require.ErrorContains(t, stop(t, api, []model.GuestExecution{{ID: "github_1", ProviderRef: "https://github.com/someone/macports-ports/actions/runs/4"}}), "not your fork")

	old := &stopAPI{}
	old.add(4, "in_progress")
	require.NoError(t, stop(t, old, []model.GuestExecution{{ID: "github_1", ProviderRef: "https://github.com/ada/macports-ports/actions/runs/4"}}))
	require.Equal(t, []int64{4}, old.canceled)
}

// An attempt the migration marked as unfinished before launches were noted
// is not known where it recorded no run, since a push may have started one;
// one that recorded a run's URL has that run canceled and read (Codex's
// review of #193 steps 5 and 6, finding 3).
func TestAGitHubAttemptFromBeforeNotesIsNotKnownWithoutARun(t *testing.T) {
	t.Parallel()
	err := stop(t, &stopAPI{}, []model.GuestExecution{{ID: "github_old", Attempt: 1, State: model.ExecutionRunning, Launch: model.LaunchUnrecorded}})
	require.ErrorContains(t, err, "attempt 1 was started before dockhand noted its launches, and recorded no run")
	old := &stopAPI{}
	old.add(4, "in_progress")
	require.NoError(t, stop(t, old, []model.GuestExecution{{ID: "github_old", Attempt: 1, Launch: model.LaunchUnrecorded, ProviderRef: "https://github.com/ada/macports-ports/actions/runs/4"}}))
}

// A launch whose runs can't be named, one from before notes with no run or
// a note that can't be read, is not known, and never keeps the runs that
// can be named from being canceled: every stop asked all of the
// provider's attempts, and returned at the first such one with a known
// run still in progress (the confirmation of #193's fixes, note 1).
func TestALaunchThatCantBeNamedDoesntKeepAKnownRunRunning(t *testing.T) {
	t.Parallel()
	known := model.GuestExecution{ID: "github_known", Attempt: 2, Launch: model.LaunchUnrecorded, ProviderRef: "https://github.com/ada/macports-ports/actions/runs/4"}
	for _, unnamed := range []struct {
		attempt model.GuestExecution
		said    string
	}{
		{model.GuestExecution{ID: "github_unknown", Attempt: 1, Launch: model.LaunchUnrecorded}, "attempt 1 was started before dockhand noted its launches, and recorded no run"},
		{model.GuestExecution{ID: "github_unread", Attempt: 1, Launch: "{not a note"}, "attempt 1's note of what it launched can't be read"},
	} {
		api := &stopAPI{}
		api.add(4, "in_progress")
		err := stop(t, api, []model.GuestExecution{unnamed.attempt, known})
		require.ErrorContains(t, err, unnamed.said)
		require.Equal(t, []int64{4}, api.canceled, "%s: the known run is canceled all the same", unnamed.said)
		require.NotContains(t, err.Error(), "still", "and read until it completed")

		// Alone, it's not known, and nothing is canceled.
		alone := &stopAPI{}
		require.ErrorContains(t, stop(t, alone, []model.GuestExecution{unnamed.attempt}), unnamed.said)
		require.Empty(t, alone.canceled)
	}
}
