package portfile

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRevisionEditPreservesSurroundingSourceAndScope(t *testing.T) {
	source := []byte("# revision 99\r\nversion 1.2\r\nrevision   7 ; # retain comment\r\nsubport child {\r\n    revision 3\r\n}\r\n")
	got, err := BumpRevision(source, "", 7)
	require.NoError(t, err)
	require.Equal(t, "# revision 99\r\nversion 1.2\r\nrevision   8 ; # retain comment\r\nsubport child {\r\n    revision 3\r\n}\r\n", string(got))
	got, err = BumpRevision(source, "child", 3)
	require.NoError(t, err)
	require.Contains(t, string(got), "revision   7 ; # retain comment")
	require.Contains(t, string(got), "    revision 4\r\n")
	for source, want := range map[string]string{
		"version 1": "version 1\nrevision 1",
		"name  jq\nversion  1.8.1\n\nchecksums x\n":                        "name  jq\nversion  1.8.1\nrevision 1\n\nchecksums x\n",
		"name                jq\nversion             1.8.1\nlicense MIT\n": "name                jq\nversion             1.8.1\nrevision            1\nlicense MIT\n",
		"# no explicit revision\r\nversion 1\r\n":                          "# no explicit revision\r\nversion 1\r\nrevision 1\r\n",
		"github.setup jqlang jq 1.8.1\n":                                   "github.setup jqlang jq 1.8.1\n\nrevision                1\n",
	} {
		got, err := BumpRevision([]byte(source), "", 0)
		require.NoError(t, err)
		require.Equal(t, want, string(got), source)
	}
	for _, source := range []string{"revision [expr {1+1}]", "revision $current", "revision 1\nrevision 2", "revision 3", "revision {"} {
		_, err := BumpRevision([]byte(source), "", 2)
		require.ErrorIs(t, err, ErrUnsupported)
	}
	_, err = BumpRevision([]byte("version 1"), "dynamic-child", 0)
	require.ErrorIs(t, err, ErrUnsupported)
	_, err = BumpRevision([]byte("version 1"), "", int(^uint(0)>>1))
	require.ErrorIs(t, err, ErrUnsupported)
}
