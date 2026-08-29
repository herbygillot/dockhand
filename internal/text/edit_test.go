package text

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func sp(a, b int) Span { return Span{Start: a, End: b} }

func mustApply(t *testing.T, src string, edits []Edit) []byte {
	t.Helper()
	out, err := Apply([]byte(src), edits)
	require.NoError(t, err)
	return out
}

func mustRefuse(t *testing.T, src string, edits []Edit, want EditErrorType) {
	t.Helper()
	_, err := Apply([]byte(src), edits)
	var ee EditError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, want, ee.Type)
}

func TestApplyIdentity(t *testing.T) {
	src := "go.setup foo 1.0 v\n"
	require.Equal(t, src, string(mustApply(t, src, nil)))
}

func TestApplyReplaceInsertDelete(t *testing.T) {
	//         0123456789
	src := "version 1.0\n"
	out := mustApply(t, src, []Edit{
		{sp(8, 11), []byte("2.34")},          // replace
		{sp(12, 12), []byte("revision 0\n")}, // insert at end
		{sp(0, 0), []byte("# managed\n")},    // insert at start
	})
	require.Equal(t, "# managed\nversion 2.34\nrevision 0\n", string(out))

	out = mustApply(t, src, []Edit{{sp(7, 11), nil}}) // delete
	require.Equal(t, "version\n", string(out))
}

func TestApplyUntouchedBytesIdentical(t *testing.T) {
	src := "a \t weird\x00 buffer {with} [stuff] $here\n"
	out := mustApply(t, src, []Edit{{sp(10, 16), []byte("BUFFER")}})
	require.Equal(t, []byte(src)[:10], out[:10])
	require.Equal(t, []byte(src)[16:], out[16:])
}

func TestApplyTemplateShape(t *testing.T) {
	src := "a long portfile body\n"
	out := mustApply(t, src, []Edit{{sp(0, len(src)), []byte("tombstone\n")}})
	require.Equal(t, "tombstone\n", string(out))
}

func TestApplyOrderIndependence(t *testing.T) {
	src := "abcdef"
	a := []Edit{{sp(0, 2), []byte("X")}, {sp(4, 6), []byte("Y")}}
	b := []Edit{{sp(4, 6), []byte("Y")}, {sp(0, 2), []byte("X")}}
	require.Equal(t, string(mustApply(t, src, a)), string(mustApply(t, src, b)))
}

func TestInsertionAtEditBoundary(t *testing.T) {
	// An insertion at a replacement's start lands before the replacement,
	// deterministically, regardless of input order.
	src := "abc"
	want := "X" + "Y" + "bc"
	for _, edits := range [][]Edit{
		{{sp(0, 0), []byte("X")}, {sp(0, 1), []byte("Y")}},
		{{sp(0, 1), []byte("Y")}, {sp(0, 0), []byte("X")}},
	} {
		require.Equal(t, want, string(mustApply(t, src, edits)))
	}
}

func TestRefusals(t *testing.T) {
	src := "abcdef"
	mustRefuse(t, src, []Edit{{sp(0, 3), []byte("x")}, {sp(2, 4), []byte("y")}}, Overlap)
	mustRefuse(t, src, []Edit{{sp(2, 2), []byte("x")}, {sp(2, 2), []byte("y")}}, Overlap)
	mustRefuse(t, src, []Edit{{sp(4, 99), []byte("x")}}, OutOfBounds)
	mustRefuse(t, src, []Edit{{sp(-1, 2), []byte("x")}}, OutOfBounds)
	mustRefuse(t, src, []Edit{{sp(3, 1), []byte("x")}}, ReversedSpan)
}

func TestSourceUnmodified(t *testing.T) {
	src := []byte("version 1.0\n")
	orig := bytes.Clone(src)
	_, err := Apply(src, []Edit{{sp(8, 11), []byte("9.9")}})
	require.NoError(t, err)
	require.Equal(t, orig, src)
}
