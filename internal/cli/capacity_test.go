package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/stretchr/testify/require"
)

func TestSetupCapacityMustBePositiveAndRunsRefuseIt(t *testing.T) {
	t.Parallel()
	db := filepath.Join(t.TempDir(), "missing", "state.db")
	var out bytes.Buffer
	err := Run(t.Context(), []string{"setup", "--capacity", "0"}, Streams{Out: &out, Err: &out}, app.Config{DBPath: db})
	require.ErrorContains(t, err, "capacity must be positive")
	require.NoDirExists(t, filepath.Dir(db))
	for _, command := range []string{"bump", "verify", "amend"} {
		err := Run(t.Context(), []string{command, "jq", "--capacity", "2"}, Streams{Out: &out, Err: &out}, app.Config{DBPath: db, Repository: "/does-not-exist"})
		require.ErrorContains(t, err, "unknown flag: --capacity", command)
	}
}
