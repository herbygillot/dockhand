package github

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
)

// ReadLog serves completed caches without credentials. Missing caches are built
// from individually committed job downloads so a failed read can resume later.
func (p *Provider) ReadLog(ctx context.Context, handle record.ProviderRun, offset int64, limit int) (verify.LogChunk, error) {
	result := verify.LogChunk{Next: offset}
	if offset < 0 || limit <= 0 {
		return result, fmt.Errorf("github verification: invalid log range")
	}
	err := p.locked(ctx, handle.RequestID, func(ctx context.Context, _ *ledger.Entry) error {
		saved, selected, row, err := p.execution(ctx, handle)
		if err != nil {
			return err
		}
		if row.State == record.ExecutionReleased {
			result.Complete = true
			return nil
		}
		name := filepath.Join(p.Directory, record.Digest([]byte(handle.RequestID))+".log")
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			ready, err := p.cacheLogs(ctx, saved, selected, name)
			if err != nil || !ready {
				return err
			}
		} else if err != nil {
			return err
		}
		result, err = readLogChunk(name, offset, limit)
		if err == nil {
			now := time.Now()
			err = os.Chtimes(name, now, now)
		}
		return err
	})
	return result, err
}

func (p *Provider) cacheLogs(ctx context.Context, saved payload, selected executionRun, name string) (bool, error) {
	api, err := p.actions(ctx, saved.Config.Destination.HeadRepository)
	if err != nil {
		return false, err
	}
	run, err := api.Run(ctx, selected.ID, selected.Attempt)
	if err != nil {
		return false, err
	}
	if !matches(saved, run) || run.GetID() != selected.ID || run.GetRunAttempt() != selected.Attempt {
		return false, fmt.Errorf("github verification: log run identity changed")
	}
	if run.GetStatus() != "completed" {
		return false, nil
	}
	jobs, err := api.Jobs(ctx, selected.ID, selected.Attempt)
	if err != nil {
		return false, err
	}
	if len(jobs) == 0 {
		return false, nil
	}
	seen := map[int64]bool{}
	for _, job := range jobs {
		if job.GetID() <= 0 || seen[job.GetID()] || job.GetRunID() != selected.ID || job.GetRunAttempt() != int64(selected.Attempt) || job.GetHeadSHA() != string(saved.Request.Spec.Source.Commit) {
			return false, fmt.Errorf("github verification: log job identity changed")
		}
		seen[job.GetID()] = true
		if job.GetStatus() != "completed" {
			return false, nil
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].GetID() < jobs[j].GetID() })
	paths := make([]string, 0, len(jobs))
	for _, job := range jobs {
		path := jobLogPath(name, job.GetID())
		if err := cacheJobLog(ctx, api, job, path); err != nil {
			return false, err
		}
		paths = append(paths, path)
	}
	if err := writeLogFile(ctx, name, func(file *os.File) error {
		for _, path := range paths {
			part, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, part)
			if err := errors.Join(copyErr, part.Close()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	// The completed aggregate now owns these bytes. Failed cleanup leaves only a
	// redundant cache; it must not invalidate an otherwise usable log.
	for _, path := range paths {
		_ = os.Remove(path)
	}
	return true, nil
}

func jobLogPath(aggregate string, id int64) string { return fmt.Sprintf("%s.job-%d", aggregate, id) }

func cacheJobLog(ctx context.Context, api actionsAPI, job *gh.WorkflowJob, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeLogFile(ctx, path, func(file *os.File) error {
		if _, err := fmt.Fprintf(file, "\n--- %s (%s) ---\n%s\n", job.GetName(), job.GetConclusion(), job.GetHTMLURL()); err != nil {
			return err
		}
		if job.GetConclusion() == "skipped" {
			return nil
		}
		body, err := api.JobLog(ctx, job.GetID())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, body)
		return errors.Join(copyErr, body.Close())
	})
}

func writeLogFile(ctx context.Context, path string, write func(*os.File) error) error {
	return atomicfile.Create(path, 0600, func(file *os.File) error {
		if err := write(file); err != nil {
			return err
		}
		return ctx.Err()
	})
}

func readLogChunk(path string, offset int64, limit int) (verify.LogChunk, error) {
	data, next, eof, err := ledger.ReadChunk(path, offset, limit)
	if err != nil {
		return verify.LogChunk{Next: offset}, err
	}
	return verify.LogChunk{Data: data, Next: next, Complete: eof}, nil
}
