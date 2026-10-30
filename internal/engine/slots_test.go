package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Tart takes as many checks as the Mac runs VMs, no setting of its own: a
// third check waits on the two using them, naming each, where a command's
// one check fills its capacity (the person's "Let macOS be the limit",
// 2026-10-26).
func TestTartsSlotsAreTheMacsVMs(t *testing.T) {
	t.Parallel()
	e := &Engine{Capacity: map[string]int{"tart": config.TartCapacity, "command": 1}}
	holder := func(number int) slotHolder { return slotHolder{run: model.Run{Number: number}} }
	require.Equal(t, []string{"Tart takes as many checks as the Mac runs VMs, 2, and check-1 and check-2 are using them"},
		slotWords(map[string][]slotHolder{"tart": {holder(1), holder(2)}}, e))
	require.Equal(t, []string{"providers.command.capacity is 1, and check-3 is using it"},
		slotWords(map[string][]slotHolder{"command": {holder(3)}}, e))
	require.Equal(t, 2, e.capacity("tart"))
}

// A check serve's queue holds for a provider its own checks fill says what
// it waits behind, once, in its journal, which the person following it
// reads: a local prefix as local says it, where nothing was said (rc1's
// full stage, G14); any other provider as awaitSlot says it.
func TestServeSaysWhatAHeldCheckWaitsBehind(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := servePrepared(t, e)
	queue := func(environment model.Environment) model.Run {
		t.Helper()
		proposal, err := e.ProposeCheck(t.Context(), CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureHead}, Where: Where{Environments: []model.Environment{{Provider: "command"}}}})
		require.NoError(t, err)
		proposal.Plan.Environments = []model.Environment{environment}
		for i := range proposal.Plan.Builds {
			proposal.Plan.Builds[i].Environment = environment
		}
		queued, err := e.Enqueue(t.Context(), branch, proposal.Plan, model.OriginPerson)
		require.NoError(t, err)
		return queued
	}
	waiting := queue(model.Environment{Provider: "local", Prefix: "/opt/local"})
	// Another check in the same prefix, which serve runs.
	holding := model.Run{ID: "run_holding", Number: 786, Plan: waiting.Plan}
	s := session(t, e)
	said := func(run model.Run) string {
		t.Helper()
		var message string
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			event, found, err := r.LatestRunEvent(run.ID, model.EventWaiting)
			if found {
				message = event.Message
			}
			return err
		}))
		return message
	}
	require.NoError(t, e.sayHeldInServe(t.Context(), s, waiting, []string{"local"}, map[model.RunID]model.Run{holding.ID: holding}))
	require.Equal(t, waiting.Name()+" waiting for /opt/local, held by check-786", said(waiting))

	// The check serve prepared, on command, fills its capacity.
	var prepared model.Run
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunPassed}})
		if len(runs) > 0 {
			prepared = runs[0]
		}
		return err
	}))
	e.Capacity = map[string]int{"command": 1}
	require.NoError(t, e.sayHeldInServe(t.Context(), s, waiting, []string{"command"}, map[model.RunID]model.Run{prepared.ID: prepared}))
	require.Equal(t, waiting.Name()+" waits for a slot on command: providers.command.capacity is 1, and "+prepared.Name()+" is using it", said(waiting))
}
