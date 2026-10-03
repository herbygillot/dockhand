package observe

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A port that reads the architecture is sampled on both, on this release
// where each runs here: on macOS 27, which runs on arm64 alone, x86_64 is
// sampled on macOS 26, its newest release, so an Intel archive is still
// covered (the M1's quick stage, 2026-10-03).
func TestEachArchitectureIsSampledWhereItLastRuns(t *testing.T) {
	t.Parallel()
	goldenGate := model.Platform{OS: "darwin", Version: "27", Architecture: "arm64"}
	profiles, err := profilesForBoundaries(map[int]bool{}, true, goldenGate)
	require.NoError(t, err)
	require.Equal(t, []model.Platform{goldenGate, {OS: "darwin", Version: "25", Architecture: "x86_64"}}, profiles)

	tahoe := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	profiles, err = profilesForBoundaries(map[int]bool{}, true, tahoe)
	require.NoError(t, err)
	require.Equal(t, []model.Platform{tahoe, {OS: "darwin", Version: "25", Architecture: "x86_64"}}, profiles)

	profiles, err = profilesForBoundaries(map[int]bool{}, false, goldenGate)
	require.NoError(t, err)
	require.Equal(t, []model.Platform{goldenGate}, profiles, "a port that doesn't read it is sampled here alone")
}
