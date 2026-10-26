package guest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// What builds from source is said with what the guest is building.
func TestTheProgressSaysWhatBuildsFromSource(t *testing.T) {
	job := buildenv.Job{Environment: model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}},
		Targets: []buildenv.Target{{PlanTarget: model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor"}}}, {PlanTarget: model.PlanTarget{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}}}}}
	tahoe, err := macos.ReleaseForDarwin(25)
	require.NoError(t, err)
	line := buildingWords(job, tahoe, Results{Building: "libharbor", Phase: "dependencies", FromSource: []string{"rust", "cargo"}})
	require.Equal(t, "building libharbor (1 of 2): dependencies; rust, cargo: no archive for macOS 26 arm64; building from source", line)
}
