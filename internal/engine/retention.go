package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// keptNewest is how many of an open branch's newest checks keep their
// logs whatever else they're kept for (D6).
const keptNewest = 3

// supersededAssessments is how long an open branch's assessments of a tree
// it moved past are kept, from when it did (D6).
const supersededAssessments = 7 * 24 * time.Hour

// archiveGrace is how long a kept archive stays whatever names it: a
// build that has just kept one may not have finished recording it.
const archiveGrace = time.Hour

// historyChunk is how many ended branches' history one transaction prunes.
// On the SQL study's year-scale database (2026-10-13) a first-time backlog
// of 200 ended branches took 17 s on an M-series Mac in chunks of ten,
// each 0.7 to 1.0 s, and 3 s a chunk at fifty: a chunk stays near a
// second, short of the 5 s a check's own writes wait for the lock.
const historyChunk = 10

// LogCleanupWords says what cleanup did to checks' logs: "compressed 242
// logs, 802 MB to 50 MB; removed 12 checks' logs, 140 MB, kept for
// nothing past 15 days".
func LogCleanupWords(done LogCleanup, after time.Duration) string {
	var words []string
	if c := done.Compressed; c.Logs > 0 {
		words = append(words, fmt.Sprintf("compressed %s, %s to %s", prose.Plural(c.Logs, "log"), prose.Bytes(c.Before), prose.Bytes(c.After)))
	}
	if len(done.Removed) > 0 {
		whose := prose.Plural(len(done.Removed), "check") + "'"
		if len(done.Removed) == 1 {
			whose = "1 check's"
		}
		words = append(words, fmt.Sprintf("removed %s logs, %s, kept for nothing past %s", whose, prose.Bytes(done.Bytes), AgeWords(after)))
	}
	return strings.Join(words, "; ")
}

// HistoryWords says what cleanup removed of build history.
func HistoryWords(pruned store.Pruned, assessments int, after time.Duration) string {
	var words []string
	// What was removed is counted, and nothing else: "recorded of 0
	// checks: 1 result" read as a contradiction where a result outlived
	// its check's removal (the v0.3.1-rc1 quick stage).
	var removed []string
	for _, count := range []struct {
		n    int
		noun string
	}{{pruned.Runs, "check"}, {pruned.Results, "result"}, {pruned.Executions, "provider run"}, {pruned.Plans, "plan"}, {pruned.Revisions, "revision"}} {
		if count.n > 0 {
			removed = append(removed, prose.Plural(count.n, count.noun))
		}
	}
	if len(removed) > 0 {
		words = append(words, fmt.Sprintf("removed %s that branches ended past %s had recorded, keeping what reuse may choose and each branch's newest check",
			prose.And(removed), AgeWords(after)))
	}
	if assessments > 0 {
		words = append(words, fmt.Sprintf("removed %s of trees open branches moved past, assessments and change records", prose.Plural(assessments, "record")))
	}
	return strings.Join(words, "; ")
}

// LogCleanup is what cleanup did to checks' logs: what it compressed, and
// the logs it removed, each a check's directory, with their bytes.
type LogCleanup struct {
	Compressed buildlog.Compression
	Removed    []string
	Bytes      int64
}

// cleanLogs keeps every finished check's logs compressed, and removes those
// cleanup no longer keeps (D6): an ended branch's, once it ended before a
// time, and an open branch's where they stand for nothing, superseded
// before that time. An open branch keeps the logs of its newest check in
// each environment it has built in, which its evidence reads, and of its
// keptNewest newest checks. A check still going is left as it is.
func (e *Engine) cleanLogs(ctx context.Context, before time.Time) (LogCleanup, error) {
	var done LogCleanup
	type branchRuns struct {
		branch model.Branch
		runs   []model.Run
		// environments are each run's environments, by run.
		environments map[model.RunID][]model.Environment
	}
	var all []branchRuns
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		branches, err := r.Branches(store.BranchFilter{})
		if err != nil {
			return err
		}
		for _, branch := range branches {
			runs, err := r.Runs(store.RunFilter{Branch: branch.ID})
			if err != nil {
				return err
			}
			found := branchRuns{branch: branch, runs: runs, environments: map[model.RunID][]model.Environment{}}
			for _, run := range runs {
				executions, err := r.Executions(run.ID)
				if err != nil {
					return err
				}
				for _, execution := range executions {
					found.environments[run.ID] = append(found.environments[run.ID], execution.Environment)
				}
			}
			all = append(all, found)
		}
		return nil
	}); err != nil {
		return done, err
	}
	var errs []error
	for _, found := range all {
		ended := found.branch.State != model.BranchOpen && !found.branch.EndedAt.IsZero() && found.branch.EndedAt.Before(before)
		seen := map[model.Environment]bool{}
		for i, run := range found.runs {
			if !run.State.Terminal() {
				continue
			}
			directory := filepath.Join(e.LogDirectory(), run.Name())
			counts := i < keptNewest
			for _, environment := range found.environments[run.ID] {
				if !seen[environment] {
					seen[environment], counts = true, true
				}
			}
			// What superseded it is the check after it, newest first.
			superseded := i > 0 && found.runs[i-1].CreatedAt.Before(before)
			if ended || !counts && superseded && found.branch.State == model.BranchOpen {
				bytes, err := removeDirectory(directory)
				if bytes > 0 {
					done.Removed, done.Bytes = append(done.Removed, run.Name()), done.Bytes+bytes
				}
				errs = append(errs, err)
				continue
			}
			compressed, err := buildlog.CompressAll(directory)
			done.Compressed.Logs += compressed.Logs
			done.Compressed.Before += compressed.Before
			done.Compressed.After += compressed.After
			errs = append(errs, err)
		}
	}
	return done, errors.Join(errs...)
}

// removeDirectory removes a directory and says how many bytes its files
// held; none where it wasn't there.
func removeDirectory(directory string) (int64, error) {
	var bytes int64
	err := filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			bytes += info.Size()
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return bytes, errors.Join(err, os.RemoveAll(directory))
}
