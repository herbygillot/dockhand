package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/stretchr/testify/require"
)

func TestImageCapabilitiesAreSharedByProviderAndEnvironmentDigest(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "state.db"), Options{})
	require.NoError(t, err)
	defer store.Close()

	observed := time.Now().UTC().Truncate(time.Millisecond)
	want := state.ImageCapabilities{
		Provider: "tart", EnvironmentDigest: "sha256:image", CapabilityDigest: "sha256:capabilities", ObservedAt: observed,
		Capabilities: record.EnvironmentCapabilities{
			Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, MacPortsPrefix: "/opt/local",
			MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsXcode, XcodeVersion: "26.0.1",
		},
	}
	require.NoError(t, store.PutImageCapabilities(t.Context(), want))
	got, err := store.ImageCapabilities(t.Context(), want.Provider, want.EnvironmentDigest)
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, err = store.ImageCapabilities(t.Context(), "another-provider", want.EnvironmentDigest)
	require.ErrorIs(t, err, state.ErrNotFound)
	_, err = store.ImageCapabilities(t.Context(), want.Provider, "sha256:another-image")
	require.ErrorIs(t, err, state.ErrNotFound)
}
