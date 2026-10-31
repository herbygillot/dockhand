package patchcheck

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Refresh reads which file each hunk placed elsewhere belongs to from BSD
// patch's output and from GNU patch's alike: a Linux host's "patching
// file" named none, so refresh re-emitted nothing there (the test
// analysis's step b).
func TestPlacedHunksAreReadFromEitherPatch(t *testing.T) {
	t.Parallel()
	bsd := "Hmm...  Looks like a unified diff to me...\nPatching file 'src/a.c' using Plan A...\nHunk #1 succeeded at 12 (offset 2 lines).\nPatching file src/b.c using Plan A...\nHunk #1 succeeded at 4 with fuzz 1.\n"
	gnu := "patching file src/a.c\nHunk #1 succeeded at 12 (offset 2 lines).\npatching file src/b.c\nHunk #1 succeeded at 4 with fuzz 1.\n"
	for name, output := range map[string]string{"BSD": bsd, "GNU": gnu} {
		placed := placedByFile([]byte(output))
		require.Len(t, placed["src/a.c"], 1, name)
		require.Equal(t, 2, placed["src/a.c"][0].Offset, name)
		require.Len(t, placed["src/b.c"], 1, name)
		require.Equal(t, 1, placed["src/b.c"][0].Fuzz, name)
	}
}
