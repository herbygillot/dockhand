package tart

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestBuildConfigDoesNotInitializeRuntimeDirectories(t *testing.T) {
	root := t.TempDir()
	p := &Provider{Config: Config{Home: filepath.Join(root, "home"), ArtifactDirectory: filepath.Join(root, "artifacts"), PortIndexExecutable: fakePortIndex(t)}, backend: newMachine()}
	_, err := p.BuildConfig(t.Context(), testPlatform, BuildOptions{Tests: record.TestDeclared})
	require.NoError(t, err)
	require.NoDirExists(t, p.Config.Home)
	require.NoDirExists(t, p.Config.ArtifactDirectory)
}
