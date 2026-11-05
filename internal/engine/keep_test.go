package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// keepingProvider is leavingProvider whose clones a check asked to keep
// are marked kept, as the Tart provider marks them.
type keepingProvider struct {
	leavingProvider
	kept map[string]time.Time
	// failed are the clones kept for a failure, unasked.
	failed map[string]bool
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
		found[i].KeptAt, found[i].Failed = p.kept[found[i].Ref], p.failed[found[i].Ref]
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
		queued, err := e.QueueCheck(t.Context(), checks.QueueRequest{Branch: branch, Revision: revision, Plan: plan, Origin: model.OriginPerson})
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
	require.ErrorContains(t, err, run.Name()+" kept no clone; dockhand check --keep retains each Tart environment's clone")
	steps, err := e.ShellSteps(t.Context(), run)
	require.NoError(t, err)
	require.Empty(t, steps, "none kept, none named")
	kept, err := e.KeptClones(t.Context())
	require.NoError(t, err)
	require.Empty(t, kept)

	provider.kept[provider.left[0]] = time.Now()
	// The check's end names the shell that opens its clone, and the
	// branch's latest check that kept one is found, as shell's refusal of
	// a name that isn't a run's says it (field testing's curl +sectrust
	// notes, note 5).
	steps, err = e.ShellSteps(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, []refusal.Next{{Verb: refusal.VerbShell, Args: []string{run.Name()}}}, steps)
	kept, err = e.KeptClones(t.Context())
	require.NoError(t, err)
	require.Len(t, kept, 1)
	require.Equal(t, run.ID, kept[0].Run.ID)
	require.Equal(t, run.Branch, kept[0].Run.Branch)
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

// Automatic cleanup spares a clone kept for its failure seven days, where
// one kept as asked goes after a day (keep_failed).
func TestAFailedChecksKeptCloneLastsSevenDays(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &keepingProvider{kept: map[string]time.Time{}, failed: map[string]bool{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	_, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Len(t, provider.left, 1)
	clone := provider.left[0]
	keptAgo := func(ago time.Duration, failed bool) {
		provider.mu.Lock()
		provider.kept[clone], provider.failed[clone] = e.now().Add(-ago), failed
		provider.mu.Unlock()
	}
	spared := func() bool {
		t.Helper()
		planned, err := e.planLeftovers(t.Context(), session(t, e), true)
		require.NoError(t, err)
		for _, leftover := range planned {
			if leftover.Ref == clone {
				return leftover.Kept != ""
			}
		}
		return false
	}
	keptAgo(48*time.Hour, true)
	require.True(t, spared(), "two days into its seven")
	keptAgo(8*24*time.Hour, true)
	require.False(t, spared(), "past seven days")
	keptAgo(48*time.Hour, false)
	require.False(t, spared(), "one kept as asked goes after a day")
}

// A shell in a running check enters the guest its live attempt runs in,
// left running, says first what it leaves as no evidence, and records the
// entry on the run; --test is refused there, and a run with no live guest
// says so.
func TestAShellEntersARunningChecksGuest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &shellingProvider{keepingProvider: keepingProvider{kept: map[string]time.Time{}}}
	e.Providers = map[string]buildenv.Provider{tahoeArm.Provider: provider}
	run := queuedHarborRun(t, e, tahoeArm)

	_, err := e.Shell(t.Context(), ShellRequest{Run: run, Command: []string{"ls"}})
	require.ErrorContains(t, err, run.Name()+" is queued and has no running guest available for a shell yet")
	execution := model.GuestExecution{ID: "ex_live", Run: run.ID, Environment: tahoeArm, Attempt: 1, State: model.ExecutionRunning, ProviderRef: "dockhand-check-live-1", CreatedAt: e.now()}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.AddExecution(execution) }))

	_, err = e.Shell(t.Context(), ShellRequest{Run: run, Test: true})
	require.EqualError(t, err, "--test reruns the test phase in a kept clone; "+run.Name()+" is running")
	var notice string
	code, err := e.Shell(t.Context(), ShellRequest{Run: run, Command: []string{"ls", "-l"}, Notice: func(words string) { notice = words }})
	require.NoError(t, err)
	require.Equal(t, 7, code)
	require.Equal(t, run.Name()+" is building in dockhand-check-live-1; a shell here leaves what it's building, and any target after it, as not evidence (dockhand check again for that).", notice)
	require.Len(t, provider.shells, 1)
	require.True(t, provider.shells[0].Running)
	require.Equal(t, "dockhand-check-live-1", provider.shells[0].Ref)
	entered, err := e.Run(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, []model.Entry{{At: e.now(), Environment: execution.Environment, Command: "ls -l"}}, entered.Entered)
}

