package staging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestRequireIndexedTarget(t *testing.T) {
	root := t.TempDir()
	fields := "portdir devel/working name working version 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(fmt.Sprintf("working %d\n%s", len(fields), fields)), 0600))
	index, err := portindex.Open(root)
	require.NoError(t, err)
	require.NoError(t, requireIndexedTarget(index, record.Target{Name: "working", Portfile: "devel/working/Portfile"}))
	require.ErrorContains(t, requireIndexedTarget(index, record.Target{Name: "missing", Portfile: "devel/missing/Portfile"}), "not indexed")
	require.ErrorContains(t, requireIndexedTarget(index, record.Target{Name: "working", Portfile: "devel/other/Portfile"}), "belongs to")
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
