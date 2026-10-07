package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A port that may have a newer release is named once for what was set
// aside, and counted after, not named every morning: dolt's misspelled
// old tag leaves it uncertain every day it's otherwise current. One whose
// set-aside tags change is named again.
func TestAnUncertainPortIsSaidOnceForWhatWasSetAside(t *testing.T) {
	t.Parallel()
	first := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.15"}}}
	require.Equal(t, "serve: 1 port of yours may have newer releases, for your look: dolt (dockhand outdated dolt says why)", uncertainWords(OutdatedLook{}, first))
	require.Equal(t, "serve: 1 port of yours may still have newer releases, as before (dockhand status lists them)", uncertainWords(first, first))
	next := OutdatedLook{Uncertain: []string{"dolt", "yq"}, SetAside: map[string][]string{"dolt": {"v040.15"}, "yq": {"v5.0"}}}
	require.Equal(t, "serve: 1 port of yours may have newer releases, for your look: yq (dockhand outdated yq says why); 1 more as before", uncertainWords(first, next))
	moved := OutdatedLook{Uncertain: []string{"dolt"}, SetAside: map[string][]string{"dolt": {"v040.16"}}}
	require.Contains(t, uncertainWords(first, moved), "for your look: dolt")
	require.Empty(t, uncertainWords(first, OutdatedLook{}))
}

// A file serve couldn't write is said once a day, each file for itself,
// where it was dropped, and status read an older state without a word
// (the architecture review's L3e).
func TestServeSaysAFileItCouldntWriteOnceADay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	var said []string
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	s := newServer(e, ServeOptions{Say: func(line string) { said = append(said, line) }, Now: func() time.Time { return now }})
	full := errors.New("no space left on device")
	s.written("serving.json", nil)
	s.written("serving.json", full)
	s.written("serving.json", full)
	s.written("outdated.json", full)
	require.Equal(t, []string{
		"serve: couldn't write " + e.serveFile("serving.json") + ", so status may read an older state: no space left on device",
		"serve: couldn't write " + e.serveFile("outdated.json") + ", so status may read an older state: no space left on device",
	}, said)
	now = now.AddDate(0, 0, 1)
	s.written("serving.json", full)
	require.Len(t, said, 3, "said again the next day")
}
