package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/stretchr/testify/require"
)

func TestDependentOptionsRejectIncompatibleWorkBeforeState(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"bump", "jq", "--dependents", "--provider", "github"},
		{"verify", "jq", "--target-image", "child=image"},
		{"verify", "jq", "--dependents", "--target-image", "child"},
		{"verify", "jq", "--dependents", "--target-image", "child="},
		{"verify", "jq", "--dependents", "--target-image", "child=a", "--target-image", "child=b"},
		{"verify", "jq", "--dependents", "--provider", "github", "--branch", "candidate"},
		{"bump-revision", "jq", "--dependents", "--skip-verify"},
		{"refresh-checksums", "jq", "--dependents", "--diff"},
	} {
		config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git rev-parse")
		require.NoFileExists(t, config.DBPath)
	}
}
