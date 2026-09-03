package sweep

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// fakePool stands in for the evaluator pool. Its evaluators are nil
// pointers on purpose: the loop only carries them from the pool to the
// verb's eval func, and a test whose eval func ignores the handle
// proves the loop without a MacPorts installation anywhere near it.
type fakePool struct {
	mu       sync.Mutex
	evs      []*eval.Evaluator
	replaces int
	replErr  error
}

func newFakePool(n int) *fakePool {
	p := &fakePool{}
	for i := 0; i < n; i++ {
		p.evs = append(p.evs, nil)
	}
	return p
}

func (p *fakePool) Evaluators() []*eval.Evaluator {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*eval.Evaluator, len(p.evs))
	copy(out, p.evs)
	return out
}

func (p *fakePool) Replace(*eval.Evaluator) (*eval.Evaluator, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replaces++
	if p.replErr != nil {
		return nil, p.replErr
	}
	return nil, nil
}

func (p *fakePool) replaceCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.replaces
}

// row is a verb's row: this test's stand-in for classify.Result and the
// write verbs' NDJSON line.
type row struct {
	portdir string
	broken  bool
	lost    bool
}

func targets(n int) []tree.Target {
	out := make([]tree.Target, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, tree.Target{Portdir: fmt.Sprintf("/ports/cat/p%03d", i)})
	}
	return out
}

func config() Config[row] {
	return Config[row]{
		PerTarget: time.Minute,
		Broken:    func(r row) bool { return r.broken },
		Abandon: func(t tree.Target, _ error) row {
			return row{portdir: t.Portdir, lost: true}
		},
	}
}

// The dispatch loop's central promise: every target in, exactly one row
// out, and the rows drained on the caller's own goroutine so nothing
// here needs a lock.
func TestRunGivesOneRowPerTarget(t *testing.T) {
	p := newFakePool(4)
	ts := targets(50)

	seen := map[string]int{}
	err := Run(context.Background(), config(), p, ts,
		func(_ context.Context, h port.Handle) row { return row{portdir: h.Target.Portdir} },
		func(r row) { seen[r.portdir]++ })

	require.NoError(t, err)
	require.Len(t, seen, len(ts))
	for _, tgt := range ts {
		assert.Equal(t, 1, seen[tgt.Portdir], tgt.Portdir)
	}
}

// Three consecutive broken rows condemn an evaluator; anything else
// resets the count. A pool that churned on ordinary declines would
// restart an interpreter every third port of a tree full of them.
func TestRunReplacesAfterThreeStrikes(t *testing.T) {
	p := newFakePool(1)
	err := Run(context.Background(), config(), p, targets(9),
		func(_ context.Context, h port.Handle) row {
			return row{portdir: h.Target.Portdir, broken: true}
		},
		func(row) {})
	require.NoError(t, err)
	assert.Equal(t, 3, p.replaceCount(), "nine broken rows are three strikes three times over")

	p = newFakePool(1)
	alternate := 0
	err = Run(context.Background(), config(), p, targets(9),
		func(_ context.Context, h port.Handle) row {
			alternate++
			return row{portdir: h.Target.Portdir, broken: alternate%3 != 0}
		},
		func(row) {})
	require.NoError(t, err)
	assert.Zero(t, p.replaceCount(), "a good row between the bad ones resets the count")
}

// The defect this loop was promoted to fix. A worker whose replacement
// fails used to return with its evaluator already dropped from the
// pool; when every worker took that exit the queue was simply never
// read, and the caller saw a census that was short with nothing said.
func TestRunReportsWhatItAbandoned(t *testing.T) {
	boom := errors.New("no evaluator could be started")
	p := newFakePool(1)
	p.replErr = boom
	ts := targets(20)

	var rows []row
	err := Run(context.Background(), config(), p, ts,
		func(_ context.Context, h port.Handle) row {
			return row{portdir: h.Target.Portdir, broken: true}
		},
		func(r row) { rows = append(rows, r) })

	require.Error(t, err)
	require.ErrorIs(t, err, ErrAbandoned)
	require.ErrorIs(t, err, boom, "the pool's own failure survives")

	var ae *AbandonedError
	require.ErrorAs(t, err, &ae)
	assert.Len(t, ae.Targets, len(ts)-3, "three were judged before the evaluator died")
	assert.Equal(t, exitcode.SweepHardErrors, ae.DockhandExit())

	// The arithmetic still adds up: every target got exactly one row,
	// and the ones nobody reached say so.
	require.Len(t, rows, len(ts))
	lost := 0
	for _, r := range rows {
		if r.lost {
			lost++
		}
	}
	assert.Equal(t, len(ts)-3, lost)
}

