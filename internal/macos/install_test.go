package macos

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestXcodeExpansionStagesArchiveBesideItsOutput(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "Xcode archive.xip")
	require.NoError(t, os.WriteFile(archive, []byte("fixture"), 0600))
	xip := filepath.Join(root, "xip")
	// xip expands beside its input archive, which need not be the caller's cwd.
	testsupport.WriteExecutable(t, xip, "#!/bin/sh\nset -eu\nmkdir -p \"$(dirname \"$2\")/Xcode.app\"\n")
	err := InstallXcode(t.Context(), func(ctx context.Context, _ io.Reader, args ...string) ([]byte, error) {
		script, _, ok := strings.Cut(args[2], "sudo -n /bin/rm -rf /Applications/Xcode.app")
		require.True(t, ok)
		script = strings.ReplaceAll(script, "/usr/bin/xip", "\""+xip+"\"")
		script = strings.ReplaceAll(script, "/private/tmp/dockhand-xcode.XXXXXX", "\""+filepath.Join(root, "work.XXXXXX")+"\"")
		// Exercise actual shell staging and cleanup, stopping before host installation.
		script += "test -d Xcode.app\n"
		return exec.CommandContext(ctx, "/bin/sh", "-c", script, "dockhand", args[len(args)-1]).CombinedOutput()
	}, archive)
	require.NoError(t, err)
	require.NoFileExists(t, archive)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the temporary expansion workspace must be cleaned")
	require.Equal(t, "xip", entries[0].Name())
}

func TestFailedXcodeExpansionPreservesDiagnosticsWithoutRecursiveCleanup(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "Xcode.xip")
	require.NoError(t, os.WriteFile(archive, []byte("fixture"), 0600))
	var output []byte
	err := InstallXcode(t.Context(), func(ctx context.Context, _ io.Reader, args ...string) ([]byte, error) {
		script, _, ok := strings.Cut(args[2], "sudo -n /bin/rm -rf /Applications/Xcode.app")
		require.True(t, ok)
		script = strings.ReplaceAll(script, "/private/tmp/dockhand-xcode.XXXXXX", "\""+filepath.Join(root, "work.XXXXXX")+"\"")
		script = strings.ReplaceAll(script, "/usr/bin/xip --expand Xcode.xip", "echo 'fixture extraction failure' >&2; exit 42")
		var err error
		output, err = exec.CommandContext(ctx, "/bin/sh", "-c", script, "dockhand", archive).CombinedOutput()
		return output, err
	}, archive)
	require.ErrorContains(t, err, "fixture extraction failure")
	require.Contains(t, string(output), "workspace retained")
	paths, err := filepath.Glob(filepath.Join(root, "work.*", "Xcode.xip"))
	require.NoError(t, err)
	require.Len(t, paths, 1)
}
