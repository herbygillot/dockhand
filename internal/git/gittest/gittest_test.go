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

	// The fork takes a push, and the remote-tracking ref it writes is
	// what PushedTo reads back.
	tip, terr := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, terr)
	require.NoError(t, repo.PushExact(ctx, "herby", tip, "dockhand/jq-1.8", ""))
	to, perr := repo.PushedTo(ctx, "dockhand/jq-1.8")
	require.NoError(t, perr)
	assert.Equal(t, "herby", to)
}

func TestFetchedLeavesOnlyTheRemoteTrackingRef(t *testing.T) {
	ctx := context.Background()
	repo := PortsTree(t, realTools)
	// Upstream's commit, minted on a scratch branch that is then
	// removed: the object stays, and only the remote-tracking ref
	// names it, which is what a fetch of a moved remote leaves.
	ahead := Commit(t, repo, "scratch", "main", "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	Fetched(t, repo, "origin", "main", ahead)
	// Removed the only way a ref moves now: one delete line, with the
	// value it must hold, in an UpdateRefs batch.
	require.NoError(t, repo.UpdateRefs(ctx, []git.RefUpdate{{Ref: "refs/heads/scratch", Old: ahead}}))

	got, err := repo.RevParse(ctx, "refs/remotes/origin/main")
	require.NoError(t, err)
	assert.Equal(t, ahead, got)
	branches, err := repo.Branches(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, branches, "a remote-tracking ref is not a local branch")
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	assert.Equal(t, "main", primary, "and does not change which branch is primary")
	assert.True(t, repo.IsAncestor(ctx, primary, ahead), "the local primary is behind what was fetched")
}

// TestFixturesReproduceTheGoldenShas pins that the fixture tree is
// BYTE-STABLE: under a fixed date, the two-port tree and the branches
// minted on it land on recorded shas. Identity, message, file modes and
// bytes are all in those shas, so a fixture that drifts by a byte fails
// here — which means a change to the shape of the tree every test in
// this repository runs against is deliberate and reviewed, rather than
// discovered later as unrelated breakage somewhere downstream.
//
// IT NAMED internal/cmd/testdata/golden AS WHAT IT DEFENDED, and that
// directory does not exist: internal/cmd went with the overhaul, and
// the only mention of those goldens left in the tree was this sentence.
// The canary outlived the thing it was watching for, which is worth
// writing down rather than quietly re-recording, because a pin whose
// stated reason is gone is a pin nobody can decide about.
//
// The property it still has is the one that made it fire: it is the
// tripwire on the fixture itself.
//
// RE-RECORDED ONCE, DELIBERATELY. Init did not create
// macports.PortGroupDir, which is the single structural feature
// tree.Open stats to decide whether a directory IS a ports tree — so
// every test ran against a repository full of portdirs that dockhand
// itself would have refused, and the comment in Init already argued
// that this was the wrong shape for a fixture to have. Adding it moved
// every sha, because it is one more file in the first commit. Nothing
// about how a commit is constructed changed.
func TestFixturesReproduceTheGoldenShas(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "2026-09-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-09-01T00:00:00Z")
	repo := Init(t, realTools, "", map[string]string{
		"sysutils/jq/Portfile": "version 1.7\n",
		"devel/olm/Portfile":   "version 3.2.16\nmaintainers nomaintainer\n",
	})
	for _, c := range []struct{ version, sha string }{
		{"2.0", "961232f8a0df5e0b706b53c2cc7591fcea45ba86"},
		{"2.2", "15e591c1b3934b093438e125573965d5405b1411"},
		{"2.3", "348277003520f7e3fa10fd4defac91bab1a3831e"},
	} {
		got := Commit(t, repo, "dockhand/jq-"+c.version, "main", "sysutils/jq/Portfile",
			"version "+c.version+"\n", "jq: update to "+c.version)
		assert.Equal(t, c.sha, got, "dockhand/jq-%s", c.version)
	}
}
