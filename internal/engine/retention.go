package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/model"
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

// LogCleanupWords says what cleanup did to checks' logs: "compressed 242
// logs, 802 MB to 50 MB; removed 12 checks' logs, 140 MB, kept for
// nothing past 15 days".
func LogCleanupWords(done LogCleanup, after time.Duration) string {
	var words []string
	if c := done.Compressed; c.Logs > 0 {
		words = append(words, fmt.Sprintf("compressed %s, %s to %s", plural(c.Logs, "log"), byteWords(c.Before), byteWords(c.After)))
	}
	if len(done.Removed) > 0 {
		whose := plural(len(done.Removed), "check") + "'"
		if len(done.Removed) == 1 {
			whose = "1 check's"
		}
		words = append(words, fmt.Sprintf("removed %s logs, %s, kept for nothing past %s", whose, byteWords(done.Bytes), AgeWords(after)))
	}
	return strings.Join(words, "; ")
}

// HistoryWords says what cleanup removed of build history.
func HistoryWords(pruned store.Pruned, assessments int, after time.Duration) string {
	var words []string
	if pruned != (store.Pruned{}) {
		words = append(words, fmt.Sprintf("removed what branches ended past %s recorded of %s: %s and %s, %s and %s, keeping what reuse may choose and each one's newest check",
			AgeWords(after), plural(pruned.Runs, "check"), plural(pruned.Results, "result"), plural(pruned.Executions, "provider run"),
			plural(pruned.Plans, "plan"), plural(pruned.Revisions, "revision")))
	}
	if assessments > 0 {
		words = append(words, fmt.Sprintf("removed %s of trees open branches moved past", plural(assessments, "assessment")))
	}
	return strings.Join(words, "; ")
}

// byteWords words a size: 820 MB, 1.4 GB, 12 KB.
func byteWords(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", math.Ceil(float64(n)/(1<<20)))
	}
	return fmt.Sprintf("%.0f KB", math.Ceil(float64(n)/(1<<10)))
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
