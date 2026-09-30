package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A port that may have a newer release is named once for what was set
// aside, and counted after, not named every morning: dolt's misspelled
// old tag leaves it uncertain every day it's otherwise current. One whose
// set-aside tags change is named again.
func TestAnUncertainPortIsSaidOnceForWhatWasSetAside(t *testing.T) {
	first := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.15"}}}
	require.Equal(t, "serve: 1 port of yours may have newer releases, for your look: dolt (dockhand outdated dolt says why)", uncertainWords(OutdatedLook{}, first))
	require.Equal(t, "serve: 1 port of yours may still have newer releases, as before (dockhand status lists them)", uncertainWords(first, first))
	next := OutdatedLook{Uncertain: []string{"dolt", "yq"}, SetAside: map[string][]string{"dolt": {"v040.15"}, "yq": {"v5.0"}}}
	require.Equal(t, "serve: 1 port of yours may have newer releases, for your look: yq (dockhand outdated yq says why); 1 more as before", uncertainWords(first, next))
	moved := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.16"}}}
	require.Contains(t, uncertainWords(first, moved), "for your look: dolt")
	require.Empty(t, uncertainWords(first, OutdatedLook{}))
}
