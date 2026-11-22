package clean

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// leavingBuilder passes every target, names its environment on the
// execution, and leaves the environment behind, as a process that died
// would. Beside them it lists one no check of this checkout made.
type leavingBuilder struct {
	mu     sync.Mutex
	left   []string
	keptAt map[string]time.Time
	failed map[string]bool
}

func (p *leavingBuilder) Name() string { return "command" }

func (p *leavingBuilder) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	ref := "vm-" + string(job.Execution.ID)
	if err := build.Refer(ref); err != nil {
		return err
	}
	p.mu.Lock()
	p.left = append(p.left, ref)
	p.mu.Unlock()
	for _, target := range job.Targets {
		if err := build.Record(buildenv.Reported(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone})); err != nil {
			return err
		}
	}
	return nil
}

func (p *leavingBuilder) Leftovers(context.Context) ([]buildenv.Leftover, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var found []buildenv.Leftover
	for _, ref := range append(slices.Clone(p.left), "vm-elsewhere") {
		found = append(found, buildenv.Leftover{Ref: ref, What: "VM " + ref, KeptAt: p.keptAt[ref], Failed: p.failed[ref]})
	}
	return found, nil
}

func (p *leavingBuilder) keep(ref string, at time.Time, failed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keptAt == nil {
		p.keptAt = map[string]time.Time{}
		p.failed = map[string]bool{}
	}
	p.keptAt[ref], p.failed[ref] = at, failed
}

func (p *leavingBuilder) RemoveLeftover(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.left = slices.DeleteFunc(p.left, func(left string) bool { return left == ref })
	return nil
}

// clean shows what checks left in providers beside the branches, removes
// what a check of this checkout left once no process runs it, and keeps
// what it can't vouch for.
func TestCleanRemovesWhatChecksLeft(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	require.Len(t, builder.left, 1)
	vm := builder.left[0]

	out, _, err := cmdtest.Dockhand(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err), "a script's clean without --yes removes nothing, and refuses")
	require.Equal(t, "Left by checks\n"+
		"  remove   VM "+vm+", left by check-1\n"+
		"  keep     VM vm-elsewhere: no check of this checkout made it\n", out)

	result, err := cmdtest.JSONOf(t, "clean")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Equal(t, vm, cmdtest.Dig(t, result.Result, "leftovers", 0, "ref"))
	require.Equal(t, "check-1", cmdtest.Dig(t, result.Result, "leftovers", 0, "check"))
	require.Equal(t, false, cmdtest.Dig(t, result.Result, "leftovers", 0, "removed"))

	out, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "\nRemoved 1 thing a check left.\n", "one line, once done")
	require.Empty(t, builder.left)

	out, _, err = cmdtest.Dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Left by checks\n  keep     VM vm-elsewhere: no check of this checkout made it\nNothing to remove.\n", out)
}

func TestCleanKeptWithoutSelectorRunsExpiredCacheCleanup(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
}

func TestPlainCleanKeepsAskedCloneAndBranchCleanRemovesIt(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	clone := builder.left[0]
	builder.keep(clone, time.Now(), false)

	out, _, err := cmdtest.Dockhand(t, "clean")
	require.NoError(t, err)
	require.Contains(t, out, "keep     VM "+clone+": kept for dockhand shell")
	require.Contains(t, builder.left, clone, "plain clean leaves an asked clone")

	_, _, err = cmdtest.Dockhand(t, "archive", "jq-update")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "clean", "jq-update", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "remove   VM "+clone+", kept by check-1 for dockhand shell", "the branch cleanup plan names its asked clone")
	require.Contains(t, builder.left, clone, "the plan does not remove it")
	_, _, err = cmdtest.Dockhand(t, "clean", "jq-update", "--yes")
	require.NoError(t, err)
	require.NotContains(t, builder.left, clone, "cleaning its own branch removes the asked clone")
}

func TestPlainCleanKeepsOnlyUnexpiredInCapFailureClones(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	for _, args := range [][]string{{"check"}, {"check", "--fresh"}} {
		_, _, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err)
	}
	require.Len(t, builder.left, 2)
	expired, current := builder.left[0], builder.left[1]
	builder.keep(expired, time.Now().Add(-8*24*time.Hour), true)
	builder.keep(current, time.Now().Add(-2*24*time.Hour), true)

	out, _, err := cmdtest.Dockhand(t, "clean", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "remove   VM "+expired)
	require.Contains(t, out, "keep     VM "+current+": kept for dockhand shell until")
	require.Contains(t, builder.left, expired)
	require.Contains(t, builder.left, current)

	_, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.NotContains(t, builder.left, expired)
	require.Contains(t, builder.left, current)
}

func TestCleanBranchSelectorRequiresKept(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "clean", "-b", "jq-update")
	require.ErrorContains(t, err, "clean -b jq-update requires --kept; use clean jq-update to clean a branch")
}

func TestCleanKeptAllowsOneBranchSelector(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	out, _, err := cmdtest.Dockhand(t, "clean", "--kept", "-b", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
}

func TestKeepPlanAndCheckAnnounceAskedCloneReplacement(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	builder := &leavingBuilder{}
	cmdtest.Stands(t).Leftovers = builder
	_, _, err := cmdtest.Dockhand(t, "check", "--keep")
	require.NoError(t, err)
	clone := builder.left[0]
	builder.keep(clone, time.Now(), false)

	out, _, err := cmdtest.Dockhand(t, "check", "--keep", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `check-1's kept clone of .+ is removed: this check keeps its own\.`, out)
	require.Contains(t, builder.left, clone, "--plan never removes the earlier clone")

	out, _, err = cmdtest.Dockhand(t, "check", "--keep")
	require.NoError(t, err)
	require.Regexp(t, `check-1's kept clone of .+ is removed: this check keeps its own\.`, out)
	require.NotContains(t, builder.left, clone, "the queued check replaces the earlier clone")
}
