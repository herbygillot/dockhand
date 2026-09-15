package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

func TestOutdatedObservesFrozenSourceWithoutStateOrDownloads(t *testing.T) {
	for _, version := range []string{"1.0", "2.0"} {
		t.Run(version, func(t *testing.T) {
			config, repo, downloads, catalogs := automaticCLI(t, version)
			before, err := repo.ReadRefs(t.Context(), "refs/heads/")
			require.NoError(t, err)
			path := filepath.Join(repo.Root, "devel/fixture/Portfile")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte("invalid working tree"), 0600))
			var stdout, stderr bytes.Buffer
			require.NoError(t, Run(t.Context(), []string{"outdated", "fixture", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), stderr.String())
			var result app.OutdatedResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.Len(t, result.Ports, 1)
			require.Equal(t, version, result.Ports[0].CurrentVersion)
			if version == "1.0" {
				require.Equal(t, upstream.UpdateAvailable, result.Ports[0].Assessment)
			} else {
				require.Equal(t, upstream.Current, result.Ports[0].Assessment)
			}
			require.Zero(t, downloads.Load())
			require.EqualValues(t, 1, catalogs.Load())
			require.NoDirExists(t, filepath.Dir(config.DBPath))
			after, err := repo.ReadRefs(t.Context(), "refs/heads/")
			require.NoError(t, err)
			require.Equal(t, before, after)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "invalid working tree", string(data))
		})
	}
}
func TestOutdatedKeepsUnknownAlongsideSuccessfulObservations(t *testing.T) {
	config, _, _, _ := automaticCLI(t, "1.0")
	var stdout, stderr bytes.Buffer
	err := Run(t.Context(), []string{"outdated", "missing", "fixture", "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, "unknown")
	var result app.OutdatedResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Len(t, result.Ports, 2)
	require.Equal(t, upstream.Unknown, result.Ports[0].Assessment)
	require.NotEmpty(t, result.Ports[0].Detail)
	require.Equal(t, upstream.UpdateAvailable, result.Ports[1].Assessment)
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}
