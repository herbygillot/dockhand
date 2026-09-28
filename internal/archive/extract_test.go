package archive

import (
	"archive/tar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractSetsAsideTheVersionedTopDirectory(t *testing.T) {
	archive := tarball(t, "jq-1.8.1", map[string]string{"src/jq.c": "int main;\n", "NEWS": "1.8.1\n"})
	dir := t.TempDir()
	n, err := Extract(t.Context(), archive, dir)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	data, err := os.ReadFile(filepath.Join(dir, "src/jq.c"))
	require.NoError(t, err)
	require.Equal(t, "int main;\n", string(data))

	zipped := zipball(t, "jq-1.8.1", map[string]string{"a.txt": "a", "b/c.txt": "c"})
	dir = t.TempDir()
	_, err = Extract(t.Context(), zipped, dir)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "b/c.txt"))

	// Members with no shared top keep their paths.
	name := filepath.Join(t.TempDir(), "flat.tar")
	out, err := os.Create(name)
	require.NoError(t, err)
	tw := tar.NewWriter(out)
	for _, member := range []string{"a.txt", "b/c.txt"} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: member, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
		_, err = tw.Write([]byte("x"))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, out.Close())
	dir = t.TempDir()
	_, err = Extract(t.Context(), name, dir)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "a.txt"))
	require.FileExists(t, filepath.Join(dir, "b/c.txt"))
}

func TestExtractRefusesAMemberOutsideIt(t *testing.T) {
	name := filepath.Join(t.TempDir(), "evil.tar")
	out, err := os.Create(name)
	require.NoError(t, err)
	tw := tar.NewWriter(out)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
	_, err = tw.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, out.Close())
	_, err = Extract(t.Context(), name, t.TempDir())
	require.ErrorContains(t, err, "member outside it")
}

func TestWalkReadsXZThroughTheSystemTool(t *testing.T) {
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("no xz here")
	}
	gz := tarball(t, "jq-1.8.1", map[string]string{"NEWS": "1.8.1\n"})
	plain := filepath.Join(t.TempDir(), "jq-1.8.1.tar")
	data, err := exec.Command("sh", "-c", "gzip -dc "+gz+" > "+plain+" && xz "+plain).CombinedOutput()
	require.NoError(t, err, string(data))
	dir := t.TempDir()
	n, err := Extract(t.Context(), plain+".xz", dir)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.FileExists(t, filepath.Join(dir, "NEWS"))
}

// A tarball names its directories too, each with a trailing slash, and
// GitHub's begin with a pax header that names the commit; some name their
// root, as ./. None of them is outside it, or a file sharing its top
// directory, and a directory that would leave it is refused as a file is.
func TestExtractTakesDirectoriesAsTarNamesThem(t *testing.T) {
	write := func(members ...string) string {
		t.Helper()
		name := filepath.Join(t.TempDir(), "source.tar")
		out, err := os.Create(name)
		require.NoError(t, err)
		tw := tar.NewWriter(out)
		require.NoError(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "0c0c3e5"}}))
		for _, member := range members {
			if strings.HasSuffix(member, "/") {
				require.NoError(t, tw.WriteHeader(&tar.Header{Name: member, Mode: 0o755, Typeflag: tar.TypeDir}))
				continue
			}
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: member, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
			_, err = tw.Write([]byte("x"))
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, out.Close())
		return name
	}
	for _, members := range [][]string{
		{"duckdb-1.5.6/", "duckdb-1.5.6/src/", "duckdb-1.5.6/src/main.cpp", "duckdb-1.5.6/CMakeLists.txt"},
		{"./", "./duckdb-1.5.6/", "./duckdb-1.5.6/src/", "./duckdb-1.5.6/src/main.cpp", "./duckdb-1.5.6/CMakeLists.txt"},
	} {
		dir := t.TempDir()
		n, err := Extract(t.Context(), write(members...), dir)
		require.NoError(t, err, "%q", members)
		require.Equal(t, 2, n)
		require.FileExists(t, filepath.Join(dir, "src/main.cpp"))
		require.FileExists(t, filepath.Join(dir, "CMakeLists.txt"))
	}
	_, err := Extract(t.Context(), write("duckdb-1.5.6/", "../evil/"), t.TempDir())
	require.ErrorContains(t, err, "member outside it")
}
