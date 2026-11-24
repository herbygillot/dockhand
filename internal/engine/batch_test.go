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

// controlledProvider holds each check in the provider until its test releases
// it, and keeps the most it built at once.
type controlledProvider struct {
	mu       sync.Mutex
	entered  chan model.RunID
	release  map[model.RunID]chan struct{}
	building int
	most     int
}

func (p *controlledProvider) Name() string { return "command" }

func (p *controlledProvider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	p.mu.Lock()
	p.building++
	p.most = max(p.most, p.building)
	release := p.release[job.Run.ID]
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.building--
		p.mu.Unlock()
	}()
	p.entered <- job.Run.ID
	select {
	case <-release:
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

func (p *controlledProvider) mostBuilding() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.most
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
	provider := &controlledProvider{entered: make(chan model.RunID, 3), release: map[model.RunID]chan struct{}{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var ids []model.RunID
	for _, name := range []string{"jq-a", "jq-b", "jq-c"} {
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: update.Branch, Mode: CaptureWorking})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		run, err := e.Enqueue(t.Context(), update.Branch, plan, model.OriginPerson)
		require.NoError(t, err)
		provider.release[run.ID] = make(chan struct{})
		ids = append(ids, run.ID)
	}

	e.Capacity = map[string]int{"command": 3}
	type result struct {
		run model.Run
		err error
	}
	ended := make(chan result, len(ids))
	go e.SeeAllThrough(t.Context(), ids, nil, func(run model.Run, err error) {
		ended <- result{run: run, err: err}
	})
	for range ids {
		<-provider.entered
	}
	// All three are inside the provider together. Release them in the order
	// they should end, waiting for each completion before releasing the next.
	for _, id := range []model.RunID{ids[2], ids[1], ids[0]} {
		close(provider.release[id])
		result := <-ended
		require.NoError(t, result.err)
		require.Equal(t, model.RunPassed, result.run.State, result.run.Detail)
		require.Equal(t, id, result.run.ID)
	}
}

// The batch keeps to a provider's capacity, which a check waiting for its
// slot can't see of its siblings still queued.
func TestABatchKeepsToAProvidersCapacity(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	provider := &controlledProvider{entered: make(chan model.RunID, 3), release: map[model.RunID]chan struct{}{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var ids []model.RunID
	for _, name := range []string{"jq-a", "jq-b", "jq-c"} {
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: update.Branch, Mode: CaptureWorking})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		run, err := e.Enqueue(t.Context(), update.Branch, plan, model.OriginPerson)
		require.NoError(t, err)
		provider.release[run.ID] = make(chan struct{})
		ids = append(ids, run.ID)
	}
	e.Capacity = map[string]int{"command": 2}
	// Said first, which run together and how many at once (G13).
	var runs []model.Run
	for _, id := range ids {
		run, err := e.Run(t.Context(), id)
		require.NoError(t, err)
		runs = append(runs, run)
	}
	require.Regexp(t, `^check-\d+, check-\d+, and check-\d+ are checked as a batch, 2 at once on command$`, e.TogetherWords(t.Context(), runs))
	require.Empty(t, e.TogetherWords(t.Context(), runs[:1]), "one check is no batch")
	completed := make(chan error, len(ids))
	waits := make(chan string, len(ids))
	// The one that waits for a sibling's slot says so, where a batch said
	// nothing of a wait (rc1's full stage, G13).
	waited := func(run model.Run, message string) {
		waits <- message
	}
	go e.SeeAllThrough(t.Context(), ids, waited, func(run model.Run, err error) {
		completed <- err
	})
	first := <-provider.entered
	second := <-provider.entered
	waitMessage := <-waits
	// Keep the first two inside the provider until the third reports its wait.
	for _, id := range []model.RunID{first, second} {
		close(provider.release[id])
	}
	third := <-provider.entered
	close(provider.release[third])
	for range ids {
		require.NoError(t, <-completed)
	}
	require.LessOrEqual(t, provider.mostBuilding(), 2, "never more than its capacity at once")
	require.Empty(t, waits, "only the third check waits for a slot")
	require.Regexp(t, `^check-\d+ waits for a slot on command: providers\.command\.capacity is 2, and check-\d+ \(a foreground dockhand, pid \d+, [^)]+\) and check-\d+ \(a foreground dockhand, pid \d+, [^)]+\) are using it$`, waitMessage)
}

// One capacity holds across drivers: a batch's check waits while another
// process, serve or a foreground check, holds the provider's one slot,
// says what holds it, and takes the slot once it's let go, where a batch
// counted only its own checks (Codex's review of 275125ff, finding 4).
func TestABatchCountsAnotherProcesssChecks(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	provider := &controlledProvider{entered: make(chan model.RunID, 1), release: map[model.RunID]chan struct{}{}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var ids []model.RunID
	for _, name := range []string{"jq-a", "jq-b"} {
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: update.Branch, Mode: CaptureWorking})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		run, err := e.Enqueue(t.Context(), update.Branch, plan, model.OriginPerson)
		require.NoError(t, err)
		provider.release[run.ID] = make(chan struct{})
		ids = append(ids, run.ID)
	}
	e.Capacity = map[string]int{"command": 1}
	// Another process takes the first check's slot, as serve does.
	other := session(t, e)
	held, err := e.Run(t.Context(), ids[0])
	require.NoError(t, err)
	lease, full, err := e.checks().Reserve(t.Context(), other, held, []string{"command"})
	require.NoError(t, err)
	require.Empty(t, full)

	waits := make(chan string, 4)
	done := make(chan model.Run, 1)
	go e.SeeAllThrough(t.Context(), ids[1:], func(_ model.Run, message string) { waits <- message }, func(run model.Run, err error) {
		require.NoError(t, err)
		done <- run
	})
	select {
	case message := <-waits:
		require.Contains(t, message, "waits for a slot on command: providers.command.capacity is 1, and "+held.Name())
	case run := <-done:
		t.Fatalf("%s ran while another process held the provider's one slot", run.Name())
	case <-time.After(30 * time.Second):
		t.Fatal("the batch's check never said what it waits for")
	}
	// Admission records a run's start as it takes its slot: the held
	// check's when the other process took it, and none for the waiting
	// one yet.
	taken, err := e.Run(t.Context(), ids[0])
	require.NoError(t, err)
	require.NotNil(t, taken.StartedAt, "its slot taken")
	waiting, err := e.Run(t.Context(), ids[1])
	require.NoError(t, err)
	require.Nil(t, waiting.StartedAt, "still waiting for its slot")
	require.NoError(t, other.Release(t.Context(), lease))
	<-provider.entered
	close(provider.release[ids[1]])
	select {
	case run := <-done:
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		require.NotNil(t, run.StartedAt, "started once it took the slot let go")
	case <-time.After(30 * time.Second):
		t.Fatal("the batch's check never took the slot let go")
	}
}