// refusingRemoval is a provider whose leftovers can't be removed.
type refusingRemoval struct{ keepingProvider }

func (*refusingRemoval) RemoveLeftover(context.Context, string) error {
	return errors.New("tart delete failed")
}

// A branch's own leftovers are what its checks left, never another's;
// clean --delete removes them before it forgets the record, and keeps the
// record where one can't be removed (field testing's note 13).
func TestForgettingABranchRemovesTheClonesItsChecksKept(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &keepingProvider{kept: map[string]time.Time{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Len(t, provider.left, 1)
	clone := provider.left[0]
	branch, err := e.Branch(t.Context(), run.Branch)
	require.NoError(t, err)

	own, err := e.BranchLeftovers(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Len(t, own, 1, "its own, never clone-elsewhere, no check's of this checkout")
	require.Equal(t, clone, own[0].Ref)
	none, err := e.BranchLeftovers(t.Context(), "branch_other")
	require.NoError(t, err)
	require.Empty(t, none, "another branch's clean doesn't take it")

	branch, err = e.Archive(t.Context(), branch, false)
	require.NoError(t, err)
	testsupport.Git(t, f.clone, "switch", "-q", "--discard-changes", "master")
	refusing := &refusingRemoval{keepingProvider: keepingProvider{leavingProvider: leavingProvider{left: []string{clone}}, kept: provider.kept}}
	e.Providers = map[string]buildenv.Provider{"command": refusing}
	plan, err := e.PlanForget(t.Context(), branch)
	require.NoError(t, err)
	require.Len(t, plan.Leftovers, 1)
	_, err = e.Forget(t.Context(), &plan)
	require.ErrorContains(t, err, "record is kept, since what its checks left wasn't all removed")
	_, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err, "the record stays, which alone claims the clone")

	e.Providers = map[string]buildenv.Provider{"command": provider}
	plan, err = e.PlanForget(t.Context(), branch)
	require.NoError(t, err)
	_, err = e.Forget(t.Context(), &plan)
	require.NoError(t, err)
	require.Contains(t, provider.removed, clone, "removed before the record went")
	require.True(t, plan.Leftovers[0].Done)
}

// A retry is the branch's next check, which removes the clone its earlier
// check kept, as a new check does: each retry of a failed Tart check left
// one more (Codex's workflow review of 1caa6c3d, finding 6, its probe).
func TestARetryRemovesTheCloneTheEarlierCheckKept(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &keepingProvider{kept: map[string]time.Time{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	usePorts(e, harborPorts())
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) (err error) { branch, err = r.Branch(revision.Branch); return err }))
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Keep: true})
	require.NoError(t, err)
	queued, err := e.QueueCheck(t.Context(), checks.QueueRequest{Branch: branch, Revision: revision, Plan: plan, Origin: model.OriginPerson})
	require.NoError(t, err)
	previous, err := e.Drive(t.Context(), session(t, e), queued.Run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, previous.State)
	require.Len(t, provider.left, 1)
	old := provider.left[0]
	retry, err := e.Retry(t.Context(), previous)
	require.NoError(t, err)
	done, err := e.Drive(t.Context(), session(t, e), retry.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, done.State)
	require.NotContains(t, provider.left, old, "a retry is the branch's next check, which should remove its earlier retained clone")
	require.Len(t, provider.left, 1)
}
