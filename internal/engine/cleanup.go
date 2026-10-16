package engine

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// CleanupReport is what one automatic cleanup removed.
type CleanupReport struct {
	// Branches are the merged branches it cleaned, and what it kept.
	Branches []CleanBranch
	// Leftovers are what checks left in providers, and what it removed.
	Leftovers []Leftover
	// Indexes are the port index generations it removed.
	Indexes []string
	// Caches are what it removed from providers' caches, such as the
	// vanilla images Tart pulled.
	Caches []string
	// Events and Sessions count what it pruned from the journal.
	Events, Sessions int
	// Archives are the kept archives it removed, which no live result
	// named.
	Archives []model.Archive
	// Logs is what it compressed of checks' logs, and removed (D6).
	Logs LogCleanup
	// History is what it removed of what ended branches recorded of their
	// checks, and Assessments the assessments of trees open branches moved
	// past (D6).
	History     store.Pruned
	Assessments int
}

// Removed counts what it removed.
func (r CleanupReport) Removed() int {
	n := len(r.Indexes) + len(r.Caches) + len(r.Archives) + len(r.Logs.Removed) + r.History.Runs + r.Assessments
	for _, leftover := range r.Leftovers {
		if leftover.Done {
			n++
		}
	}
	for _, branch := range r.Branches {
		for _, step := range branch.Steps {
			if step.Done {
				n++
			}
		}
	}
	return n
}

// Cleanup is decision 36's automatic cleanup, which serve runs at most
// once a day: what clean --merged would remove, less anything it would
// keep, what checks whose process died left in providers, port index
// generations unused for longer than after, and kept archives no live
// result names. Open branches, and work of anyone's own, are never
// touched.
func (e *Engine) Cleanup(ctx context.Context, session *coord.Session, after time.Duration) (CleanupReport, error) {
	var report CleanupReport
	plans, err := e.PlanClean(ctx)
	if err != nil {
		return report, err
	}
	var removable []CleanBranch
	for _, plan := range plans {
		if slices.ContainsFunc(plan.Steps, func(s CleanStep) bool { return s.Kept == "" }) {
			removable = append(removable, plan)
		}
	}
	if report.Branches, err = e.ApplyClean(ctx, removable); err != nil {
		return report, err
	}
	leftovers, err := e.PlanLeftovers(ctx, session)
	if err != nil {
		return report, err
	}
	if report.Leftovers, err = e.RemoveLeftovers(ctx, session, leftovers); err != nil {
		return report, err
	}
	cache, err := e.indexCache()
	if err != nil {
		return report, err
	}
	removed, err := portindex.Collect(ctx, cache, e.now().Add(-after), false)
	for _, item := range removed {
		if item.Completed {
			report.Indexes = append(report.Indexes, item.Path)
		}
	}
	if err != nil {
		return report, fmt.Errorf("cleaning the port index cache: %w", err)
	}
	if len(report.Indexes) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s unused for %s", prose.Plural(len(report.Indexes), "port index generation"), after)})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// What providers keep to make environments from goes once unused for
	// CacheUnused, as the vanilla images Tart pulled do.
	for _, name := range slices.Sorted(maps.Keys(e.Providers)) {
		provider, ok := e.Providers[name].(buildenv.CacheProvider)
		if !ok {
			continue
		}
		removed, err := provider.PruneCache(ctx, CacheUnused)
		report.Caches = append(report.Caches, removed...)
		if err != nil {
			return report, fmt.Errorf("cleaning %s's cache: %w", name, err)
		}
	}
	if len(report.Caches) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s unused for %s: %s", prose.Plural(len(report.Caches), "cached image"), CacheUnused, strings.Join(report.Caches, ", "))})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// Checks' logs are kept compressed, and go where they stand for
	// nothing after after (D6).
	if report.Logs, err = e.cleanLogs(ctx, e.now().Add(-after)); err != nil {
		return report, fmt.Errorf("cleaning checks' logs: %w", err)
	}
	if logs := report.Logs; len(logs.Removed) > 0 || logs.Compressed.Logs > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo, Message: LogCleanupWords(logs, after)})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// What a branch ended past after recorded of its checks goes, but for
	// what reuse may still choose and its newest check, which status
	// shows; and an open branch's assessments of a tree it moved past
	// (D6).
	//
	// History goes a chunk of branches to a transaction, so a large
	// backlog never holds the writer lock past the busy timeout a check's
	// own writes wait for (the SQL study's SQL-S2).
	for from := model.BranchID(""); ; {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			pruned, next, err := tx.PruneHistory(e.now().Add(-after), from, historyChunk)
			if err != nil {
				return err
			}
			report.History.Add(pruned)
			from = next
			return nil
		})
		if err != nil {
			return report, fmt.Errorf("cleaning build history: %w", err)
		}
		if from == "" {
			break
		}
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		if report.Assessments, err = tx.PruneAssessments(e.now().Add(-supersededAssessments)); err != nil {
			return err
		}
		if report.History == (store.Pruned{}) && report.Assessments == 0 {
			return nil
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo, Message: HistoryWords(report.History, report.Assessments, after)})
		return err
	})
	if err != nil {
		return report, fmt.Errorf("cleaning build history: %w", err)
	}
	// Kept archives go once no live result names them: the newest passed
	// build of each target in each environment that reuse may choose, and
	// an open branch's newest passed result of each, which its evidence
	// may name (decisions 36 and 44, D6). One kept within archiveGrace
	// stays, as a build's that has just kept it. What stays is said.
	removedArchives, keptArchives, err := e.pruneArchives(ctx, e.now().Add(-archiveGrace), e.now().Add(-after))
	report.Archives = removedArchives
	if err != nil {
		return report, fmt.Errorf("cleaning kept archives: %w", err)
	}
	if len(removedArchives) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s, %s, that neither reuse nor an open branch's newest results name; %s kept, %s", prose.Plural(len(removedArchives), "kept archive"), archiveBytes(removedArchives),
					prose.Plural(len(keptArchives), "archive"), archiveBytes(keptArchives))})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// The journal keeps what happened within after, as the index cache
	// does, and the sessions that ended or went quiet before it go with
	// their events; one a lease names stays (design v3 §11). It keeps
	// today's whatever after says, since serve.submit_limit counts the
	// pull requests opened since midnight from them (servedToday): an
	// after shorter than the day so far pruned them, and serve opened
	// more than its limit (the limits sweep, 2026-10-01).
	before := e.now().Add(-after)
	if today := dayStart(e.now()); before.After(today) {
		before = today
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		report.Events, report.Sessions, err = tx.PruneJournal(before)
		if err != nil || report.Events+report.Sessions == 0 {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelVerbose,
			Message: fmt.Sprintf("pruned %s and %s older than %s from the journal", prose.Plural(report.Events, "event"), prose.Plural(report.Sessions, "session"), after)})
		return err
	})
	return report, err
}

