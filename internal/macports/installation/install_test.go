package installation

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// An installation keeps each port's archive, by macports.conf's documented
// setting, added once.
func TestAnInstallationKeepsEachPortsArchive(t *testing.T) {
	prefix := t.TempDir()
	conf := filepath.Join(prefix, "etc", "macports", "macports.conf")
	require.NoError(t, os.MkdirAll(filepath.Dir(conf), 0o755))
	require.NoError(t, os.WriteFile(conf, []byte("#portimage_mode         directory_and_archive\nbuildfromsource ifneeded\n"), 0o644))
	bin := t.TempDir()
	testsupport.WriteExecutable(t, filepath.Join(bin, "sudo"), "#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n")
	guest := func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, args[0], args[1:]...)
		command.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
		command.Stdin = input
		return command.CombinedOutput()
	}

	require.NoError(t, KeepArchives(t.Context(), guest, prefix))
	require.NoError(t, KeepArchives(t.Context(), guest, prefix))
	data, err := os.ReadFile(conf)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "\nportimage_mode directory_and_archive\n"), "added once, after the file's own settings")
	require.True(t, strings.HasSuffix(string(data), "\nportimage_mode directory_and_archive\n"))
	require.Contains(t, string(data), "buildfromsource ifneeded\n", "the file's own settings stay")
}
