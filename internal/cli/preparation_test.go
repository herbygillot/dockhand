package cli

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/stretchr/testify/require"
)

func TestBumpParsesOptionalVersionWithoutInitializingState(t *testing.T) {
	config := app.Config{DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), Repository: "/missing/repository"}
	for _, args := range [][]string{{"bump", "jq"}, {"bump", "jq", "v1.8.1"}, {"bump", "jq", "1.8.1"}} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.ErrorIs(t, err, ErrNotImplemented)
	}
	for _, args := range [][]string{{"bump"}, {"bump", "jq", "1", "2"}, {"bump-revision", "jq", "1"}, {"bump", "jq", "1", "--diff", "--wait"}, {"bump-revision", "jq", "--diff", "--branch="}, {"bump-revision", "jq", "--diff", "--variant=bad"}} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNotImplemented)
		require.NotContains(t, err.Error(), "git ")
	}
	var out bytes.Buffer
	require.ErrorIs(t, Run(t.Context(), []string{"bump", "jq", ""}, Streams{Out: &out, Err: &out}, config), upstream.ErrVersionInput)
	require.ErrorIs(t, Run(t.Context(), []string{"bump", "jq", "2", "--diff"}, Streams{Out: &out, Err: &out}, config), prepare.ErrNotImplemented)
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func TestRevisionPreviewCLIUsesCommittedSourceWithoutStateOrProvider(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts port-tclsh is required for preview integration test")
	}
	root := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", "-b", "candidate", root).CombinedOutput()
	require.NoError(t, err, "%s", output)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	blob, err := repo.WriteBlob(t.Context(), []byte("PortSystem 1.0\nname fixture\nversion 1.2\nrevision 0\ncategories devel\n"))
	require.NoError(t, err)
	tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Desired: git.RefValue{Exists: true, Object: commit}}}))
	config := app.Config{Repository: root, DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), TclExecutable: executable}
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--diff", "--reason", "rebuild"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Contains(t, stdout.String(), "-revision 0\n+revision 1")
	require.Contains(t, stderr.String(), "Branch: candidate")
	require.Contains(t, stderr.String(), "working-tree edits are excluded")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "index"))
	actual, _, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, commit, actual)
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--diff", "--branch", "candidate", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	var result app.Preview
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, "candidate", result.Branch)
	require.Contains(t, result.Diff, "+revision 1")
	require.Empty(t, stderr.String())
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}
