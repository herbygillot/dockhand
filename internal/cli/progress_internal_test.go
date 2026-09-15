package cli

import (
	"bytes"
	"encoding/json"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCommandProgressStaysOnStderrAndEscapesControlCharacters(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state.db")
	root, err := NewRoot(app.Config{DBPath: db})
	require.NoError(t, err)
	root.AddCommand(&cobra.Command{Use: "fixture", RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := progress.WithScope(cmd.Context(), "attempt_one")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(progress.WithScope(ctx, "attempt_two"), "packing source")
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]bool{"done": true})
	}})
	var out, diagnostics bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&diagnostics)
	root.SetArgs([]string{"--json", "fixture"})
	require.NoError(t, root.ExecuteContext(t.Context()))
	require.JSONEq(t, `{"done":true}`, out.String())
	require.Equal(t, "attempt_one: staging\\n\\x1b[31m\nattempt_two: packing source\n", diagnostics.String())
	require.NoFileExists(t, db)
}

func TestCohortProgressCountsQueuedTargets(t *testing.T) {
	var out bytes.Buffer
	reporter := newReporter(&out, nil, false)
	status := workflow.Status{Jobs: []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}, Attempts: []record.Attempt{{State: record.AttemptQueued}, {State: record.AttemptRunning}, {State: record.AttemptQueued}}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, 1, strings.Count(out.String(), "waiting for provider admission"))
	require.Contains(t, out.String(), "2 targets waiting")
}
