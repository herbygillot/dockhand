package engine

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// shellKeptProvider is a leavingProvider that lists one of its environments
// as kept for dockhand shell.
type shellKeptProvider struct {
	*leavingProvider
	kept string
}

func (p shellKeptProvider) Leftovers(ctx context.Context) ([]buildenv.Leftover, error) {
	found, err := p.leavingProvider.Leftovers(ctx)
	for i := range found {
		if found[i].Ref == p.kept {
			found[i].KeptAt = time.Now()
		}
	}
	return found, err
}

// reclaimFixture drives one check that leaves its environment behind, as
// a process killed outright leaves its Tart clone running.
func reclaimFixture(t *testing.T) (*Engine, *leavingProvider, model.Run, string) {
	t.Helper()
	f := setup(t)
	e := f.open(t)
	provider := &leavingProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Len(t, provider.left, 1)
	return e, provider, run, provider.left[0]
}

func eventCount(t *testing.T, e *Engine) int {
	t.Helper()
	var events []model.Event
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) (err error) {
		events, err = r.Events(0, 100000, "")
		return err
	}))
	return len(events)
}

// A check waiting for a slot removes what cleanup would remove of the VMs
// running in it, and nothing else: an environment no check of this
// checkout made, one whose check a live process drives, and one kept for
// dockhand shell all stay, and finding nothing to remove writes nothing
// (#239).
func TestAWaitReclaimsOnlyWhatCleanupWouldRemove(t *testing.T) {
	t.Parallel()
	e, provider, run, clone := reclaimFixture(t)
	judge := session(t, e)

	said, err := e.reclaim(t.Context(), judge, "command", []string{"clone-elsewhere"})
	require.NoError(t, err)
	require.Empty(t, said, "no check of this checkout made it")

	driver := session(t, e)
	lease, err := driver.Acquire(t.Context(), checks.RunResource(run.ID))
	require.NoError(t, err)
	before := eventCount(t, e)
	said, err = e.reclaim(t.Context(), judge, "command", []string{clone})
	require.NoError(t, err)
	require.Empty(t, said, "a live process drives its check")
	require.Empty(t, provider.removed)
	require.Equal(t, before, eventCount(t, e), "a wait that removes nothing writes nothing")
	require.NoError(t, driver.Release(t.Context(), lease))

	e.Providers["command"] = shellKeptProvider{provider, clone}
	said, err = e.reclaim(t.Context(), judge, "command", []string{clone})
	require.NoError(t, err)
	require.Empty(t, said, "kept for dockhand shell")
	require.Empty(t, provider.removed)

	e.Providers["command"] = provider
	said, err = e.reclaim(t.Context(), judge, "command", []string{clone})
	require.NoError(t, err)
	require.Equal(t, []string{"removed clone " + clone + ", left by check-1: its check is no longer running"}, said)
	require.Equal(t, []string{clone}, provider.removed)
}

// Two waiters may reclaim the same VM at once: the check's lease decides,
// and the one that finds it gone says nothing and fails nothing (#239).
func TestTwoWaitersReclaimOneVMOnce(t *testing.T) {
	t.Parallel()
	e, provider, _, clone := reclaimFixture(t)
	var wg sync.WaitGroup
	results := make([][]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		judge := session(t, e)
		wg.Go(func() { results[i], errs[i] = e.reclaim(t.Context(), judge, "command", []string{clone}) })
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Len(t, append(results[0], results[1]...), 1, "said once: %q", results)
	require.Equal(t, []string{clone}, provider.removed)
}

// A removal that finds its environment already gone, as the second of two
// waiters does once it has the lease, is neither done nor an error.
func TestALeftoverAlreadyGoneIsNotAnError(t *testing.T) {
	t.Parallel()
	e, provider, _, clone := reclaimFixture(t)
	cleaner := session(t, e)
	plan, err := e.PlanLeftovers(t.Context(), cleaner)
	require.NoError(t, err)
	_, err = e.RemoveLeftovers(t.Context(), cleaner, slices.Clone(plan))
	require.NoError(t, err)
	again, err := e.RemoveLeftovers(t.Context(), cleaner, plan)
	require.NoError(t, err)
	for _, leftover := range again {
		if leftover.Ref == clone {
			require.False(t, leftover.Done)
			require.Equal(t, "already removed", leftover.Kept)
		}
	}
	require.Equal(t, []string{clone}, provider.removed)
}
