package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// GitHub exposes completed job logs. Cache one pinned attempt's log locally so
// trace offsets remain stable across reruns and subsequent reads.
func (p *Provider) ReadLog(ctx context.Context, handle record.ProviderRun, offset int64, limit int) (verify.LogChunk, error) {
	result := verify.LogChunk{Next: offset}
	if offset < 0 || limit <= 0 {
		return result, fmt.Errorf("github verification: invalid log range")
	}
	err := p.locked(ctx, handle.RequestID, func(ctx context.Context) error {
		saved, selected, api, err := p.execution(ctx, handle)
		if err != nil {
			return err
		}
		name := filepath.Join(p.Directory, digest([]byte(handle.RequestID))+".log")
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			run, err := api.Run(ctx, selected.ID, selected.Attempt)
			if err != nil {
				return err
			}
			if !matches(saved, run) || run.GetRunAttempt() != selected.Attempt {
				return fmt.Errorf("github verification: log run identity changed")
			}
			if run.GetStatus() != "completed" {
				return nil
			}
			jobs, err := api.Jobs(ctx, selected.ID, selected.Attempt)
			if err != nil {
				return err
			}
			if len(jobs) == 0 {
				return nil
			}
			sort.Slice(jobs, func(i, j int) bool { return jobs[i].GetID() < jobs[j].GetID() })
			file, err := os.CreateTemp(p.Directory, ".github-log-*")
			if err != nil {
				return err
			}
			defer os.Remove(file.Name())
			defer file.Close()
			for _, job := range jobs {
				if job.GetRunID() != selected.ID || job.GetRunAttempt() != int64(selected.Attempt) || job.GetHeadSHA() != string(saved.Request.Spec.Source.Commit) {
					return fmt.Errorf("github verification: log job identity changed")
				}
				if job.GetStatus() != "completed" {
					return nil
				}
				if _, err := fmt.Fprintf(file, "\n--- %s (%s) ---\n%s\n", job.GetName(), job.GetConclusion(), job.GetHTMLURL()); err != nil {
					return err
				}
				if job.GetConclusion() == "skipped" {
					continue
				}
				body, err := api.JobLog(ctx, job.GetID())
				if err != nil {
					return err
				}
				_, copyErr := io.Copy(file, body)
				closeErr := body.Close()
				if err := errors.Join(copyErr, closeErr); err != nil {
					return err
				}
			}
			if err := file.Close(); err != nil {
				return err
			}
			if err := os.Rename(file.Name(), name); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		data := make([]byte, limit)
		count, err := file.ReadAt(data, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		result.Data, result.Next, result.Complete = data[:count], offset+int64(count), errors.Is(err, io.EOF)
		return nil
	})
	return result, err
}
