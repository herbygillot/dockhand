package macos

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	require.Contains(t, string(output), "Xcode installation failed on macOS ")
	require.Contains(t, string(output), " GB was free in /private/tmp\n", "the free space, which a failed expansion turns on")
	require.NotContains(t, string(output), "workspace retained", "a path in a VM setup deletes named nothing reachable")
	paths, err := filepath.Glob(filepath.Join(root, "work.*", "Xcode.xip"))
	require.NoError(t, err)
	require.Len(t, paths, 1)
}

// An archive's copy is read for its size and sha256 where it is, as stat
// and shasum give them, to compare with the original before it's expanded
// (field testing, monterey's xip, 2026-10-16).
func TestAGuestCopysDigestIsRead(t *testing.T) {
	// The guest's commands are macOS's, as BSD stat -f, which a Linux
	// host's GNU stat reads as another flag.
	if runtime.GOOS != "darwin" {
		t.Skip("runs the guest's macOS commands on the host")
	}
	path := filepath.Join(t.TempDir(), "Xcode.xip")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	digest, err := GuestDigest(t.Context(), func(ctx context.Context, _ io.Reader, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	}, path)
	require.NoError(t, err)
	require.Equal(t, ArchiveDigest{Size: 7, SHA256: "f16d05ec6b29248d2c61adb1e9263f78e4f7bace1b955014a2d17872cfe4064d"}, digest)
	require.Equal(t, "7 bytes, sha256 f16d05ec6b29248d2c61adb1e9263f78e4f7bace1b955014a2d17872cfe4064d", digest.String())
}
