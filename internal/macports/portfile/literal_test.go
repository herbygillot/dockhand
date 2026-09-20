package portfile_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

// A checksum held in a table and a revision the Portfile computes are each
// owned by the one place the Portfile writes them, however deep that is.
func TestUniqueLiteralFindsTheOneOwnerAtAnyDepth(t *testing.T) {
	t.Parallel()
	src := []byte("# rmd160 0123abcd in a comment does not count\n" +
		"set modules {\n    qtbase {{0123abcd 4567ef01} 7}\n}\n" +
		"subport qt-tools { revision 3 }\n" +
		"set y [lindex ${module_info} 0]\n" +
		"distname ${name}-1.2\n")
	span, ok := portfile.UniqueLiteral(src, "0123abcd")
	require.True(t, ok, "a digest in a table")
	require.Equal(t, "0123abcd", span.Text(src))
	span, ok = portfile.UniqueLiteral(src, "revision 3")
	require.True(t, ok, "the words revision 3 in a subport body")
	require.Equal(t, "revision 3", span.Text(src))
	_, ok = portfile.UniqueLiteral(src, "module_info")
	require.False(t, ok, "a variable name is not a token the Portfile writes as a value")
	_, ok = portfile.UniqueLiteral(src, "1.2")
	require.False(t, ok, "a fragment of a composed word is not a whole token")
	_, ok = portfile.UniqueLiteral(src, "lindex")
	require.True(t, ok, "a word inside a command substitution is reached")
	_, ok = portfile.UniqueLiteral([]byte("revision 3\nsubport x { revision 3 }\n"), "revision 3")
	require.False(t, ok, "two owners are no owner")
	_, ok = portfile.UniqueLiteral([]byte("# revision 3\n"), "revision 3")
	require.False(t, ok, "a comment is not an owner")
}
