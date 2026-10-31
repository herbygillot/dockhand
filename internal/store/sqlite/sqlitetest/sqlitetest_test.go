package sqlitetest_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/store/sqlite"
	"github.com/herbygillot/dockhand/internal/store/sqlite/sqlitetest"
)

// A fresh database opens as this build's schema, migrating nothing, and
// each is its own: what one records, another doesn't hold.
func TestAFreshDatabaseIsCurrentAndItsOwn(t *testing.T) {
	t.Parallel()
	first := sqlitetest.Fresh(t, filepath.Join(t.TempDir(), "home", ".dockhand", "dockhand.db"))
	s, err := sqlite.Open(t.Context(), first, sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	require.Nil(t, s.Migration(), "nothing to migrate")
	_, err = s.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)

	second, err := sqlite.Open(t.Context(), sqlitetest.Fresh(t, filepath.Join(t.TempDir(), "dockhand.db")), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { second.Close() })
	dirs, err := second.Repositories(t.Context())
	require.NoError(t, err)
	require.Empty(t, dirs)
}
