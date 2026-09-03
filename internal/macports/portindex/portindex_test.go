package portindex

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

// entryText renders one entry the way MacPorts' portindex writes it:
// a "name length" header line, then the payload and its newline, with
// no separator before the next entry. The declared length covers that
// newline and is counted in UTF-16 code units — Tcl's own `string
// length` — which is why it is spelled out here rather than taken from
// len(). A fixture that writes a byte count, as this one used to,
// agrees with a reader that cannot read a real tree.
func entryText(name, payload string) string {
	body := payload + "\n"
	return fmt.Sprintf("%s %d\n%s", name, len(utf16.Encode([]rune(body))), body)
}

// writeIndex writes a synthetic PortIndex (and optionally its quick
// accelerator) from name→payload pairs, in order.
func writeIndex(t *testing.T, root string, entries [][2]string, withQuick bool) {
	t.Helper()
	var index, quick strings.Builder
	for _, e := range entries {
		name, payload := e[0], e[1]
		fmt.Fprintf(&quick, "%s %d\n", strings.ToLower(name), index.Len())
		index.WriteString(entryText(name, payload))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte(index.String()), 0o644))
	if withQuick {
		require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexQuickFile), []byte(quick.String()), 0o644))
	}
}

var testEntries = [][2]string{
	{"kubectl", `name kubectl portdir sysutils/kubectl version 1.34.0 maintainers {{@patarra gmail.com:patarra} openmaintainer}`},
	{"kubectl-1.37", `name kubectl-1.37 portdir sysutils/kubectl version 1.37.0 description {Kubernetes cluster CLI}`},
	{"py314-numpy", `name py314-numpy portdir python/py-numpy version 2.3.2`},
}

func TestLookup(t *testing.T) {
	for _, withQuick := range []bool{true, false} {
		root := t.TempDir()
		writeIndex(t, root, testEntries, withQuick)
		ix, err := Open(root)
		require.NoError(t, err, "withQuick=%v", withQuick)
		assert.Equal(t, len(testEntries), ix.Len())

		sub, err := ix.Lookup("kubectl-1.37")
		require.NoError(t, err)
		assert.Equal(t, "kubectl-1.37", sub.Name)
		assert.Equal(t, "sysutils/kubectl", sub.Portdir)
		assert.Equal(t, "1.37.0", sub.Fields["version"])

		// Subport of a portdir whose name shares no prefix with it.
		py, err := ix.Lookup("py314-numpy")
		require.NoError(t, err)
		assert.Equal(t, "python/py-numpy", py.Portdir)

		// Braced payload values survive the list lens.
		top, err := ix.Lookup("kubectl")
		require.NoError(t, err)
		assert.Equal(t, "{@patarra gmail.com:patarra} openmaintainer", top.Fields["maintainers"])

		_, err = ix.Lookup("no-such-port")
		require.ErrorIs(t, err, ErrNotIndexed)
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, root, testEntries, true)
	ix, err := Open(root)
	require.NoError(t, err)
	e, err := ix.Lookup("KUBECTL-1.37")
	require.NoError(t, err)
	assert.Equal(t, "kubectl-1.37", e.Name)
}

func TestStaleQuickTriggersRescan(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, root, testEntries, true)
	// Regenerate the PortIndex with an extra leading entry, leaving the
	// quick accelerator behind: every stored offset now points at the
	// wrong entry, and the new name is absent from quick entirely.
	grown := append([][2]string{{"aalib", `name aalib portdir graphics/aalib version 1.4rc5`}}, testEntries...)
	writeIndex(t, root, grown, false)
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexQuickFile), quickFor(testEntries), 0o644))

	ix, err := Open(root)
	require.NoError(t, err)
	e, err := ix.Lookup("kubectl-1.37")
	require.NoError(t, err)
	assert.Equal(t, "sysutils/kubectl", e.Portdir)
	e, err = ix.Lookup("aalib")
	require.NoError(t, err)
	assert.Equal(t, "graphics/aalib", e.Portdir)
}

// quickFor builds accelerator content whose offsets assume entries was
// the index's layout.
func quickFor(entries [][2]string) []byte {
	var quick strings.Builder
	pos := 0
	for _, e := range entries {
		name, payload := e[0], e[1]
		fmt.Fprintf(&quick, "%s %d\n", strings.ToLower(name), pos)
		pos += len(entryText(name, payload))
	}
	return []byte(quick.String())
}

func TestOpenErrors(t *testing.T) {
	_, err := Open(t.TempDir())
	require.ErrorIs(t, err, ErrNoIndex)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte("not an index at all\n"), 0o644))
	_, err = Open(root)
	require.ErrorIs(t, err, ErrMalformed)
}
