package command

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// leavingBuilder passes every target, names its environment on the
// execution, and leaves the environment behind, as a process that died
// would. Beside them it lists one no check of this checkout made.
type leavingBuilder struct {
	mu   sync.Mutex
	left []string
}

func (p *leavingBuilder) Name() string { return "command" }

func (p *leavingBuilder) Execute(_ context.Context, job engine.Job, build engine.Build) error {
	ref := "vm-" + string(job.Execution.ID)
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

func (p *leavingBuilder) Leftovers(context.Context) ([]engine.Leftover, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var found []engine.Leftover
	for _, ref := range append(slices.Clone(p.left), "vm-elsewhere") {
		found = append(found, engine.Leftover{Ref: ref, What: "VM " + ref})
	}
	return found, nil
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
	checkedBranch(t)
	builder := &leavingBuilder{}
	testLeftovers = builder
	t.Cleanup(func() { testLeftovers = nil })
	_, _, err := dockhand(t, "check")
	require.NoError(t, err)
	require.Len(t, builder.left, 1)
	vm := builder.left[0]

	out, _, err := dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Left by checks\n"+
		"  remove   VM "+vm+", left by check-1\n"+
		"  keep     VM vm-elsewhere: no check of this checkout made it\n"+
		"Nothing was removed; --yes removes these.\n", out)

	result, err := jsonOf(t, "clean")
	require.NoError(t, err)
	require.Equal(t, vm, dig(t, result.Result, "leftovers", 0, "ref"))
	require.Equal(t, "check-1", dig(t, result.Result, "leftovers", 0, "check"))
	require.Equal(t, false, dig(t, result.Result, "leftovers", 0, "removed"))

	out, _, err = dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "\nLeft by checks\n  removed  VM "+vm+", left by check-1\n  keep     VM vm-elsewhere: no check of this checkout made it\n")
	require.Empty(t, builder.left)

	out, _, err = dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Left by checks\n  keep     VM vm-elsewhere: no check of this checkout made it\nNothing to remove.\n", out)
}
