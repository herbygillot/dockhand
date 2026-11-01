package screen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Each sequence a terminal sends is its key; one that names no key here,
// and a control character other than Enter's and Ctrl-C's, is dropped.
func TestTheDecoderReadsEachKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
		want  []Key
	}{
		{"up", "\x1b[A", []Key{{Code: Up}}},
		{"down", "\x1b[B", []Key{{Code: Down}}},
		{"application up", "\x1bOA", []Key{{Code: Up}}},
		{"application down", "\x1bOB", []Key{{Code: Down}}},
		{"page up", "\x1b[5~", []Key{{Code: PageUp}}},
		{"page down", "\x1b[6~", []Key{{Code: PageDown}}},
		{"home", "\x1b[H", []Key{{Code: Home}}},
		{"home, VT220", "\x1b[1~", []Key{{Code: Home}}},
		{"home, application", "\x1bOH", []Key{{Code: Home}}},
		{"end", "\x1b[F", []Key{{Code: End}}},
		{"end, VT220", "\x1b[4~", []Key{{Code: End}}},
		{"enter, raw", "\r", []Key{{Code: Enter}}},
		{"enter, a pipe's", "\n", []Key{{Code: Enter}}},
		{"ctrl-c", "\x03", []Key{{Code: Interrupt}}},
		{"runes", "lé", []Key{{Code: Rune, Rune: 'l'}, {Code: Rune, Rune: 'é'}}},
		{"right arrow, unknown", "\x1b[C", nil},
		{"a function key, unknown", "\x1b[15~", nil},
		{"a modified arrow, unknown", "\x1b[1;5A", nil},
		{"another control character", "\x01\x7f", nil},
		{"unknown between keys", "q\x1b[Cq", []Key{{Code: Rune, Rune: 'q'}, {Code: Rune, Rune: 'q'}}},
		{"escape before another escape", "\x1b\x1b[A", []Key{{Code: Escape}, {Code: Up}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var d decoder
			require.Equal(t, tc.want, append(d.feed([]byte(tc.input)), d.flush()...))
		})
	}
}

// A sequence one read splits is held for the next, as a rune split is;
// an ESC nothing follows is the Escape key once the input is quiet.
func TestTheDecoderHoldsASplitSequence(t *testing.T) {
	t.Parallel()
	var d decoder
	require.Empty(t, d.feed([]byte("\x1b")))
	require.Empty(t, d.feed([]byte("[")))
	require.Equal(t, []Key{{Code: PageDown}}, d.feed([]byte("6~")))
	require.Empty(t, d.feed([]byte{0xc3}))
	require.Equal(t, []Key{{Code: Rune, Rune: 'é'}}, d.feed([]byte{0xa9}))

	require.Empty(t, d.feed([]byte("\x1b")))
	require.Equal(t, []Key{{Code: Escape}}, d.flush())
	require.Empty(t, d.feed([]byte("\x1b[5")))
	require.Empty(t, d.flush(), "a sequence begun and never ended is dropped")
}
