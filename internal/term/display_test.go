package term

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// On a terminal the marks are coloured, ✗ red, ! yellow where it's a
// mark, ✓ green, and nothing else; a rune a write splits is coloured
// whole; Plain turns it off (the person's "Marks only", 2026-10-22).
func TestTheMarksAreColouredAndNothingElse(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	marks := &markWriter{w: &out}
	_, err := marks.Write([]byte("  ✗ gdal  failed!\n  ! hyperfine held: a new major version\njq  ✓  !  ✓\nwhat? yes! no\n"))
	require.NoError(t, err)
	require.Equal(t, "  "+red+"✗"+reset+" gdal  failed!\n  "+yellow+"!"+reset+" hyperfine held: a new major version\njq  "+green+"✓"+reset+"  "+yellow+"!"+reset+"  "+green+"✓"+reset+"\nwhat? yes! no\n", out.String())

	out.Reset()
	split := []byte("✓ done\n")
	_, err = marks.Write(split[:2])
	require.NoError(t, err)
	n, err := marks.Write(split[2:])
	require.NoError(t, err)
	require.Equal(t, len(split)-2, n)
	require.Equal(t, green+"✓"+reset+" done\n", out.String())

	out.Reset()
	streams := Streams{Out: &switchWriter{w: marks}}.Plain()
	_, err = streams.Out.Write([]byte("✗ plain\n"))
	require.NoError(t, err)
	require.Equal(t, "✗ plain\n", out.String())
}

// Off a terminal prose wraps at COLUMNS, else at 100, with a hanging
// indent, and nothing is cut.
func TestProseWrapsAtTheWidthWithAHangingIndent(t *testing.T) {
	t.Parallel()
	streams := Streams{Out: &bytes.Buffer{}}
	require.Equal(t, 100, streams.Width())
	narrow := streams.WithDisplay(Display{Columns: 30})
	require.Equal(t, 30, narrow.Width())
	text := strings.Repeat("word ", 10)
	require.Equal(t, "  ! jq  word word word word\n        word word word word\n        word word", narrow.Hanging("  ! jq  ", text))
}
