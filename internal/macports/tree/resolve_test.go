package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// treeWith builds a minimal ports tree with the given category/dir
// portdirs.
func treeWith(t *testing.T, portdirs ...string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	for _, pd := range portdirs {
		dir := filepath.Join(root, filepath.FromSlash(pd))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte("PortSystem 1.0\n"), 0o644))
	}
	return root
}

// writeIndex writes a PortIndex of name→portdir entries at the tree
// root. The declared length covers the payload's own trailing newline —
// there is no separator byte between entries — and counts UTF-16 code
// units, which for these ASCII payloads is the character count. The
// real thing is the checked-in slice these tests' neighbours use; this
// one exists so a resolution test can name its own portdirs.
func writeIndex(t *testing.T, root string, entries map[string]string) {
	t.Helper()
	var index strings.Builder
	for name, portdir := range entries {
		body := fmt.Sprintf("name %s portdir %s\n", name, portdir)
		fmt.Fprintf(&index, "%s %d\n%s", name, len(utf16.Encode([]rune(body))), body)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte(index.String()), 0o644))
}

func TestPathTarget(t *testing.T) {
	root := treeWith(t, "sysutils/kubectl")
	portdir := filepath.Join(root, "sysutils", "kubectl")

	// An absolute path needs no tree.
	tgt, ok := PathTarget(portdir)
	require.True(t, ok)
	assert.Equal(t, Target{Portdir: portdir}, tgt)

	// "." from inside the portdir, resolved to absolute.
	t.Chdir(portdir)
	tgt, ok = PathTarget(".")
	require.True(t, ok)
	assert.Equal(t, portdir, tgt.Portdir)

	// A directory with no Portfile is not a portdir; neither is a name.
	_, ok = PathTarget(root)
	assert.False(t, ok)
	_, ok = PathTarget("kubectl")
	assert.False(t, ok)
}

func TestResolveCategoryDir(t *testing.T) {
	root := treeWith(t, "sysutils/kubectl")
	tr, err := Open(root)
	require.NoError(t, err)

	// category/dir, relative to the tree root, from elsewhere.
	t.Chdir(t.TempDir())
	tgt, err := tr.Resolve("sysutils/kubectl")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "sysutils", "kubectl"), tgt.Portdir)
}

func TestResolveNames(t *testing.T) {
	root := treeWith(t, "sysutils/kubectl", "python/py-numpy")
	writeIndex(t, root, map[string]string{
		"kubectl":      "sysutils/kubectl",
		"kubectl-1.37": "sysutils/kubectl",
		"py314-numpy":  "python/py-numpy",
	})
	tr, err := Open(root)
	require.NoError(t, err)

	// A subport name resolves through the index, carrying the subport.
	tgt, err := tr.Resolve("kubectl-1.37")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "sysutils", "kubectl"), tgt.Portdir)
	assert.Equal(t, "kubectl-1.37", tgt.Subport)

	// Case-insensitively, per the port client's convention.
	tgt, err = tr.Resolve("PY314-NumPy")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "python", "py-numpy"), tgt.Portdir)
	assert.Equal(t, "py314-numpy", tgt.Subport)

	_, err = tr.Resolve("no-such-port")
	require.ErrorIs(t, err, ErrPortNotFound)
}

func TestResolveDirnameFallback(t *testing.T) {
	// No index at all: directory names still resolve, and misses hint
	// at portindex.
	root := treeWith(t, "sysutils/kubectl")
	tr, err := Open(root)
	require.NoError(t, err)
	tgt, err := tr.Resolve("kubectl")
	require.NoError(t, err)
	assert.Equal(t, Target{Portdir: filepath.Join(root, "sysutils", "kubectl")}, tgt)

	_, err = tr.Resolve("kubectl-1.37")
	require.ErrorIs(t, err, ErrPortNotFound)
	require.ErrorContains(t, err, "portindex")

	// A stale index entry pointing at a vanished portdir falls through
	// to the directory walk. A fresh Tree, since the index caches.
	writeIndex(t, root, map[string]string{"kubectl": "gone/kubectl"})
	tr, err = Open(root)
	require.NoError(t, err)
	tgt, err = tr.Resolve("kubectl")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "sysutils", "kubectl"), tgt.Portdir)
	assert.Empty(t, tgt.Subport)
}

func TestTreeResolveCachesIndex(t *testing.T) {
	root := treeWith(t, "sysutils/kubectl")
	writeIndex(t, root, map[string]string{"kubectl-1.37": "sysutils/kubectl"})
	tr, err := Open(root)
	require.NoError(t, err)

	tgt, err := tr.Resolve("kubectl-1.37")
	require.NoError(t, err)
	assert.Equal(t, "kubectl-1.37", tgt.Subport)

	// One open per Tree: repeated use hands back the same Index.
	idx1, err := tr.index()
	require.NoError(t, err)
	idx2, err := tr.index()
	require.NoError(t, err)
	assert.Same(t, idx1, idx2)
}

func TestOpenRejectsNonTree(t *testing.T) {
	_, err := Open(t.TempDir())
	require.ErrorIs(t, err, ErrNotPortsTree)
}

func TestTargetPortfile(t *testing.T) {
	p, err := Target{Portdir: "/x/sysutils/foo"}.Portfile()
	require.NoError(t, err)
	assert.Equal(t, "/x/sysutils/foo/Portfile", p)

	_, err = Target{}.Portfile()
	require.ErrorIs(t, err, ErrNoPortdir)
}
