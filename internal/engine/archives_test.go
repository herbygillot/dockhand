package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// An archive a build reported is kept once it arrives whole and matches
// its digest, and only then recorded, which makes it ready for dependents
// (decision 44). One kept already isn't fetched again; one that arrives
// damaged, or can't be named as a file, is kept nowhere.
func TestAnArchiveIsKeptOnlyWholeAndOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	content := []byte("libharbor's archive")
	sum := sha256.Sum256(content)
	archive := model.Archive{Digest: "sha256:" + hex.EncodeToString(sum[:]), Name: "libharbor-4_0.darwin_25.arm64.tbz2"}
	fetches := 0
	fetch := func(data []byte) func(string) error {
		return func(path string) error {
			fetches++
			return os.WriteFile(path, data, 0o644)
		}
	}

	require.ErrorContains(t, e.keepArchive(t.Context(), archive, fetch([]byte("damaged"))), "arrived as sha256:")
	_, kept, err := e.keptArchive(t.Context(), archive.Digest)
	require.NoError(t, err)
	require.False(t, kept, "a damaged archive isn't kept")

	require.NoError(t, e.keepArchive(t.Context(), archive, fetch(content)))
	found, kept, err := e.keptArchive(t.Context(), archive.Digest)
	require.NoError(t, err)
	require.True(t, kept)
	require.Equal(t, archive.Name, found.Name)
	require.Equal(t, int64(len(content)), found.Size)
	data, err := os.ReadFile(e.archivePath(archive.Digest))
	require.NoError(t, err)
	require.Equal(t, content, data)
	entries, err := os.ReadDir(e.ArchiveDirectory())
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing is left of the fetches but the archive")

	require.NoError(t, e.keepArchive(t.Context(), archive, fetch(content)))
	require.Equal(t, 2, fetches, "one kept already isn't fetched again")

	// A record whose file is gone isn't kept: it is fetched again.
	require.NoError(t, os.Remove(e.archivePath(archive.Digest)))
	_, kept, err = e.keptArchive(t.Context(), archive.Digest)
	require.NoError(t, err)
	require.False(t, kept)
	require.NoError(t, e.keepArchive(t.Context(), archive, fetch(content)))
	require.Equal(t, 3, fetches)
	require.FileExists(t, e.archivePath(archive.Digest))

	for _, name := range []string{"", "..", "../libharbor.tbz2", filepath.Join("devel", "libharbor.tbz2")} {
		named := archive
		named.Name = name
		require.Error(t, e.keepArchive(t.Context(), named, fetch(content)), "%q isn't a file's name", name)
	}
	require.Equal(t, 3, fetches, "nothing is fetched under a name that isn't a file's")
}
