package gittest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/tool"
)

// realTools is the real PATH search: git is genuinely driven.
var realTools = tool.NewFinder(nil)

func TestPortsTreeIsOneCommitOnMain(t *testing.T) {
	ctx := context.Background()
	repo := PortsTree(t, realTools)

	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	assert.Equal(t, "main", primary, "the default branch is pinned, whatever the machine's config says")
	branches, err := repo.Branches(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, branches)
	history, err := repo.RevList(ctx, "main", 10)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	subject, err := repo.Subject(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, "initial tree", subject)
	blob, err := repo.BlobAt(ctx, "main", "sysutils/jq/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 1.7\n", string(blob))
}

func TestInitCommitsWhatIsAlreadyThere(t *testing.T) {
	// A caller that populated the directory itself — a copied port
	// fixture — hands Init the directory and no files.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "devel", "x"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devel", "x", "Portfile"), []byte("version 9\n"), 0o644))
	repo := Init(t, realTools, dir, nil)

	blob, err := repo.BlobAt(context.Background(), "main", "devel/x/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 9\n", string(blob))
}

func TestCommitMintsAndMoveBranchLeavesTheReflog(t *testing.T) {
	ctx := context.Background()
	repo := PortsTree(t, realTools)

	tip := Commit(t, repo, "dockhand/jq-1.8", "main", "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	got, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, tip, got)
	assert.True(t, repo.IsAncestor(ctx, "main", tip))
	blob, err := repo.BlobAt(ctx, tip, "sysutils/jq/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 1.8\n", string(blob))
	subject, err := repo.Subject(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, "jq: update to 1.8", subject)

	// An amend: the branch moves to a sibling commit, and the old tip
	// survives only on the reflog.
	fixed := Commit(t, repo, "scratch", "main", "sysutils/jq/Portfile", "version 1.8.1\n", "jq: update to 1.8.1")
	MoveBranch(t, repo, "dockhand/jq-1.8", fixed)
	got, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, fixed, got)
	assert.False(t, repo.IsAncestor(ctx, tip, fixed))
	assert.True(t, repo.FormerTips(ctx, "dockhand/jq-1.8")[tip], "the former tip is on the reflog")
}

func TestNoteWritesTheRawBytes(t *testing.T) {
	ctx := context.Background()
	repo := PortsTree(t, realTools)
	tip := Commit(t, repo, "dockhand/jq-1.8", "main", "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")

	Note(t, repo, tip, "{not json")
	body, err := repo.NoteRead(ctx, git.VerifyNotesRef, tip)
	require.NoError(t, err)
	assert.Equal(t, "{not json\n", string(body), "git completes the final line, as notes add -m did")

	Note(t, repo, tip, "second")
	body, err = repo.NoteRead(ctx, git.VerifyNotesRef, tip)
	require.NoError(t, err)
	assert.Equal(t, "second\n", string(body), "a second note replaces the first")
}

func TestBareForkNamesBothRemotes(t *testing.T) {
	ctx := context.Background()
	repo := PortsTree(t, realTools)
	Commit(t, repo, "dockhand/jq-1.8", "main", "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")

	fork := BareFork(t, repo, "herbygillot", "herby")
	assert.Equal(t, "ports", filepath.Base(fork))
	assert.Equal(t, "herbygillot", filepath.Base(filepath.Dir(fork)), "the owner segment names the login")
	remotes, err := repo.Remotes(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"origin": UpstreamURL, "herby": fork}, remotes)

	// The fork takes a push, and the push records the tracking remote
	// a PR lookup reads.
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))
	assert.Equal(t, "herby", repo.TrackedRemote(ctx, "dockhand/jq-1.8"))
}

// TestFixturesReproduceTheGoldenShas pins the property the goldens
// depend on: under the golden date, the two-port tree cmd's golden
// fixtures build, and the branches minted on it, land on the shas the
// recorded goldens carry. Identity, message, file modes and bytes are
// all in those shas, so a fixture that drifts by a byte fails here
// before it fails every golden under internal/cmd/testdata/golden.
func TestFixturesReproduceTheGoldenShas(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "2026-09-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-09-01T00:00:00Z")
	repo := Init(t, realTools, "", map[string]string{
		"sysutils/jq/Portfile": "version 1.7\n",
		"devel/olm/Portfile":   "version 3.2.16\nmaintainers nomaintainer\n",
	})
	for _, c := range []struct{ version, sha string }{
		{"2.0", "27423649d22e7bb6bb26e23e7bf2f737e76e5fd6"},
		{"2.2", "44cfc9eea2501444a372ea718e90925fa4f59c3f"},
		{"2.3", "7a42be6e7ed19d5592a652c8d16ba6ab99edba04"},
	} {
		got := Commit(t, repo, "dockhand/jq-"+c.version, "main", "sysutils/jq/Portfile",
			"version "+c.version+"\n", "jq: update to "+c.version)
		assert.Equal(t, c.sha, got, "dockhand/jq-%s", c.version)
	}
}
