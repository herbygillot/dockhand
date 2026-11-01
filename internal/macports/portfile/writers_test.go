package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SetLiteral finds the one literal `set` of a variable, anywhere, and
// nothing where it's set twice or computed; DeclareSubportChecksums writes
// zeroed placeholders after a subport block's last fetch line, naming each
// archive where there are several, and nothing where the block declares
// checksums already or has no one literal name. Each ran only behind
// tclsh (the test analysis's step b).
func TestPortfilesPureGoWriters(t *testing.T) {
	t.Parallel()
	src := []byte("name mysql9\nsubport mysql9-client {\n    set revision_client 1\n    revision ${revision_client}\n}\n")
	span, value, ok := SetLiteral(src, "revision_client")
	require.True(t, ok)
	require.Equal(t, "1", value)
	require.Equal(t, "1", string(src[span.Start:span.End]))
	_, _, ok = SetLiteral([]byte("set v 1\nset v 2\n"), "v")
	require.False(t, ok, "set twice")
	_, _, ok = SetLiteral([]byte("set v [expr 1]\n"), "v")
	require.False(t, ok, "computed")
	_, _, ok = SetLiteral([]byte("set w 1\n"), "v")
	require.False(t, ok, "never set")

	block := "name kb\nversion 0.2.0\nchecksums rmd160 1 sha256 2 size 3\nsubport kb-old {\n    version 0.1.6\n    distname kb-0.1.6\n    description old\n}\n"
	got, ok := DeclareSubportChecksums([]byte(block), "kb-old", []string{"kb-0.1.6.tar.gz"})
	require.True(t, ok)
	require.Contains(t, string(got), "    distname kb-0.1.6\n    checksums")
	require.Contains(t, string(got), "rmd160  0")
	require.Contains(t, string(got), "sha256  0")
	require.Contains(t, string(got), "size    0")
	require.NotContains(t, string(got), "kb-0.1.6.tar.gz \\", "one archive isn't named")
	several, ok := DeclareSubportChecksums([]byte(block), "kb-old", []string{"kb-0.1.6.tar.gz", "kb-data.tar.gz"})
	require.True(t, ok)
	require.Contains(t, string(several), "kb-data.tar.gz", "several are each named")
	_, ok = DeclareSubportChecksums([]byte("name kb\nsubport kb-old {\n    version 0.1.6\n    checksums sha256 1\n}\n"), "kb-old", []string{"kb.tar.gz"})
	require.False(t, ok, "it declares them already")
	_, ok = DeclareSubportChecksums([]byte("name kb\nforeach v {1 2} {\n    subport kb-$v {}\n}\n"), "kb-1", []string{"kb.tar.gz"})
	require.False(t, ok, "no block with a literal name")
}

// A main port declaring no checksums gets placeholders at its top after
// its last fetch line, aligned as it is; one declaring some, or with no
// fetch line, gets none.
func TestAMainPortsChecksumsAreDeclaredAfterItsFetchLines(t *testing.T) {
	got, ok := DeclareChecksums([]byte("github.setup        owner tool 1.0\nrevision            0\ndistname            tool_1.0\n\nhomepage            x\n"), []string{"tool_1.0.tar.gz"})
	require.True(t, ok)
	require.Equal(t, "github.setup        owner tool 1.0\nrevision            0\ndistname            tool_1.0\n\nchecksums           rmd160  0 \\\n                    sha256  0 \\\n                    size    0\n\nhomepage            x\n", string(got))
	_, ok = DeclareChecksums([]byte("version 1\nchecksums sha256 1\n"), []string{"a.tar.gz"})
	require.False(t, ok)
	_, ok = DeclareChecksums([]byte("name tool\n"), []string{"a.tar.gz"})
	require.False(t, ok)
}
