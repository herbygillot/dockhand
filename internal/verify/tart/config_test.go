package tart

import (
	"github.com/herbygillot/dockhand/internal/verify"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestBuildConfigDoesNotInitializeRuntimeDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := &Provider{Config: Config{Home: filepath.Join(root, "home"), ArtifactDirectory: filepath.Join(root, "artifacts"), PortIndexExecutable: fakePortIndex(t)}, backend: newMachine()}
	_, err := p.BuildConfig(t.Context(), testPlatform, verify.BuildOptions{Tests: record.TestDeclared})
	require.NoError(t, err)
	require.NoDirExists(t, p.Config.Home)
	require.NoDirExists(t, p.Config.ArtifactDirectory)
}

// A release past the one Tart builds on unasked is refused when it is only
// the host's, and built when a person named it; a named release whose image
// is missing asks for setup of that release.
func TestNamedReleasesAreDeliberate(t *testing.T) {
	t.Parallel()
	goldenGate := record.Platform{OS: "darwin", Version: "26", Architecture: "arm64"}
	provider := &Provider{Config: Config{Home: t.TempDir(), ArtifactDirectory: t.TempDir(), PortIndexExecutable: fakePortIndex(t)}, backend: newMachine()}
	_, err := provider.BuildConfig(t.Context(), goldenGate, verify.BuildOptions{Tests: record.TestDeclared})
	require.ErrorIs(t, err, verify.ErrImageUnavailable)
	require.ErrorContains(t, err, "does not build on by default")

	config, err := provider.BuildConfig(t.Context(), goldenGate, verify.BuildOptions{Tests: record.TestDeclared, Named: true})
	require.NoError(t, err)
	require.Equal(t, goldenGate, config.Platform)
	require.Contains(t, string(config.ProviderConfig), "dockhand-base-golden-gate")

	machine := newMachine()
	machine.environmentError = verify.ErrImageUnavailable
	provider = &Provider{Config: Config{Home: t.TempDir(), ArtifactDirectory: t.TempDir(), PortIndexExecutable: fakePortIndex(t)}, backend: machine}
	sonoma := record.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}
	_, err = provider.BuildConfig(t.Context(), sonoma, verify.BuildOptions{Tests: record.TestDeclared, NeedsXcode: true, Named: true})
	require.ErrorIs(t, err, verify.ErrImageUnavailable)
	require.ErrorContains(t, err, "image dockhand-xcode-sonoma is unavailable; run dockhand setup --os sonoma --xcode")
}
