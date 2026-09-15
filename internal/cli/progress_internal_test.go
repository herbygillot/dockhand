package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
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
