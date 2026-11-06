package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
)

// A shell's completion script is written as it is, on a terminal too:
// wrapped at its width, fish's and bash's broke where a line split, fish
// reading "completions are run" as a command (#88).
func TestCompletionScriptsAreNotWrapped(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	for _, shell := range []string{"fish", "bash", "zsh"} {
		var out, errs bytes.Buffer
		require.NoError(t, cmdtest.RunFor(t, t.Context(), []string{"completion", shell}, term.Streams{Out: &out, Err: &errs}.AsTerminal()))
		longest := 0
		for _, line := range strings.Split(out.String(), "\n") {
			longest = max(longest, len(line))
		}
		require.Greater(t, longest, 100, "%s: a line longer than the terminal's width stays whole", shell)
		require.NotContains(t, out.String(), "\ncompletions are run", shell)
	}
}
