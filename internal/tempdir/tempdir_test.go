package tempdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNamesTheRootForTheProcess(t *testing.T) {
	r, err := New()
	require.NoError(t, err)
	defer r.Remove() //nolint:errcheck // test cleanup

	assert.Contains(t, filepath.Base(r.Path()), fmt.Sprintf("dockhand-%d-", os.Getpid()),
		"a leftover tree must be attributable to the run that made it")
	// The pid alone would collide across recycled pids, so the name must
	// carry more than it.
	assert.NotEqual(t, fmt.Sprintf("dockhand-%d-", os.Getpid()), filepath.Base(r.Path()))
	st, err := os.Stat(r.Path())
	require.NoError(t, err)
	assert.True(t, st.IsDir())
}

func TestTwoRootsNeverCollide(t *testing.T) {
	a, err := New()
	require.NoError(t, err)
	defer a.Remove() //nolint:errcheck // test cleanup
	b, err := New()
	require.NoError(t, err)
	defer b.Remove() //nolint:errcheck // test cleanup
	assert.NotEqual(t, a.Path(), b.Path())
}

func TestMakeDirNamesTheDirectoryForItsPurpose(t *testing.T) {
	r, err := New()
	require.NoError(t, err)
	defer r.Remove() //nolint:errcheck // test cleanup

	dir, remove, err := r.MakeDir("portfetch")
	require.NoError(t, err)
	assert.Equal(t, r.Path(), filepath.Dir(dir), "issued directories live under the root")
	assert.True(t, strings.HasPrefix(filepath.Base(dir), "portfetch-"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644))
	remove()
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "the remover takes the contents with it")
}

// Removing the root is the last-resort cleanup: it takes what the
// per-use removers did not.
func TestRemoveTakesEverythingUnderIt(t *testing.T) {
	r, err := New()
	require.NoError(t, err)
	a, _, err := r.MakeDir("shadow")
	require.NoError(t, err)
	b, _, err := r.MakeDir("distfiles")
	require.NoError(t, err)

	require.NoError(t, r.Remove())
	for _, d := range []string{a, b, r.Path()} {
		_, err := os.Stat(d)
		assert.True(t, os.IsNotExist(err), "%s should be gone", d)
	}
}

// The zero Root is what a caller with no run to belong to gets: it
// issues from the system temporary directory and owns nothing.
func TestZeroRootIssuesFromTheSystemTempDir(t *testing.T) {
	var r Root
	assert.Empty(t, r.Path())

	dir, remove, err := r.MakeDir("shadow")
	require.NoError(t, err)
	defer remove()
	assert.Equal(t, filepath.Clean(os.TempDir()), filepath.Dir(dir))
	assert.NoError(t, r.Remove(), "the zero Root owns nothing to remove")

	_, err = os.Stat(dir)
	assert.NoError(t, err, "removing the zero Root must not touch what it issued")
}
