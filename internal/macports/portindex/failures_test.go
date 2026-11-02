package portindex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestFailureLinesAreKeptByDirectory(t *testing.T) {
	t.Parallel()
	output := []byte(`Creating port index in /tmp/index
Failed to parse file devel/working/Portfile with subport 'working-devel': invalid command name "boom"
Failed to parse file devel/working/Portfile: invalid command name "versio"
Failed to parse file security/KeePassXC/Portfile with subport 'keepassxc-devel': no such variable
Failed to parse file notaport: whatever
Failed to parse file a/b/c/Portfile: too deep

Total number of ports parsed:	3
`)
	require.Equal(t, map[string]string{
		"devel/working":      `Failed to parse file devel/working/Portfile: invalid command name "versio"`,
		"security/KeePassXC": `Failed to parse file security/KeePassXC/Portfile with subport 'keepassxc-devel': no such variable`,
	}, parseFailures(output), "the main port's line before a subport's, and nothing that names no port directory")
	require.Nil(t, parseFailures([]byte("Creating port index in /tmp/index\n")))
}

// A Portfile that doesn't parse is left out of the index and the run goes
// on: without -e portindex exits 0, as name lookup's index is built, and
// with -e it exits 2, its index written, which a strict incremental pass
// accepts where coverage holds. Either way the line it wrote is kept, so
// a lookup can say why a port is missing (herbygillot/dockhand#8).
func TestAPortfilePortindexCannotParseIsRecordedWithItsLine(t *testing.T) {
	t.Parallel()
	executable := testsupport.MacPortsTool(t, "portindex")
	root := t.TempDir()
	put := func(name, contents string) {
		file := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, []byte(contents), 0600))
	}
	put("devel/working/Portfile", "PortSystem 1.0\nname working\nversion 1\ncategories devel\n")
	put("security/KeePassXC/Portfile", "PortSystem 1.0\nname keepassxc\nversio 2.8.0\ncategories security\n")
	config, err := ResolveTool(t.Context(), Config{Env: os.Environ(), Executable: executable})
	require.NoError(t, err)

	lenient := filepath.Join(t.TempDir(), "lenient")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, nil, lenient, "", nil, false, nil, testGeneration()), "without -e the run succeeds, the broken port left out")
	index, err := Open(lenient)
	require.NoError(t, err)
	_, err = index.Lookup("keepassxc")
	require.ErrorIs(t, err, ErrNotIndexed)
	reason, ok := index.Failed("security/KeePassXC")
	require.True(t, ok)
	require.Contains(t, reason, `invalid command name "versio"`)
	_, ok = index.Failed("devel/working")
	require.False(t, ok, "a directory that parsed has no failure")

	put("devel/working/Portfile", "PortSystem 1.0\nname working\nversion 1\ncategories devel\nrevision 1\n")
	strict := filepath.Join(t.TempDir(), "strict")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, nil, strict, lenient, []string{"devel/working/Portfile"}, true, nil, testGeneration()), "a strict incremental pass exits 2 for the unchanged broken port, and its coverage holds")
	index, err = Open(strict)
	require.NoError(t, err)
	reason, ok = index.Failed("security/KeePassXC")
	require.True(t, ok, "portindex retries a port its seed lacks, so the failure is said again and kept on the strict path")
	require.Contains(t, reason, `invalid command name "versio"`)

	require.Error(t, buildPortIndex(t.Context(), config, testPlatform, root, nil, filepath.Join(t.TempDir(), "full-strict"), "", nil, true, nil, testGeneration()), "a strict full pass fails the run on any Portfile that doesn't parse")
}
