package app

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/stretchr/testify/require"
)

// A release is named as setup names it, keeps the evaluated platform's
// operating system and architecture, and is built once however often it is
// named; available names what the local images serve.
func TestBuildPlatformsNameReleasesAndPreparedImages(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	executable := filepath.Join(directory, "tart")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\n[ \"$1\" = list ] || exit 1\necho '[{\"Name\":\"dockhand-base-sequoia\",\"Source\":\"local\",\"State\":\"stopped\"},{\"Name\":\"dockhand-xcode-sonoma\",\"Source\":\"local\",\"State\":\"stopped\"}]'\n")
	services := &Services{local: &tart.Provider{Config: tart.Config{Executable: executable, Home: directory}}}
	host := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	on := func(version string) record.Platform {
		return record.Platform{OS: "darwin", Version: version, Architecture: "arm64"}
	}

	platforms, err := services.buildPlatforms(t.Context(), host, []string{"sequoia", "14", "Sonoma"})
	require.NoError(t, err)
	require.Equal(t, []record.Platform{on("24"), on("23")}, platforms)

	platforms, err = services.buildPlatforms(t.Context(), host, []string{"tahoe", AvailablePlatforms})
	require.NoError(t, err)
	require.Equal(t, []record.Platform{on("25"), on("23"), on("24")}, platforms)

	platforms, err = services.buildPlatforms(t.Context(), host, nil)
	require.NoError(t, err)
	require.Empty(t, platforms, "nothing named builds on the evaluated platform")

	_, err = services.buildPlatforms(t.Context(), host, []string{"leopard"})
	require.ErrorContains(t, err, "unknown release")

	empty := filepath.Join(directory, "empty-tart")
	testsupport.WriteExecutable(t, empty, "#!/bin/sh\necho '[]'\n")
	services.local = &tart.Provider{Config: tart.Config{Executable: empty, Home: directory}}
	_, err = services.buildPlatforms(t.Context(), host, []string{AvailablePlatforms})
	require.ErrorContains(t, err, "no Tart image is prepared")
}
