package tart

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestRequireIndexedTarget(t *testing.T) {
	root := t.TempDir()
	fields := "portdir devel/working name working version 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(fmt.Sprintf("working %d\n%s", len(fields), fields)), 0600))
	require.NoError(t, requireIndexedTarget(root, record.Target{Name: "working", Portfile: "devel/working/Portfile"}))
	require.ErrorContains(t, requireIndexedTarget(root, record.Target{Name: "missing", Portfile: "devel/missing/Portfile"}), "not indexed")
	require.ErrorContains(t, requireIndexedTarget(root, record.Target{Name: "working", Portfile: "devel/other/Portfile"}), "belongs to")
}
