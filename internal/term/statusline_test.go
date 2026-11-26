package term

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// A line printed while the count shows clears it, and draws it again after.
func TestAReportPrintsAroundTheCount(t *testing.T) {
	t.Parallel()
	var err bytes.Buffer
	line := &StatusLine{w: &err}
	line.Show("Looking up each port's newest release: 1 of 2")
	line.Say("Building the PortIndex")
	line.Clear()
	require.Equal(t, "\r\033[KLooking up each port's newest release: 1 of 2\r\033[KBuilding the PortIndex\nLooking up each port's newest release: 1 of 2\r\033[K", err.String())
}

// The redrawn line is cut short of the terminal's width: one that wrapped
// left its first row behind at each redraw, since the clear reaches only
// the row the cursor is on, and a check's progress read as one long line
// of every step (the #332 try on the M5).
func TestTheRedrawnLineIsCutToTheTerminalsWidth(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	line := &StatusLine{w: &out, columns: func() int { return 20 }}
	line.Show("⋯ check-1: attempt 1 of 3 on local:/opt/macports-test")
	require.Equal(t, "\r\033[K⋯ check-1: attempt…", out.String())
	out.Reset()
	line.Say("a line of its own, which isn't cut")
	require.Equal(t, "\r\033[Ka line of its own, which isn't cut\n⋯ check-1: attempt…", out.String())
	out.Reset()
	short := &StatusLine{w: &out, columns: func() int { return 0 }}
	short.Show("⋯ off a terminal, nothing is cut, however long it is")
	require.Equal(t, "\r\033[K⋯ off a terminal, nothing is cut, however long it is", out.String())
}
