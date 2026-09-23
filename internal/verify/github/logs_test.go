package github

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/require"
)

type interruptedLog struct{}

func (interruptedLog) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }

func TestLogDownloadsResumeCompletedJobs(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	calls := map[int64]int{}
	f.api.jobLog = func(_ context.Context, id int64) (io.ReadCloser, error) {
		calls[id]++
		if id == 101 && calls[id] == 1 {
			return io.NopCloser(io.MultiReader(strings.NewReader("incomplete body"), interruptedLog{})), nil
		}
		body := "first job log\n"
		if id == 101 {
			body = "second job log\n"
		}
		return io.NopCloser(strings.NewReader(body)), nil
	}
	_, err = f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	name := filepath.Join(f.provider.Directory, record.Digest([]byte(f.request.ID))+".log")
	require.NoFileExists(t, name)
	require.FileExists(t, jobLogPath(name, 100))
	require.NoFileExists(t, jobLogPath(name, 101))
	temps, err := filepath.Glob(filepath.Join(f.provider.Directory, ".github-log-*"))
	require.NoError(t, err)
	require.Empty(t, temps)

	// Retry from a new provider and a changed API job order.
	restarted := *f.provider
	f.api.jobs[0], f.api.jobs[1] = f.api.jobs[1], f.api.jobs[0]
	result, err := restarted.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.Equal(t, map[int64]int{100: 1, 101: 2}, calls)
	body := string(result.Data)
	require.Equal(t, 1, strings.Count(body, "first job log"))
	require.Equal(t, 1, strings.Count(body, "second job log"))
	require.Less(t, strings.Index(body, "first job log"), strings.Index(body, "second job log"))
	require.NotContains(t, body, "incomplete body")
	require.EqualValues(t, len(result.Data), result.Next)
	require.FileExists(t, name)
	require.NoFileExists(t, jobLogPath(name, 100))
	require.NoFileExists(t, jobLogPath(name, 101))
}

func TestConcurrentLogReadersShareCompletedDownloads(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]verify.LogChunk, 2)
	errors := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			provider := *f.provider
			results[i], errors[i] = provider.ReadLog(t.Context(), submission.Run, 0, 4096)
		})
	}
	wg.Wait()
	for _, err := range errors {
		require.NoError(t, err)
	}
	require.True(t, results[0].Complete)
	require.Equal(t, results[0], results[1])
	require.Equal(t, 2, f.api.logCalls)
}

func TestLogIdentityMismatchDoesNotDownload(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"run", "job", "duplicate job"} {
		t.Run(mismatch, func(t *testing.T) {
			f := setup(t)
			f.ready()
			submission, err := f.provider.Submit(t.Context(), f.request)
			require.NoError(t, err)
			switch mismatch {
			case "run":
				f.api.run.ID = gh.Ptr(int64(11))
			case "job":
				f.api.jobs[1].RunID = gh.Ptr(int64(11))
			case "duplicate job":
				f.api.jobs[1].ID = f.api.jobs[0].ID
			}
			_, err = f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
			require.ErrorContains(t, err, "identity changed")
			require.Zero(t, f.api.logCalls)
			caches, err := filepath.Glob(filepath.Join(f.provider.Directory, "*.log*"))
			require.NoError(t, err)
			require.Empty(t, caches)
		})
	}
}

func TestCanceledLogWriteDoesNotPublishCache(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.log")
	ctx, cancel := context.WithCancel(t.Context())
	err := writeLogFile(ctx, path, func(f *os.File) error {
		_, err := f.WriteString("complete bytes")
		cancel()
		return err
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoFileExists(t, path)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Empty(t, entries)
}
