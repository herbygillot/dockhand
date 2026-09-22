package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/app"
	"path/filepath"
	"strings"
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
	require.NoDirExists(t, filepath.Dir(config.DBPath), "a dry run creates no database")
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

func TestPreparationsLandOnAnAdoptedBranchAsAmendments(t *testing.T) {
	t.Parallel()
	config, repo, base := preparationCLI(t)
	handMade := func(branch, version string) string {
		blob, err := repo.WriteBlob(t.Context(), []byte("PortSystem 1.0\nname fixture\nversion "+version+"\nrevision 0\ncategories devel\n"))
		require.NoError(t, err)
		tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
		require.NoError(t, err)
		tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Mode: 0o40000, Type: "tree", Object: tree}})
		require.NoError(t, err)
		tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Mode: 0o40000, Type: "tree", Object: tree}})
		require.NoError(t, err)
		sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
		commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{base}, Message: "fixture: update to " + version + " by hand", Author: sig, Committer: sig})
		require.NoError(t, err)
		require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/" + branch, Desired: git.RefValue{Exists: true, Object: commit}}}))
		return commit
	}
	handMade("mine", "1.3")
	var out, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--adopt", "mine", "--dry-run"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.NoDirExists(t, filepath.Dir(config.DBPath), "a dry run creates no database, adoption included")
	require.Contains(t, out.String(), "-revision 0\n+revision 1")
	require.Contains(t, out.String(), " version 1.3", "the edit is made on the branch's Portfile, not master's")
	out.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"adopt", "mine"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Contains(t, out.String(), "fixture (mine): tracked")

	// A preview of a port mid-contribution previews onto the contribution,
	// as the bump would land, and reads the records without touching them.
	out.Reset()
	stderr.Reset()
	before, _, err := repo.Branch(t.Context(), "mine")
	require.NoError(t, err)
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--dry-run", "--json", "--subject", "rebuild"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	var preview app.Preview
	decodeResult(t, out.Bytes(), &preview)
	require.Equal(t, "mine", preview.Branch, "the preview lands where the bump would")
	require.Contains(t, preview.Diff, " version 1.3", "the edit is previewed on the contribution's Portfile, not master's")
	require.Contains(t, preview.Diff, "-revision 0\n+revision 1")
	require.Contains(t, stderr.String(), "lands as an amendment")
	after, _, err := repo.Branch(t.Context(), "mine")
	require.NoError(t, err)
	require.Equal(t, before, after)

	out.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--to", "branch", "--json", "--timestamps"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Contains(t, stderr.String(), "lands as an amendment")
	require.Regexp(t, `"message":"fixture: took [^"]*preparation `, stderr.String(), "a finished job says how long its phases took, from the record")
	require.Regexp(t, `"time":"20[0-9]{2}-[0-9]{2}-[0-9]{2}T`, stderr.String(), "--timestamps puts the time on JSON reports")
	out.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"status", "fixture", "-v"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Regexp(t, `took: preparation [^;]*; in all `, out.String(), "status -v shows the same durations")
	head, tree, err := repo.Branch(t.Context(), "mine")
	require.NoError(t, err)
	_, portfile, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	require.Contains(t, string(portfile), "version 1.3\nrevision 1\n")
	message, err := repo.CommitMessage(t.Context(), head)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(message, "fixture: update to 1.3 by hand"), "the contribution keeps its message: %q", message)

	handMade("theirs", "1.4")
	out.Reset()
	stderr.Reset()
	err = Run(t.Context(), []string{"bump-revision", "fixture", "--adopt", "theirs", "--subject", "rebuild", "--to", "branch"}, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "fixture already has an open contribution on branch mine", "one open contribution per port: the second branch is refused")
}