// CacheUnused is how long something a provider keeps to make environments
// from, such as a vanilla image Tart pulled, goes unused before cleanup
// removes it (decision 36).
const CacheUnused = 30 * 24 * time.Hour

// CleanupEvery is how often automatic cleanup runs, whichever process runs
// it: serve, or one a command starts once its own work is done (decision
// 36).
const CleanupEvery = 24 * time.Hour

// cleanupStamp is the file whose time is the last automatic cleanup's,
// beside the database.
func (e *Engine) cleanupStamp() string { return e.serveFile("cleanup.stamp") }

// CleanupReason is why automatic cleanup is due: every has passed since
// the last, or, when LowSpace, free space ran short. Words say which.
type CleanupReason struct {
	LowSpace bool
	Words    string
}

// CleanupDue says whether automatic cleanup is due, and why: every since
// the last, or, at most once in LowSpacePause, less than minFree free where
// the database or a provider's cache is; zero minFree watches no space. It
// reads files and the providers alone, never the database, so a command
// asks after closing it.
func (e *Engine) CleanupDue(every time.Duration, minFree uint64) (bool, CleanupReason) {
	since := every
	if info, err := os.Stat(e.cleanupStamp()); err == nil {
		since = e.now().Sub(info.ModTime())
	}
	if since >= every {
		return true, CleanupReason{Words: fmt.Sprintf("%s since the last", every)}
	}
	if minFree == 0 || since < LowSpacePause {
		return false, CleanupReason{}
	}
	places := []string{filepath.Dir(e.LogDirectory())}
	for _, name := range slices.Sorted(maps.Keys(e.Providers)) {
		if provider, ok := e.Providers[name].(buildenv.CacheProvider); ok {
			if storage, err := provider.Storage(); err == nil && storage != "" {
				places = append(places, storage)
			}
		}
	}
	for _, place := range places {
		if free, ok := freeSpace(place); ok && free < minFree {
			return true, CleanupReason{LowSpace: true, Words: fmt.Sprintf("only %s free where %s is, under %s", prose.Bytes(int64(free)), place, prose.Bytes(int64(minFree)))}
		}
	}
	return false, CleanupReason{}
}

// LowSpacePause is how long cleanup waits after a pass before free space
// that is still short runs it again.
const LowSpacePause = time.Hour

// StampCleanup marks automatic cleanup as run now. It is stamped before it
// runs, so one that fails isn't tried again at once.
func (e *Engine) StampCleanup() error {
	return e.stampServeFile("cleanup.stamp", e.now())
}
