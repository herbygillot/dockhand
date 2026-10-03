package upstream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A port whose version is its source's with other separators takes the
// version as people type it in the source's spelling (field testing,
// batch 12: libunibreak's 8.0 is its tag's 8_0).
func TestARequestedVersionIsSpelledAsTheSourceSpellsIt(t *testing.T) {
	t.Parallel()
	require.Equal(t, "8_0", sourceSpelling("8.0", "7.0", "7_0"))
	require.Equal(t, "8_0", sourceSpelling("8_0", "7.0", "7_0"))
	require.Equal(t, "1.2.4", sourceSpelling("1.2.4", "1.2.3", "1.2.3"), "the same spelling")
	require.Equal(t, "2.0", sourceSpelling("2.0", "2026.1", "v1"), "unrelated spellings")
}
