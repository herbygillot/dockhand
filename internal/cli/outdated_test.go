package cli

import (
	"bytes"
	"encoding/json"
	"github.com/herbygillot/dockhand/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/outdated"
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
			var result outdated.Result
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
	var result outdated.Result
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Len(t, result.Ports, 2)
	require.Equal(t, upstream.Unknown, result.Ports[0].Assessment)
	require.NotEmpty(t, result.Ports[0].Detail)
	require.Equal(t, upstream.UpdateAvailable, result.Ports[1].Assessment)
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func TestOutdatedSelectionValidation(t *testing.T) {
	for _, args := range [][]string{{"outdated"}, {"outdated", "fixture", "--maintainer", "@owner"}, {"outdated", "--category", ""}, {"outdated", "--maintainer", "*"}} {
		config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git rev-parse")
		require.NoDirExists(t, filepath.Dir(config.DBPath))
	}
}

func TestOutdatedIndexedSelectionKeepsSourceAndCoverage(t *testing.T) {
	if _, err := exec.LookPath("portindex"); err != nil {
		t.Skip("MacPorts portindex required")
	}
	t.Setenv("HOME", t.TempDir())
	config, repo, downloads, _ := automaticCLI(t, "1.0")
	head, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	before, data, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	data = append(data, []byte("maintainers {example.org:owner @contributor} openmaintainer\nsubport fixture-extra {}\n")...)
	tree, err = repo.EditTree(t.Context(), tree, []git.FileEdit{
		{Path: "devel/fixture/Portfile", Before: before, After: data, Mode: before.Mode},
		{Path: "devel/broken/Portfile", After: []byte("PortSystem 1.0\nerror broken\n"), Mode: 0100644},
	})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{head}, Message: "selection fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: head}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	refs, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	dirty := filepath.Join(repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(dirty), 0700))
	require.NoError(t, os.WriteFile(dirty, []byte("invalid dirty checkout"), 0600))
	for _, args := range [][]string{
		{"outdated", "--maintainer", "contributor@github", "--category", "devel", "--json"},
		{"outdated", "--maintainer", "owner@example.org", "--json"},
		{"outdated", "--category", "devel", "--category", "devel", "--json"},
	} {
		var out, stderr bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &stderr}, config)
		require.ErrorContains(t, err, "unknown", stderr.String())
		var result outdated.Result
		require.NoError(t, json.Unmarshal(out.Bytes(), &result), out.String())
		require.Equal(t, commit, string(result.Source.Commit))
		require.Len(t, result.Ports, 3)
		require.Equal(t, "devel/broken/Portfile", result.Ports[0].Selector)
		require.Equal(t, upstream.Unknown, result.Ports[0].Assessment)
		require.Equal(t, "fixture", result.Ports[1].Selector)
		require.Equal(t, upstream.UpdateAvailable, result.Ports[1].Assessment)
		require.Equal(t, "fixture-extra", result.Ports[2].Selector)
		require.Equal(t, upstream.Unknown, result.Ports[2].Assessment)
		require.Contains(t, result.Ports[2].Detail, "primary port")
	}
	require.Zero(t, downloads.Load())
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	after, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	require.Equal(t, refs, after)
	dirtyData, err := os.ReadFile(dirty)
	require.NoError(t, err)
	require.Equal(t, "invalid dirty checkout", string(dirtyData))
}

func TestOutdatedEmptySelectionAndDuplicatePorts(t *testing.T) {
	if _, err := exec.LookPath("portindex"); err != nil {
		t.Skip("MacPorts portindex required")
	}
	t.Setenv("HOME", t.TempDir())
	config, _, downloads, catalogs := automaticCLI(t, "1.0")
	var out, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"outdated", "--category", "not-a-category"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Contains(t, out.String(), "No ports matched")
	require.Zero(t, catalogs.Load())
	out.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"outdated", "fixture", "fixture", "--json"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	var result outdated.Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Ports, 1)
	require.EqualValues(t, 1, catalogs.Load())
	require.Zero(t, downloads.Load())
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}
