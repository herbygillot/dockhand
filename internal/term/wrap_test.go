package term

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Prose wraps at the width under a hanging indent; a table's line, a word
// with nowhere to break, and a line not yet ended are written as they are.
func TestWrapped(t *testing.T) {
	require.Equal(t, "Next: one two\n  three", Wrapped("Next: one two three", 13))
	require.Equal(t, "  ! held: one\n    two three", Wrapped("  ! held: one two three", 14))
	require.Equal(t, "  jq  1.7.1  1.8.1  update", Wrapped("  jq  1.7.1  1.8.1  update", 10), "a table's")
	require.Equal(t, "see\n  "+strings.Repeat("x", 20), Wrapped("see "+strings.Repeat("x", 20), 10), "a long word overruns")
	require.Equal(t, "short", Wrapped("short", 0))

	var out bytes.Buffer
	w := &wrapWriter{w: &out, width: 10}
	_, _ = w.Write([]byte("? answer one two three "))
	_, _ = w.Write([]byte("now\nwrap these words here\n"))
	require.Equal(t, "? answer one two three now\nwrap these\n  words\n  here\n", out.String(), "a prompt, then lines")
}

// Unwrapped turns the wrap off for a log's or a document's output.
func TestUnwrapped(t *testing.T) {
	var out bytes.Buffer
	streams := Begin(Streams{Out: &out}.WithDisplay(Display{Columns: 10}), nil)
	streams.Unwrapped()
	_, _ = streams.Out.Write([]byte("a log line longer than ten\n"))
	require.Equal(t, "a log line longer than ten\n", out.String())
}
