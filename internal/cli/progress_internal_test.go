package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCommandProgressStaysOnStderrAndEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	db := filepath.Join(t.TempDir(), "state.db")
	root, err := NewRoot(app.Config{DBPath: db})
	require.NoError(t, err)
	root.AddCommand(&cobra.Command{Use: "fixture", RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := progress.WithScope(cmd.Context(), "attempt_one")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(ctx, "staging\n\x1b[31m")
		progress.Report(progress.WithScope(ctx, "attempt_two"), "packing source")
		return nil
	}})
	var out, diagnostics bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&diagnostics)
	root.SetArgs([]string{"--json", "fixture"})
	require.NoError(t, root.ExecuteContext(t.Context()))
	require.Empty(t, out.String(), "a fixture command without a result writes nothing; run writes the envelope")
	require.Equal(t, "{\"level\":\"info\",\"scope\":\"attempt_one\",\"message\":\"staging\\n\\u001b[31m\"}\n{\"level\":\"info\",\"scope\":\"attempt_two\",\"message\":\"packing source\"}\n", diagnostics.String(), "JSON mode reports one object per line on stderr")
	require.NoFileExists(t, db)
}

func TestCohortProgressCountsQueuedTargets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	reporter := newReporter(&out, nil, false, progress.Info, false)
	status := workflow.Status{Jobs: []view.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}, Attempts: []record.Attempt{{State: record.AttemptQueued}, {State: record.AttemptRunning}, {State: record.AttemptQueued}}}}}
	require.NoError(t, reporter.status(t.Context(), status))
	require.Equal(t, 1, strings.Count(out.String(), "waiting for provider admission"))
	require.Contains(t, out.String(), "2 targets waiting")
}
