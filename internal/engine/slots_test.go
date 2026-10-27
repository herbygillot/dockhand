package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/model"
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
