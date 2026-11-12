package checks

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
	c := &Service{Capacities: map[string]int{"tart": config.TartCapacity, "command": 1}}
	holder := func(number int) slotHolder { return slotHolder{run: model.Run{Number: number}} }
	require.Equal(t, []string{"Tart takes as many checks as the Mac runs VMs, 2, and check-1 and check-2 are using them"},
		c.slotWords(Full{"tart": {holder(1), holder(2)}}))
	require.Equal(t, []string{"providers.command.capacity is 1, and check-3 is using it"},
		c.slotWords(Full{"command": {holder(3)}}))
	require.Equal(t, 2, c.Capacity("tart"))
	// GitHub's says what it guards (Codex's review of #197, finding 4).
	c.Capacities["github"] = 20
	require.Equal(t, []string{"providers.github.capacity, which guards the hourly GitHub API budget, is 20, and check-5 is using it"},
		c.slotWords(Full{"github": {holder(5)}}))
}

// A check held for a provider's slots says what it waits behind, in its
// journal, which the person following it reads, wherever it waits: a
// local prefix as local says it, where serve's queue said nothing (rc1's
// full stage, G14); any other provider with what fills it.
func TestServeSaysWhatAHeldCheckWaitsBehind(t *testing.T) {
	t.Parallel()
	c := service(t)
	waiting := c.queued(t, "jq-wait", model.Environment{Provider: "local", Prefix: "/opt/local"})
	// Another check in the same prefix, which serve runs.
	holding := model.Run{ID: "run_holding", Number: 786, Plan: waiting.Plan}
	s := c.session(t)
	said := func(run model.Run) string {
		t.Helper()
		var message string
		require.NoError(t, c.Store.View(t.Context(), c.Repository, func(r store.Reader) error {
			event, found, err := r.LatestRunEvent(run.ID, model.EventWaiting)
			if found {
				message = event.Message
			}
			return err
		}))
		return message
	}
	require.NoError(t, c.SayHeld(t.Context(), s, waiting, Full{"local": {{run: holding}}}))
	require.Equal(t, waiting.Name()+" waiting for /opt/local, held by check-786", said(waiting))

	// Another branch's check, on command, fills its capacity.
	other := c.queued(t, "jq-other", model.Environment{Provider: "command"})
	c.Capacities = map[string]int{"command": 1}
	require.NoError(t, c.SayHeld(t.Context(), s, waiting, Full{"command": {{run: other}}}))
	require.Equal(t, waiting.Name()+" waits for a slot on command: providers.command.capacity is 1, and "+other.Name()+" is using it", said(waiting))
}
