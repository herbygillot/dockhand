package staging

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func TestRequireIndexedTarget(t *testing.T) {
	root := t.TempDir()
	fields := "portdir devel/working name working version 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(fmt.Sprintf("working %d\n%s", len(fields), fields)), 0600))
	index, err := portindex.Open(root)
	require.NoError(t, err)
	require.NoError(t, requireIndexedTarget(index, model.Target{Name: "working", Portfile: "devel/working/Portfile"}))
	require.ErrorContains(t, requireIndexedTarget(index, model.Target{Name: "missing", Portfile: "devel/missing/Portfile"}), "not indexed")
	require.ErrorContains(t, requireIndexedTarget(index, model.Target{Name: "working", Portfile: "devel/other/Portfile"}), "belongs to")
}

func TestInvalidPayloadAndCancellationPreserveExistingArchive(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "input.tar")
	require.NoError(t, os.WriteFile(destination, []byte("previous archive"), 0600))
	for _, name := range []string{"ports", "../escape", "ports/Portfile", "/absolute", "."} {
		err := Archive(t.Context(), nil, nil, Request{}, destination, map[string][]byte{name: []byte("bad")}, nil)
		require.ErrorContains(t, err, "invalid provider payload")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, Archive(ctx, nil, nil, Request{}, destination, nil, nil), context.Canceled)
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "previous archive", string(data))
	entries, err := os.ReadDir(filepath.Dir(destination))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// The archive holds the index staged for its release, not the root's,
// which another release staging the same revision at once installs its own
// into, nor a file of one being installed (batch 14).
func TestTheArchiveHoldsTheIndexStagedForItsRelease(t *testing.T) {
	root, staged := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel", "working"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel", "working", "Portfile"), []byte("PortSystem 1.0\n"), 0o600))
	for name, text := range map[string]string{"PortIndex": "another release's\n", "PortIndex.quick": "another release's\n", ".PortIndex.123": "half installed\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(text), 0o600))
	}
	for _, name := range []string{"PortIndex", "PortIndex.quick"} {
		require.NoError(t, os.WriteFile(filepath.Join(staged, name), []byte("this release's\n"), 0o600))
	}
	archive, err := os.Create(filepath.Join(t.TempDir(), "input.tar"))
	require.NoError(t, err)
	require.NoError(t, packSource(t.Context(), root, []string{filepath.Join(staged, "PortIndex"), filepath.Join(staged, "PortIndex.quick")}, map[string][]byte{"input.json": []byte("{}")}, archive))
	_, err = archive.Seek(0, io.SeekStart)
	require.NoError(t, err)
	packed := map[string]string{}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		packed[header.Name] = string(data)
	}
	require.NoError(t, archive.Close())
	require.Equal(t, "this release's\n", packed["ports/PortIndex"])
	require.Equal(t, "this release's\n", packed["ports/PortIndex.quick"])
	require.NotContains(t, packed, "ports/.PortIndex.123")
	require.Contains(t, packed, "ports/devel/working/Portfile")
	require.Contains(t, packed, "input.json")
}
