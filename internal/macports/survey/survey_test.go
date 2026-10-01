package survey

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// A survey of named ports brings their directories and _resources, not
// the whole tree: outdated hugo spent most of its 7 seconds materializing
// every port and removing them again (batch 14).
func TestASurveyOfNamedPortsBringsOnlyThem(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{"_resources/port1.0/group/fixture-1.0.tcl", "devel/alpha/Portfile", "devel/alpha/files/patch-a.diff", "devel/beta/Portfile"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(file)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, file), []byte("PortSystem 1.0\n"), 0o600))
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	t.Setenv("TMPDIR", t.TempDir())

	files, err := Open(t.Context(), repo, nil, model.Platform{}, nil, Selection{Ports: []string{"devel/alpha/Portfile", "beta"}})
	require.NoError(t, err)
	defer func() { require.NoError(t, files.Close()) }()
	require.False(t, files.Projection.Whole())
	require.FileExists(t, filepath.Join(files.Root, "devel/alpha/files/patch-a.diff"))
	require.FileExists(t, filepath.Join(files.Root, "_resources/port1.0/group/fixture-1.0.tcl"))
	// A bare name no index placed is brought as it's resolved, which walks
	// the categories.
	require.NoFileExists(t, filepath.Join(files.Root, "devel/beta/Portfile"))
}
