package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestStatusSelectorsCLIReadRecordedBranchWithoutExecution(t *testing.T) {
	config, id := queuedJob(t)
	// The recorded association remains queryable after its Git ref is removed.
	cmd := exec.CommandContext(t.Context(), "git", "update-ref", "-d", "refs/heads/candidate")
	cmd.Dir = config.Repository
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, args := range [][]string{
		{"status", string(id)}, {"status", "--active"}, {"status", "--branch", "candidate"},
		{"status", "--branch", "candidate", "--active"}, {"status", string(id), "--active"},
	} {
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(t.Context(), append(args, "--json"), Streams{Out: &stdout, Err: &stderr}, config))
		var result workflow.Status
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.Len(t, result.Jobs, 1)
		require.Equal(t, id, result.Jobs[0].Job.ID)
		require.Equal(t, record.JobQueued, result.Jobs[0].Job.State)
		require.Equal(t, record.PhaseVerification, result.Jobs[0].Job.Phase)
		require.Empty(t, result.Jobs[0].Attempts)
		require.Len(t, result.Changes, 1)
		require.Empty(t, stderr.String())
	}
	var outbuf bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"status", string(id)}, Streams{Out: &outbuf, Err: &outbuf}, config))
	require.Contains(t, outbuf.String(), "phase: verification")
	outbuf.Reset()
	require.NoError(t, Run(t.Context(), []string{"status", "--branch", "missing", "--active"}, Streams{Out: &outbuf, Err: &outbuf}, config))
	require.Contains(t, outbuf.String(), "Contribution branch: missing")
	require.Contains(t, outbuf.String(), "Showing queued and active jobs.")
	require.Contains(t, outbuf.String(), "No matching jobs.")
	require.NotContains(t, outbuf.String(), string(id))
	require.ErrorIs(t, Run(t.Context(), []string{"status", "unknown"}, Streams{Out: &outbuf, Err: &outbuf}, config), state.ErrNotFound)
}

func TestStatusRejectsInvalidSelectorsBeforeOpeningRepository(t *testing.T) {
	config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
	for _, args := range [][]string{
		{"status", "job", "--branch", "candidate"}, {"status", "job", "second"},
		{"status", ""}, {"status", "bad\njob"}, {"status", "--branch="}, {"status", "--branch", "bad..branch"},
	} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git ")
	}
	_, err := os.Stat(filepath.Dir(config.DBPath))
	require.ErrorIs(t, err, os.ErrNotExist)
}
