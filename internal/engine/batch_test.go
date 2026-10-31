package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// pacedProvider builds each check's targets after its run's pause, and
// keeps the most it built at once.
type pacedProvider struct {
	mu       sync.Mutex
	pause    map[model.RunID]time.Duration
	building int
	most     int
}

func (p *pacedProvider) Name() string { return "command" }

func (p *pacedProvider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	p.mu.Lock()
	p.building++
	p.most = max(p.most, p.building)
	pause := p.pause[job.Run.ID]
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.building--
		p.mu.Unlock()
	}()
	select {
	case <-time.After(pause):
	case <-ctx.Done():
		return ctx.Err()
	}
	for _, target := range job.Targets {
		if err := build.Record(buildenv.Reported(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone})); err != nil {
			return err
		}
	}
	return nil
}

// A batch's checks run together, at most as many on a provider as its
// capacity, and each is said as it ends, in the order they end, so a
// batch takes about its longest check where it took the sum (Codex's
// workflow review of 2ad896d2; the person's "every ask I add to Dockhand
// can increase the amount of time it takes to go through the bump
// process", 2026-10-29).
func TestABatchsChecksRunTogether(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	provider := &pacedProvider{pause: map[model.RunID]time.Duration{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var ids []model.RunID
	for i, pause := range []time.Duration{900 * time.Millisecond, 450 * time.Millisecond, 50 * time.Millisecond} {
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: []string{"jq-a", "jq-b", "jq-c"}[i]}, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: update.Branch})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		run, err := e.Enqueue(t.Context(), update.Branch, plan, model.OriginPerson)
		require.NoError(t, err)
		provider.pause[run.ID] = pause
		ids = append(ids, run.ID)
	}

	e.Capacity = map[string]int{"command": 3}
	var ended []model.RunID
	e.SeeAllThrough(t.Context(), ids, nil, func(run model.Run, err error) {
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		ended = append(ended, run.ID)
	})
	// One at a time, they'd end in the order given.
	require.Equal(t, []model.RunID{ids[2], ids[1], ids[0]}, ended, "together, in the order they end")
}

// The batch keeps to a provider's capacity, which a check waiting for its
// slot can't see of its siblings still queued.
func TestABatchKeepsToAProvidersCapacity(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	provider := &pacedProvider{pause: map[model.RunID]time.Duration{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var ids []model.RunID
	for _, name := range []string{"jq-a", "jq-b", "jq-c"} {
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: update.Branch})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		run, err := e.Enqueue(t.Context(), update.Branch, plan, model.OriginPerson)
		require.NoError(t, err)
		provider.pause[run.ID] = 100 * time.Millisecond
		ids = append(ids, run.ID)
	}
	e.Capacity = map[string]int{"command": 2}
	n := 0
	var mu sync.Mutex
	var waits []string
	// The one that waits for a sibling's slot says so, where a batch said
	// nothing of a wait (rc1's full stage, G13).
	waited := func(run model.Run, message string) {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, message)
	}
	e.SeeAllThrough(t.Context(), ids, waited, func(run model.Run, err error) {
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		n++
	})
	require.Equal(t, 3, n)
	require.LessOrEqual(t, provider.most, 2, "never more than its capacity at once")
	require.Len(t, waits, 1)
	require.Regexp(t, `^check-\d+ waits for a slot on command, which the batch's other checks are using \(capacity 2\)$`, waits[0])
}
