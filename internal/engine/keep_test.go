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
	usePorts(e, harborPorts())
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

// Shell starts the kept clone of the check, with the first port it
// changed, and says when there's none to start.
func TestShellStartsTheClonesACheckKept(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &shellingProvider{keepingProvider: keepingProvider{kept: map[string]time.Time{}}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)

	_, err = e.Shell(t.Context(), ShellRequest{Run: run, Command: []string{"ls"}})
	require.ErrorContains(t, err, run.Name()+" kept no clone; dockhand check --keep keeps each environment's")

	provider.kept[provider.left[0]] = time.Now()
	code, err := e.Shell(t.Context(), ShellRequest{Run: run, Command: []string{"ls"}})
	require.NoError(t, err)
	require.Equal(t, 7, code)
	require.Equal(t, buildenv.ShellJob{Ref: provider.left[0], Portfile: provider.shells[0].Portfile, Command: []string{"ls"}}, provider.shells[0])
	require.NotEmpty(t, provider.shells[0].Portfile)
	_, err = e.Shell(t.Context(), ShellRequest{Run: run, Port: "nosuch"})
	require.ErrorContains(t, err, "didn't build nosuch")
	_, err = e.Shell(t.Context(), ShellRequest{Run: run, Release: "sequoia"})
	require.ErrorContains(t, err, "kept no clone on sequoia")
}

// shellingProvider is keepingProvider whose kept clones take a shell,
// which exits 7.
type shellingProvider struct {
	keepingProvider
	shells []buildenv.ShellJob
}

func (p *shellingProvider) Shell(_ context.Context, job buildenv.ShellJob) (int, error) {
	p.shells = append(p.shells, job)
	return 7, nil
}

// A drain that found the queue empty reads it again after letting go of
// the lead, and drains a check queued meanwhile, which check -d's drain
// would otherwise leave.
func TestADrainRunsWhatWasQueuedAsItEnded(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &leavingProvider{}}
	var late model.Run
	var said []string
	err := e.Serve(t.Context(), session(t, e), ServeOptions{Drain: true, Poll: 10 * time.Millisecond, Say: func(line string) {
		said = append(said, line)
		if line == "serve: the queue is empty" && late.ID == "" {
			late = queuedHarborRun(t, e, tahoeArm)
		}
	}})
	require.NoError(t, err)
	require.Contains(t, said, "serve: "+late.Name()+" was queued as the drain ended; draining again")
	finished, err := e.Run(t.Context(), late.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, finished.State, finished.Detail)
}
