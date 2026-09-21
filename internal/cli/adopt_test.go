package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestAdoptCLITracksAHandMadeBranchAndDryRunRecordsNothing(t *testing.T) {
	t.Parallel()
	config, repo, base := preparationCLI(t)
	blob, err := repo.WriteBlob(t.Context(), []byte("PortSystem 1.0\nname fixture\nversion 1.3\nrevision 0\ncategories devel\n"))
	require.NoError(t, err)
	tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{base}, Message: "fixture: update to 1.3 by hand", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/mine", Desired: git.RefValue{Exists: true, Object: commit}}}))

	var out, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"adopt", "mine", "--dry-run", "--json"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	var result workflow.AdoptResult
	decodeResult(t, out.Bytes(), &result)
	require.Equal(t, "devel/fixture/Portfile", result.Portfile)
	require.Equal(t, "fixture", result.Change.InitiatingTarget)
	require.Contains(t, result.Detail, "nothing recorded")
	out.Reset()
	require.NoError(t, Run(t.Context(), []string{"adopt", "mine"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Contains(t, out.String(), "fixture (mine): tracked")
	require.Contains(t, out.String(), "verify fixture builds it")
	err = Run(t.Context(), []string{"adopt", "mine"}, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "already tracked as the contribution for fixture")
	err = Run(t.Context(), []string{"adopt", "candidate"}, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "one commit above a commit of master", "the fixture's candidate is master itself, a root commit")
	err = Run(t.Context(), []string{"adopt", "not a branch"}, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "literal local branch")
}
