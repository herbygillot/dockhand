package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGitHubBuildFlagsSelectWorkflowPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flags []string
		valid bool
	}{
		{[]string{"--provider", "github"}, true},
		{[]string{"--provider", "github", "--tests", "workflow"}, true},
		{[]string{"--provider", "github", "--tests", "declared"}, false},
		{[]string{"--provider", "github", "--tests", "skip"}, false},
		{[]string{"--provider", "github", "--image", "image"}, false},
		{[]string{"--provider", "github", "--capacity", "2"}, false},
		{[]string{"--provider", "github", "--from-source=false"}, false},
		{[]string{"--provider", "tart", "--tests", "workflow"}, false},
		{[]string{"--provider", "unknown"}, false},
	} {
		t.Run(tc.flags[len(tc.flags)-1], func(t *testing.T) {
			cmd := &cobra.Command{Use: "fixture"}
			var options buildOptions
			options.flags(cmd, app.Config{})
			cmd.Flags().String("remote", "personal", "")
			require.NoError(t, cmd.ParseFlags(tc.flags))
			config, err := options.config(cmd, app.Config{})
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "github", config.VerificationProvider)
			require.Equal(t, string(record.TestWorkflow), options.tests)
			require.Equal(t, "personal", config.VerificationDestination.Remote)
		})
	}
}

func TestGitHubRequiresCommittedBranchBeforeOpeningState(t *testing.T) {
	t.Parallel()
	db := filepath.Join(t.TempDir(), "missing", "state.db")
	var output bytes.Buffer
	err := Run(t.Context(), []string{"verify", "fixture", "--working-tree", "--provider", "github"}, Streams{Out: &output, Err: &output}, app.Config{DBPath: db})
	require.ErrorContains(t, err, "requires committed source")
	require.NoFileExists(t, db)
}
