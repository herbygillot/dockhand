package portedit

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProfilesIncludeBoundaryAndArchitectureWithoutImpossibleOldARM(t *testing.T) {
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	profiles, err := observationProfiles([]byte(`if {${os.major} >= 17} {version 1} else {version 0}
if {${build_arch} eq "arm64"} {distfiles a} else {distfiles b}`), native)
	require.NoError(t, err)
	require.Equal(t, native, profiles[0])
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"})
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"})
	require.NotContains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "arm64"})
	_, err = observationProfiles([]byte(`if {${os.major} >= $minimum} {version 1}`), native)
	require.ErrorIs(t, err, ErrProbeInconclusive)
}
