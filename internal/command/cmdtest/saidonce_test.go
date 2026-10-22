package cmdtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/term"
)

// SaidOnce counts a name once a line, outside tables and paths, and the
// Next: lines.
func TestSaidOnce(t *testing.T) {
	require.Empty(t, SaidOnce("jq-update · checking commit 1a2b3c4\n  jq  ✓\nPassed.\n"))
	require.Empty(t, SaidOnce("check-1 failed\nLogs:\n  jq on command, attempt 1: ~/.dockhand/logs/check-1/command-1/command.log.gz\n"), "a path")
	require.Empty(t, SaidOnce("Replaced check-1, canceled\n  RUN      STATE\n  check-1  canceled\n"), "a table")
	require.Equal(t, []string{"1a2b3c4 named 2 times"}, SaidOnce("checking commit 1a2b3c4\nPassed for commit 1a2b3c4.\n"))
	require.Equal(t, []string{"2 Next: lines"}, SaidOnce("Next: dockhand tidy\nNext: dockhand check\n"))
	words := strings.Repeat("word ", 21)
	require.Len(t, Overwidth(words+"\n"+strings.Repeat("y", 100)), 1, "prose the writer would wrap")
	require.Empty(t, Overwidth(strings.Repeat("x", 101)), "nowhere to break")
	require.Empty(t, Overwidth("  "+strings.Repeat("cell  ", 20)), "a table's")
}

// -v, --json, a preview, and help are read in full.
func TestSaidOnceChecks(t *testing.T) {
	for _, args := range [][]string{{"ship", "-v"}, {"ship", "-vv"}, {"status", "--json"}, {"submit", "--plan"}, {"serve"}, {"review", "1", "--markdown"}} {
		require.False(t, saidOnceChecked(args, testStreams()), args)
	}
	require.True(t, saidOnceChecked([]string{"ship", "jq", "--yes"}, testStreams()))
}

func testStreams() term.Streams { return term.Streams{In: strings.NewReader("")} }
