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
