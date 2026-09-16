package dependency

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManifestOwnershipHonorsExtractionDirectory(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"other/Cargo.lock": "lock", "actual/subdir/Cargo.lock": "nested"})
	require.NoError(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "actual/subdir"}, false))
	require.ErrorIs(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "missing/subdir"}, false), ErrManifestMissing)
	require.NoError(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "renamed/subdir"}, true))
	require.Error(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "../actual"}, false))
}