// A pool with no evaluators abandons everything rather than deadlocking
// or reporting an empty sweep as a complete one. Nothing was judged, so
// every row says so.
func TestRunWithNoEvaluators(t *testing.T) {
	var rows []row
	err := Run(context.Background(), config(), newFakePool(0), targets(3),
		func(context.Context, port.Handle) row {
			t.Error("there is no evaluator to judge with")
			return row{}
		},
		func(r row) { rows = append(rows, r) })
	require.ErrorIs(t, err, ErrAbandoned)
	require.Len(t, rows, 3)
	for _, r := range rows {
		assert.True(t, r.lost)
	}
}

// The resume-by-rerun gate. An interrupt must leave the ports it never
// reached unrecorded: rerunning the command is the resume, so a kill
// that wrote a failure row for every remaining target would make the
// rerun skip ports it never touched.
func TestRunInterruptRecordsOnlyWhatItReached(t *testing.T) {
	const stopAfter = 7
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := newFakePool(1)
	ts := targets(40)
	done := 0
	var rows []row
	err := Run(ctx, config(), p, ts,
		func(_ context.Context, h port.Handle) row {
			if done++; done == stopAfter {
				cancel()
			}
			return row{portdir: h.Target.Portdir}
		},
		func(r row) { rows = append(rows, r) })

	require.ErrorIs(t, err, context.Canceled)
	// The row for the target in flight when the cancellation landed is
	// dropped too: a result computed under a dead context describes the
	// interruption, not the port.
	require.Len(t, rows, stopAfter-1)
	for _, r := range rows {
		assert.False(t, r.lost, "an interrupt fabricates nothing")
	}

	// The rerun: the same command over what the first run did not
	// record covers exactly the rest, once each.
	recorded := map[string]bool{}
	for _, r := range rows {
		recorded[r.portdir] = true
	}
	var rest []tree.Target
	for _, tgt := range ts {
		if !recorded[tgt.Portdir] {
			rest = append(rest, tgt)
		}
	}
	seen := map[string]int{}
	require.NoError(t, Run(context.Background(), config(), newFakePool(4), rest,
		func(_ context.Context, h port.Handle) row { return row{portdir: h.Target.Portdir} },
		func(r row) { seen[r.portdir]++ }))
	assert.Len(t, seen, len(ts)-len(rows))
	for _, tgt := range ts[:len(rows)] {
		assert.NotContains(t, seen, tgt.Portdir, "the rerun skips what the first run recorded")
	}
}

// A target that hangs its evaluator becomes that verb's own row, not a
// stuck sweep — and the bound is the verb's, because a census's minute
// is the wrong bound for a bump that fetches a distfile.
func TestRunPerTargetTimeout(t *testing.T) {
	cfg := config()
	cfg.PerTarget = 20 * time.Millisecond
	var rows []row
	err := Run(context.Background(), cfg, newFakePool(2), targets(4),
		func(cctx context.Context, h port.Handle) row {
			<-cctx.Done()
			return row{portdir: h.Target.Portdir, broken: true}
		},
		func(r row) { rows = append(rows, r) })
	require.NoError(t, err)
	assert.Len(t, rows, 4)
}

// The temp root is applied by the loop, so no verb has to rebuild the
// handle — and a verb that rebuilt one could hold an evaluator across
// targets, which is what the three-strikes replacement rests on not
// happening.
func TestRunBuildsTheHandleAtDispatch(t *testing.T) {
	var got port.Handle
	require.NoError(t, Run(context.Background(), config(), newFakePool(1),
		[]tree.Target{{Portdir: "/ports/cat/p", Subport: "p-sub"}},
		func(_ context.Context, h port.Handle) row {
			got = h
			return row{portdir: h.Target.Portdir}
		},
		func(row) {}))
	assert.Equal(t, "p-sub", got.Target.Subport)
}

func TestRunWithNoTargets(t *testing.T) {
	require.NoError(t, Run(context.Background(), config(), newFakePool(2), nil,
		func(context.Context, port.Handle) row { t.Fatal("nothing to do"); return row{} },
		func(row) {}))
}
