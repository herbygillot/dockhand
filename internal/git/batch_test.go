package git

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// session opens a batch against a fresh repository and closes it with
// the test, so every case below reads from one live `cat-file --batch`.
func session(t *testing.T) (*Repo, *Batch) {
	t.Helper()
	r := newRepo(t)
	b, err := r.CatFile(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	return r, b
}

// The absence wire. A request git cannot resolve is an ANSWER — the
// process stays up and exits zero — and statestore.Read turns exactly
// this into ErrNoState, so it must never arrive as a transport failure.
func TestObjectReportsAMissingRequestAsErrNoObject(t *testing.T) {
	_, b := session(t)

	_, err := b.Object("refs/dockhand/state^{commit}")
	require.ErrorIs(t, err, ErrNoObject)
	require.ErrorContains(t, err, "refs/dockhand/state", "the refusal names what was asked for")

	// And the session survives it: an absence is not a broken stream.
	obj, err := b.Object("HEAD^{commit}")
	require.NoError(t, err)
	assert.Equal(t, "commit", obj.Type)
}

// One session answers many requests, which is the whole point of the
// type: N records cost one process rather than N.
func TestObjectReadsManyObjectsOverOneSession(t *testing.T) {
	r, b := session(t)
	ctx := context.Background()

	want := map[string]string{}
	for i := range 20 {
		content := fmt.Sprintf("version 1.%d\n", i)
		blob, err := r.gitStdin(ctx, []byte(content), "hash-object", "-w", "--stdin")
		require.NoError(t, err)
		want[blob] = content
	}
	for oid, content := range want {
		obj, err := b.Object(oid)
		require.NoError(t, err)
		assert.Equal(t, "blob", obj.Type)
		assert.Equal(t, oid, obj.OID)
		assert.Equal(t, content, string(obj.Data), "the bytes are the object's own, with no framing left on")
	}
}

// A blob whose bytes contain the framing git wraps them in — a newline,
// and a line that looks like a header — still comes back whole: the
// length in the header is what bounds the read, never a scan for a
// delimiter.
func TestObjectReadsBytesThatLookLikeFraming(t *testing.T) {
	r, b := session(t)
	content := "deadbeef blob 12\n\n\nnot a header\n"

	oid, err := r.gitStdin(context.Background(), []byte(content), "hash-object", "-w", "--stdin")
	require.NoError(t, err)
	obj, err := b.Object(oid)
	require.NoError(t, err)
	assert.Equal(t, content, string(obj.Data))
}

// Tree parses the binary entry form, which is what lets a flat tree of
// records be read without one `ls-tree` per record and without ever
// addressing a record as <tree>:<name>.
func TestTreeReadsTheEntriesOfATree(t *testing.T) {
	r, b := session(t)

	entries, err := b.Tree("HEAD^{tree}")
	require.NoError(t, err)
	byName := map[string]TreeEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	require.Contains(t, byName, "README")
	require.Contains(t, byName, "sysutils")

	assert.Equal(t, "100644", byName["README"].Mode)
	assert.False(t, byName["README"].Dir(), "a file is not a directory")
	assert.True(t, byName["sysutils"].Dir(), "a category is")

	blob, err := b.Object(byName["README"].OID)
	require.NoError(t, err)
	assert.Equal(t, "a tree\n", string(blob.Data), "the entry's oid addresses the blob directly")

	// The oid width comes off the tree's own object name, so the entry
	// ids are the same length as the tree's.
	tree, err := r.RevParse(context.Background(), "HEAD^{tree}")
	require.NoError(t, err)
	assert.Len(t, byName["README"].OID, len(tree))
}

// The empty tree is git's own, present in every repository whether or
// not anything wrote it — which is what lets the store's first amend
// graft onto EmptyTree in a checkout that has never run dockhand.
func TestTreeReadsTheEmptyTree(t *testing.T) {
	_, b := session(t)

	entries, err := b.Tree("4b825dc642cb6eb9a060e54bf8d69288fbee4904")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A blob is not a tree, and saying so is better than parsing its bytes
// as entries and reporting whatever garbage they spell.
func TestTreeRefusesAnObjectThatIsNotATree(t *testing.T) {
	_, b := session(t)

	_, err := b.Tree("HEAD^{commit}")
	require.Error(t, err)
	assert.ErrorContains(t, err, "not a tree")
}

// A request is one line, so a name carrying a newline would be two —
// and the second would be answered into the first's slot, putting every
// object after it off by one. Refused before it reaches the wire, and
// the session stays usable.
func TestObjectRefusesARequestThatIsNotOneLine(t *testing.T) {
	_, b := session(t)

	_, err := b.Object("HEAD\nHEAD")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoObject, "a malformed request is not a missing object")

	obj, err := b.Object("HEAD^{tree}")
	require.NoError(t, err)
	assert.Equal(t, "tree", obj.Type)
}

// Close reaps the process. A session closed twice answers the same way
// rather than waiting for a child that has already been collected.
func TestCloseIsSafeToRepeat(t *testing.T) {
	r := newRepo(t)
	b, err := r.CatFile(context.Background())
	require.NoError(t, err)

	require.NoError(t, b.Close())
	require.NoError(t, b.Close())
}
