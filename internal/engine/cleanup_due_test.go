package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
)

// caching is a provider that keeps a cache, as Tart keeps the vanilla images
// it pulled, and removes what it is told has gone unused.
type caching struct {
	scriptedProvider
	storage string
	unused  []time.Duration
}

func (c *caching) Storage() (string, error) { return c.storage, nil }

func (c *caching) PruneCache(_ context.Context, unused time.Duration) ([]string, error) {
	c.unused = append(c.unused, unused)
	return []string{"ghcr.io/cirruslabs/macos-sonoma-vanilla@sha256:aaaa"}, nil
}

// Automatic cleanup is due a day after the last, or, at most once an hour,
// when free space runs short where the database or a provider's cache is
// (decision 36).
func TestCleanupIsDueDailyOrWhenSpaceRunsShort(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	cache := &caching{storage: t.TempDir()}
	e.Providers = map[string]buildenv.Provider{"tart": cache}

	due, why := e.CleanupDue(CleanupEvery, 0)
	require.True(t, due, "never cleaned")
	require.Equal(t, CleanupReason{Words: "24h0m0s since the last"}, why)
	require.NoError(t, e.StampCleanup())
	due, _ = e.CleanupDue(CleanupEvery, 0)
	require.False(t, due, "just cleaned")
	due, _ = e.CleanupDue(CleanupEvery, 1<<62)
	require.False(t, due, "space is short, but the last pass was just now")

	stamp := e.cleanupStamp()
	twoHours := at.Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stamp, twoHours, twoHours))
	due, why = e.CleanupDue(CleanupEvery, 1<<62)
	require.True(t, due, "space is short, and an hour has passed")
	require.True(t, why.LowSpace)
	require.Contains(t, why.Words, " free where ")
	due, _ = e.CleanupDue(CleanupEvery, 1)
	require.False(t, due, "space is plenty")

	yesterday := at.Add(-25 * time.Hour)
	require.NoError(t, os.Chtimes(stamp, yesterday, yesterday))
	due, _ = e.CleanupDue(CleanupEvery, 0)
	require.True(t, due, "a day since the last")
}

// A pass removes what providers keep that has gone unused for CacheUnused.
func TestCleanupPrunesProvidersCaches(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	cache := &caching{storage: t.TempDir()}
	e.Providers = map[string]buildenv.Provider{"tart": cache}
	report, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{CacheUnused}, cache.unused)
	require.Equal(t, []string{"ghcr.io/cirruslabs/macos-sonoma-vanilla@sha256:aaaa"}, report.Caches)
}

// Each checkout keeps its own cleanup day. One database may serve two
// checkouts, and one stamp beside it let one checkout's commands hold off
// the other's cleanup of merged branches.
func TestEachCheckoutKeepsItsOwnCleanupDay(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	other := filepath.Join(t.TempDir(), "other-ports")
	run(t, filepath.Dir(other), "clone", "-q", f.upstream, other)
	options := f.options
	options.Tree, options.Worktrees = other, t.TempDir()
	o, err := Open(t.Context(), options)
	require.NoError(t, err)
	t.Cleanup(func() { o.Close() })
	require.NotEqual(t, e.Repository, o.Repository)

	require.NoError(t, e.StampCleanup())
	due, _ := e.CleanupDue(CleanupEvery, 0)
	require.False(t, due, "this checkout was just cleaned")
	due, _ = o.CleanupDue(CleanupEvery, 0)
	require.True(t, due, "the other checkout wasn't")
}
