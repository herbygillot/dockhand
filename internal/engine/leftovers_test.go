package engine

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// leavingProvider passes every target, naming each execution's environment
// on it, and leaves the environment behind, as a process that died would.
// Beside them it lists one no check made.
type leavingProvider struct {
	mu      sync.Mutex
	left    []string
	removed []string
}

func (p *leavingProvider) Name() string { return "command" }

func (p *leavingProvider) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	ref := "clone-" + string(job.Execution.ID)
	if err := build.Refer(ref); err != nil {
		return err
	}
	p.mu.Lock()
	p.left = append(p.left, ref)
	p.mu.Unlock()
	for _, target := range job.Targets {
		if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone}); err != nil {
			return err
		}
	}
	return nil
}

func (p *leavingProvider) Leftovers(context.Context) ([]buildenv.Leftover, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var found []buildenv.Leftover
	for _, ref := range append(slices.Clone(p.left), "clone-elsewhere") {
		found = append(found, buildenv.Leftover{Ref: ref, What: "clone " + ref})
	}
	return found, nil
}

func (p *leavingProvider) RemoveLeftover(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.left = slices.DeleteFunc(p.left, func(left string) bool { return left == ref })
	p.removed = append(p.removed, ref)
	return nil
}

// What a check's environment left is removed once no process drives the
// check, under the check's lease; one a live process drives, even after
// the plan was made, is kept, and so is one no check of this checkout made.
func TestLeftoversGoOnlyWhenNoProcessDrivesTheirCheck(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &leavingProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Len(t, provider.left, 1)
	clone := provider.left[0]

	cleaner := session(t, e)
	plan, err := e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	byRef := map[string]Leftover{}
	for _, leftover := range plan {
		byRef[leftover.Ref] = leftover
	}
	require.Equal(t, "no check of this checkout made it", byRef["clone-elsewhere"].Kept)
	require.Nil(t, byRef["clone-elsewhere"].Run)
	require.Empty(t, byRef[clone].Kept, "its check ended, and no process drives it")
	require.Equal(t, run.ID, byRef[clone].Run.ID)
	require.Equal(t, "command", byRef[clone].Provider)

	// A process takes the check up between the plan and the removal.
	driver := session(t, e)
	lease, err := driver.Acquire(t.Context(), RunResource(run.ID))
	require.NoError(t, err)
	done, err := e.RemoveLeftovers(t.Context(), cleaner, plan)
	require.NoError(t, err)
	require.Empty(t, provider.removed)
	for _, leftover := range done {
		if leftover.Ref == clone {
			require.Equal(t, "check-1 is running", leftover.Kept)
		}
	}
	plan, err = e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	for _, leftover := range plan {
		if leftover.Ref == clone {
			require.Equal(t, "check-1 is running", leftover.Kept)
		}
	}

	require.NoError(t, driver.Release(t.Context(), lease))
	plan, err = e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	done, err = e.RemoveLeftovers(t.Context(), cleaner, plan)
	require.NoError(t, err)
	require.Equal(t, []string{clone}, provider.removed, "only the check's own, never another's")
	removed := slices.IndexFunc(done, func(l Leftover) bool { return l.Ref == clone })
	require.True(t, done[removed].Done)

	var events []model.Event
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		events, err = r.Events(0, 1000)
		return err
	}))
	require.True(t, slices.ContainsFunc(events, func(event model.Event) bool {
		return event.Kind == "cleanup" && event.Run == run.ID && event.Message == "removed clone "+clone+", left by check-1"
	}), "the removal is journaled on the check")
}

// serve's daily cleanup removes what checks left, as clean does.
func TestCleanupRemovesWhatChecksLeft(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	provider := &leavingProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	_, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	clone := provider.left[0]

	report, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{clone}, provider.removed)
	require.Equal(t, 1, report.Removed(), "the clone, and not the one no check of this checkout made")
}
