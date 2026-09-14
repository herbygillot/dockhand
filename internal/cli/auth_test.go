package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/stretchr/testify/require"
)

func TestAuthLoginHelpAndMissingClientIDDoNotOpenState(t *testing.T) {
	t.Setenv("DOCKHAND_GITHUB_CLIENT_ID", "")
	database := filepath.Join(t.TempDir(), "state.db")
	var output bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), []string{"auth", "login", "--help"}, cli.Streams{Out: &output, Err: &output}, app.Config{DBPath: database, Repository: "/missing/repository"}))
	require.Contains(t, output.String(), "--client-id")
	require.Contains(t, output.String(), "--no-browser")
	require.NotContains(t, output.String(), "State database:")
	require.NoFileExists(t, database)
	output.Reset()
	err := cli.Run(t.Context(), []string{"auth", "login", "--no-browser"}, cli.Streams{Out: &output, Err: &output}, app.Config{DBPath: database, Repository: "/missing/repository"})
	require.ErrorContains(t, err, "client ID")
	require.NoFileExists(t, database)
}
