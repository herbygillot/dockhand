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

func TestProfilesRefuseUnresolvedReadsAlongsideKnownBoundaries(t *testing.T) {
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	for _, source := range []string{
		`set major ${os.major}; if {$major >= 17} {version 1}`,
		`if {${os.major} >= 17 && ${os.major} < $limit} {version 1}`,
		`if {${os.major} >= 17 + 5} {version 1}`,
		`if {${os.major} >= 17.5} {version 1}`,
		`if {17 + 25 < ${os.major}} {version 1}`,
		`if {${os.version} eq "25.1.0"} {version 1}`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := observationProfiles([]byte(source), native)
			require.ErrorIs(t, err, ErrProbeInconclusive)
		})
	}
}
