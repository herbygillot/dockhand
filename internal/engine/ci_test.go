package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A check on macOS 26 alone leaves MacPorts' CI's 14 and 15 uncovered;
// one on the github provider runs MacPorts' own workflow, and leaves none.
func TestUncoveredCIIsWhatNoEnvironmentBuiltOn(t *testing.T) {
	tahoe := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	ci := []string{"14", "15", "26"}
	require.Equal(t, []string{"14", "15"}, uncoveredCI(ci, []model.Environment{tahoe}))
	require.Equal(t, []string{"14"}, uncoveredCI(ci, []model.Environment{tahoe, sequoia}))
	require.Empty(t, uncoveredCI(ci, []model.Environment{{Provider: buildenv.GitHub}}))
	require.Empty(t, uncoveredCI(nil, []model.Environment{tahoe}), "a tree whose workflow names none")
}

// ci reads MacPorts' CI's releases from the workflow at master as last
// fetched, as Tart names them.
func TestCIIsTheReleasesMastersWorkflowNames(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{".github/workflows/main.yml": "jobs:\n  build:\n    strategy:\n      matrix:\n        os: [macos-14, macos-15, macos-26]\n"})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "CI")
	e := f.open(t)
	_, err := e.ciSlugs(t.Context())
	require.ErrorContains(t, err, "master hasn't been fetched")
	_, err = e.Start(t.Context(), StartRequest{Name: "fetches-master"})
	require.NoError(t, err)
	slugs, err := e.ciSlugs(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"sonoma", "sequoia", "tahoe"}, slugs)
}
