package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestVerificationArgumentsFailBeforeOpeningState(t *testing.T) {
	for _, args := range [][]string{{"verify", "jq", "--image", "base", "--capacity", "0"}, {"verify", "jq", "--image", "base", "--tests", "perhaps"}, {"verify", "jq", "--image", "base", "--variant", "ssl"}, {"verify", "jq", "--image", "base", "--variant", "+ssl", "--variant=-ssl"}} {
		var out bytes.Buffer
		db := filepath.Join(t.TempDir(), "missing", "state.db")
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, app.Config{DBPath: db, Repository: "/does-not-exist"})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git")
		require.NoDirExists(t, filepath.Dir(db))
	}
}
func TestResultCodesDistinguishFailureAttentionAndInterruption(t *testing.T) {
	require.Equal(t, 2, ExitCode(errors.Join(ErrJobFailed, errors.New("write"))))
	require.Equal(t, 3, ExitCode(ErrNeedsAttention))
	require.Equal(t, 130, ExitCode(context.Canceled))
	status := workflow.Status{Jobs: []workflow.JobStatus{{Job: record.Job{State: record.JobCanceled}}}}
	require.ErrorIs(t, outcome(status, false), ErrJobCanceled)
	require.NoError(t, outcome(status, true))
}
func TestJSONResultKeepsLogsAndProgressOffStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := runtime{json: true}
	result := ActionResult{Status: workflow.Status{ReadAt: time.Now(), Jobs: []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}}}}}
	reporter := newReporter(&stderr, nil, false)
	require.NoError(t, reporter.status(t.Context(), result.Status))
	require.NoError(t, reporter.status(t.Context(), result.Status))
	require.NoError(t, r.result(&stdout, result))
	var decoded ActionResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	require.Equal(t, record.JobID("job"), decoded.Status.Jobs[0].Job.ID)
	require.Equal(t, "job: active\n", stderr.String())
}

type logProvider struct {
	verify.Provider
	data []byte
	seen []int64
}

func (p *logProvider) ReadLog(_ context.Context, _ record.ProviderRun, offset int64, limit int) (verify.LogChunk, error) {
	p.seen = append(p.seen, offset)
	n := min(3, len(p.data)-int(offset))
	return verify.LogChunk{Data: p.data[offset : int(offset)+n], Next: offset + int64(n), Complete: int(offset)+n == len(p.data)}, nil
}
func TestTraceResumesOffsetsAndDrainsTerminalLogs(t *testing.T) {
	var output bytes.Buffer
	provider := &logProvider{data: []byte("first\nsecond\n")}
	reporter := newReporter(&output, provider, true)
	run := record.ProviderRun{Provider: "test", RunID: "run"}
	status := workflow.Status{Jobs: []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}, Attempts: []record.Attempt{{Run: run, State: record.AttemptRunning}}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, []int64{0}, provider.seen)
	status.Jobs[0].Attempts[0].State = record.AttemptFinished
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, "job: active\nfirst\nsecond\n", output.String())
	require.Equal(t, []int64{0, 3, 6, 9, 12}, provider.seen)
}

func TestCompletionEmphasizesPassedVerificationAndKeepsReuseDecisionEarlier(t *testing.T) {
	var output bytes.Buffer
	reporter := newReporter(&output, nil, false)
	status := workflow.Status{Jobs: []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive, ReuseDetail: "Previous image differs; running a new build"}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	status.Jobs[0].Job.State = record.JobCompleted
	status.Jobs[0].Attempts = []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, "job: Previous image differs; running a new build\njob: active\njob: completed; verification passed\n", output.String())
	status.Jobs[0].Attempts = nil
	require.Empty(t, completedOutcome(status.Jobs[0]), "completion without a build must not claim verification passed")
	status.Jobs[0].Reused = &record.Attempt{Evidence: &record.Evidence{Verdict: record.VerdictPassed}}
	require.Equal(t, "verification passed (reused)", completedOutcome(status.Jobs[0]))
}
