package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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

// Cleanup prunes the journal, which only grew. Events older than its age
// go, and so do the sessions that ended, or went quiet, before it, but
// for one a lease still names. What's newer stays.
func TestCleanupPrunesTheJournal(t *testing.T) {
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	f := setup(t)
	e := f.open(t)
	old := e.now().Add(-40 * 24 * time.Hour)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for _, s := range []model.Session{
			{ID: "ses_ended", Repository: e.Repository, Kind: model.SessionForeground, PID: 1, ProcessStart: "a", Version: "v", StartedAt: old, HeartbeatAt: old, EndedAt: &old},
			{ID: "ses_quiet", Repository: e.Repository, Kind: model.SessionObserver, PID: 2, ProcessStart: "b", Version: "v", StartedAt: old, HeartbeatAt: old},
			{ID: "ses_holding", Repository: e.Repository, Kind: model.SessionServe, PID: 3, ProcessStart: "c", Version: "v", StartedAt: old, HeartbeatAt: old},
		} {
			if err := tx.AddSession(s); err != nil {
				return err
			}
		}
		if _, err := tx.AcquireLease("run:run_old", "ses_holding"); err != nil {
			return err
		}
		for _, event := range []model.Event{{At: old, Kind: "run.state", Level: model.LevelInfo, Message: "long ago"}, {At: e.now(), Kind: "run.state", Level: model.LevelInfo, Message: "today"}} {
			if _, err := tx.AppendEvent(event); err != nil {
				return err
			}
		}
		return nil
	}))

	report, err := e.Cleanup(t.Context(), session(t, e), 30*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, report.Events)
	require.Equal(t, 2, report.Sessions, "the ended one and the quiet one")
	events, err := e.Events(t.Context(), 0)
	require.NoError(t, err)
	var messages []string
	for _, event := range events {
		messages = append(messages, event.Message)
	}
	require.Contains(t, messages, "today")
	require.NotContains(t, messages, "long ago")
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		_, err := r.Session("ses_holding")
		require.NoError(t, err, "a lease still names it")
		for _, gone := range []model.SessionID{"ses_ended", "ses_quiet"} {
			_, err := r.Session(gone)
			require.ErrorIs(t, err, store.ErrNotFound, gone)
		}
		return nil
	}))
}

// Cleanup keeps today's events, however short cleanup.after is: serve's
// daily limit counts the pull requests it opened since midnight from
// them, and an hour's after pruned the morning's, so serve opened more
// than its limit. Yesterday's go as after says.
func TestCleanupKeepsTheEventsTodaysSubmitLimitCounts(t *testing.T) {
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	f := setup(t)
	e := f.open(t)
	now := e.now()
	morning := dayStart(now).Add(time.Minute)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for _, event := range []model.Event{
			{At: dayStart(now).Add(-time.Minute), Kind: ServeSubmitKind, Level: model.LevelInfo, Message: "yesterday's"},
			{At: morning, Kind: ServeSubmitKind, Level: model.LevelInfo, Message: "this morning's"},
		} {
			if _, err := tx.AppendEvent(event); err != nil {
				return err
			}
		}
		return nil
	}))
	require.Greater(t, now.Sub(morning), time.Hour, "the morning's is older than after")

	report, err := e.Cleanup(t.Context(), session(t, e), time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, report.Events, "yesterday's")
	opened, err := e.servedToday(t.Context(), now)
	require.NoError(t, err)
	require.Equal(t, 1, opened, "this morning's still counts")
}

// Cleanup removes the kept archives no live result names, with their
// files, and what no record names once it is older than its age: a file
// whose record never followed, a fetch that didn't finish. An open
// branch's newest results' archives stay (decisions 36 and 44, D6).
func TestCleanupRemovesArchivesNoLiveResultNames(t *testing.T) {
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{active: []model.ActivePort{}}}
	run, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm).ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)

	old := e.now().Add(-40 * 24 * time.Hour)
	directory := e.ArchiveDirectory()
	require.NoError(t, os.MkdirAll(filepath.Join(directory, ".incoming-1"), 0o755))
	for _, name := range []string{"libharbor", "gone", "orphan", "fresh"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(name), 0o644))
	}
	// gone's file is new, though its record is old: it goes because its
	// record does, not by its age.
	for _, name := range []string{"libharbor", "orphan", ".incoming-1"} {
		require.NoError(t, os.Chtimes(filepath.Join(directory, name), old, old))
	}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		// The scripted build's results name sha256:<target>.
		for _, archive := range []model.Archive{{Digest: "sha256:libharbor", Name: "libharbor.tbz2", Size: 9, KeptAt: old}, {Digest: "sha256:gone", Name: "gone.tbz2", Size: 4, KeptAt: old}} {
			if err := tx.KeepArchive(archive); err != nil {
				return err
			}
		}
		return nil
	}))

	report, err := e.Cleanup(t.Context(), session(t, e), 30*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, report.Archives, 1)
	require.Equal(t, "sha256:gone", report.Archives[0].Digest)
	require.FileExists(t, filepath.Join(directory, "libharbor"), "an open branch's result names it")
	require.FileExists(t, filepath.Join(directory, "fresh"), "no record names it yet, but it's new")
	for _, name := range []string{"gone", "orphan", ".incoming-1"} {
		require.NoFileExists(t, filepath.Join(directory, name))
		require.NoDirExists(t, filepath.Join(directory, name))
	}
	events, err := e.Events(t.Context(), 0)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(events, func(event model.Event) bool {
		return event.Message == "removed 1 kept archive, 1 MB, that neither reuse nor an open branch's newest results name; 1 archive kept, 1 MB"
	}), "cleanup says what it removed and what the store keeps")
}
