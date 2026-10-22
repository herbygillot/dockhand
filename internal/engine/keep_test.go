package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// keepingProvider is leavingProvider whose clones a check asked to keep
// are marked kept, as the Tart provider marks them.
type keepingProvider struct {
	leavingProvider
	kept map[string]time.Time
}

func (p *keepingProvider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	if err := p.leavingProvider.Execute(ctx, job, build); err != nil {
		return err
	}
	if job.Plan.Keep {
		p.mu.Lock()
		p.kept["clone-"+string(job.Execution.ID)] = time.Now()
		p.mu.Unlock()
	}
	return nil
}

func (p *keepingProvider) Leftovers(ctx context.Context) ([]buildenv.Leftover, error) {
	found, err := p.leavingProvider.Leftovers(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range found {
		found[i].KeptAt = p.kept[found[i].Ref]
	}
	return found, err
}

// A clone a check kept for dockhand shell is spared by automatic cleanup
// for a day, unless space is low; clean removes it, and so does the
// branch's next check.
func TestAKeptCloneLastsUntilCleanOrTheNextCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &keepingProvider{kept: map[string]time.Time{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	e.PortReader = harborPorts()
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) (err error) {
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	check := func() {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Keep: true})
		require.NoError(t, err)
		queued, err := e.QueueCheck(t.Context(), QueueRequest{Branch: branch, Revision: revision, Plan: plan, Origin: model.OriginPerson})
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.Run.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
	}
	check()
	require.Len(t, provider.left, 1)
	kept := provider.left[0]

	report, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Contains(t, provider.left, kept, "automatic cleanup spares it")
	for _, leftover := range report.Leftovers {
		if leftover.Ref == kept {
			require.Contains(t, leftover.Kept, "kept for dockhand shell until ")
		}
	}
	_, err = e.cleanup(t.Context(), session(t, e), 7*24*time.Hour, false)
	require.NoError(t, err)
	require.NotContains(t, provider.left, kept, "where space is low, it goes")

	check()
	again := provider.left[len(provider.left)-1]
	planned, err := e.PlanLeftovers(t.Context(), session(t, e))
	require.NoError(t, err)
	for _, leftover := range planned {
		if leftover.Ref == again {
			require.Empty(t, leftover.Kept, "clean removes it")
		}
	}
	check()
	require.NotContains(t, provider.left, again, "the branch's next check removes it")
}
