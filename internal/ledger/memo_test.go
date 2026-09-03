package ledger

// The decline memo: a content-addressed cache of judgments.
//
// Two properties are load-bearing and everything here serves one of
// them. The key must be the WHOLE of the input, so that any difference
// at all is a miss rather than a wrong answer — proven one component at
// a time, including every component of the environment digest. And a
// decline the network decided must never reach the ref, proven by
// asking the store to keep one and then reading the ref back to see
// that nothing landed.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/plan"
)

// groupDir writes a PortGroup directory with two groups in it and
// returns the path — the tree component of the environment digest, in
// miniature.
func groupDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "_resources", "port1.0", "group")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	write(t, dir, "github-1.0.tcl", "# github\n")
	write(t, dir, "golang-1.0.tcl", "# golang\n")
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

// testEnv is a complete environment over a freshly written group
// directory.
func testEnv(t *testing.T) Env {
	t.Helper()
	return Env{
		PortGroupDir: groupDir(t),
		MacPorts:     "2.11.5",
		Prefix:       "/opt/local",
		Platform:     "macosx_24_arm64",
		Shim:         "shim-3",
	}
}

// testKey is a complete key over a complete environment.
func testKey(t *testing.T) MemoKey {
	t.Helper()
	digest, err := testEnv(t).Digest()
	require.NoError(t, err)
	return MemoKey{
		Env:      digest,
		Intent:   "bump",
		Params:   "version=1.8.2",
		Portdir:  "sysutils/jq",
		Subport:  "",
		Variants: "",
		Portfile: []byte("version 1.7\n"),
	}
}

// A blank component is a refusal, not a digest. Keying on a gap would
// collide two environments under one name, silently and permanently.
func TestEnvDigestRefusesAnIncompleteEnvironment(t *testing.T) {
	full := testEnv(t)
	for _, blank := range []struct {
		name string
		set  func(*Env)
	}{
		{"PortGroupDir", func(e *Env) { e.PortGroupDir = "" }},
		{"MacPorts", func(e *Env) { e.MacPorts = "" }},
		{"Prefix", func(e *Env) { e.Prefix = "" }},
		{"Platform", func(e *Env) { e.Platform = "" }},
		{"Shim", func(e *Env) { e.Shim = "" }},
	} {
		e := full
		blank.set(&e)
		_, err := e.Digest()
		require.ErrorIs(t, err, ErrEnvIncomplete, "%s left blank", blank.name)
	}
	_, err := full.Digest()
	assert.NoError(t, err, "and the complete one digests")
}

// Every component of the environment, moved in turn, is a different
// digest — the PortGroups tree three ways, since a group can be edited,
// added or removed and each has to move the answer.
func TestEachEnvironmentComponentMovesTheDigest(t *testing.T) {
	base := testEnv(t)
	was, err := base.Digest()
	require.NoError(t, err)

	moved := func(name string, e Env) {
		t.Helper()
		got, err := e.Digest()
		require.NoError(t, err)
		assert.NotEqual(t, was, got, "%s did not move the digest", name)
	}
	moved("MacPorts", Env{base.PortGroupDir, "2.12.0", base.Prefix, base.Platform, base.Shim})
	moved("Prefix", Env{base.PortGroupDir, base.MacPorts, "/opt/mports", base.Platform, base.Shim})
	moved("Platform", Env{base.PortGroupDir, base.MacPorts, base.Prefix, "macosx_23_arm64", base.Shim})
	moved("Shim", Env{base.PortGroupDir, base.MacPorts, base.Prefix, base.Platform, "shim-4"})

	// A group's content. This is the case bump-revision's own comment
	// names — "the port evaluates to revision N with no revision line to
	// increment; the counter is coming from somewhere, a PortGroup most
	// likely" — and it moves not one byte of the Portfile.
	write(t, base.PortGroupDir, "golang-1.0.tcl", "# golang, now supplying a revision\n")
	moved("an edited group", base)

	edited, err := base.Digest()
	require.NoError(t, err)
	write(t, base.PortGroupDir, "perl5-1.0.tcl", "# perl5\n")
	added, err := base.Digest()
	require.NoError(t, err)
	assert.NotEqual(t, edited, added, "an added group did not move the digest")

	require.NoError(t, os.Remove(filepath.Join(base.PortGroupDir, "perl5-1.0.tcl")))
	back, err := base.Digest()
	require.NoError(t, err)
	assert.Equal(t, edited, back, "removing it again returns the same digest; content is the whole key")
}

// A tree with no PortGroups at all digests as no groups rather than as
// a failure — and the day it gains one, the digest moves.
func TestEnvDigestTreatsAMissingGroupDirAsNoGroups(t *testing.T) {
	root := t.TempDir()
	e := Env{
		PortGroupDir: filepath.Join(root, "absent"),
		MacPorts:     "2.11.5", Prefix: "/opt/local",
		Platform: "macosx_24_arm64", Shim: "shim-3",
	}
	empty, err := e.Digest()
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(e.PortGroupDir, 0o755))
	write(t, e.PortGroupDir, "github-1.0.tcl", "# github\n")
	got, err := e.Digest()
	require.NoError(t, err)
	assert.NotEqual(t, empty, got)
}

// Each key component in turn, plus the format integer, and every one of
// them is a different name. Anything less and the memo would answer a
// question it was not asked.
func TestEachKeyComponentMovesTheName(t *testing.T) {
	base := testKey(t)
	was := base.name(MemoFormat, 40)

	moved := func(name string, k MemoKey) {
		t.Helper()
		assert.NotEqual(t, was, k.name(MemoFormat, 40), "%s did not move the key", name)
	}
	env := base
	env.Env = "a different environment"
	moved("Env", env)

	verb := base
	verb.Intent = "refresh-checksums"
	moved("Intent", verb)

	params := base
	params.Params = "version=1.8.3"
	moved("Params", params)

	dir := base
	dir.Portdir = "sysutils/jq2"
	moved("Portdir", dir)

	sub := base
	sub.Subport = "jq-devel"
	moved("Subport", sub)

	variants := base
	variants.Variants = "+universal"
	moved("Variants", variants)

	src := base
	src.Portfile = []byte("version 1.8\n")
	moved("Portfile", src)

	assert.NotEqual(t, was, base.name(MemoFormat+1, 40),
		"the format integer did not move the key; a rule change would keep answering from the old build's memos")
	assert.Equal(t, was, base.name(MemoFormat, 40), "and the same key is the same name")
}

// The components are length-prefixed, so no two different keys can hash
// alike by joining differently.
func TestKeyComponentsCannotSpellEachOther(t *testing.T) {
	a := MemoKey{Env: "e", Intent: "ab", Params: "c"}
	b := MemoKey{Env: "e", Intent: "a", Params: "bc"}
	assert.NotEqual(t, a.name(MemoFormat, 40), b.name(MemoFormat, 40))
}

// The name must parse as an object name in the repository's own hash
// format: a 40-hex key in a sha256 repository is git's fatal band, not
// a miss.
func TestKeyNameIsTheRepositorysWidth(t *testing.T) {
	k := testKey(t)
	assert.Len(t, k.name(MemoFormat, 40), 40)
	assert.Len(t, k.name(MemoFormat, 64), 64)
	assert.Equal(t, k.name(MemoFormat, 64)[:40], k.name(MemoFormat, 40),
		"one digest, cut to fit; the two widths are the same answer")
}

// The round trip, against real git: what a memo stores is what a hit
// hands back, sentence and code alike, so a remembered decline reads
// exactly like a re-derived one.
func TestMemoRoundTripsADecline(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	memo := OpenMemo(repo)
	k := testKey(t)

	_, hit, err := memo.Lookup(ctx, k)
	require.NoError(t, err)
	assert.False(t, hit, "an empty ref misses")

	kept := &plan.Decline{
		Type:   plan.RevisionShapeAmbiguous,
		Detail: "the port evaluates to revision 3 with no revision line to increment",
	}
	require.NoError(t, memo.Store(ctx, k, kept))

	got, hit, err := memo.Lookup(ctx, k)
	require.NoError(t, err)
	require.True(t, hit)
	assert.Equal(t, kept.Type, got.Type)
	assert.Equal(t, kept.Detail, got.Detail)
	assert.Equal(t, kept.Error(), got.Error(), "the same sentence, so the same output")
	assert.Equal(t, plan.ByPortfile, got.Determined,
		"a memo that exists is portfile-determined by the store's own rule")

	// The Portfile edited to fix the very thing that was declined: a
	// different key, so a miss, so the work is done again. No
	// invalidation, no TTL — the content IS the address.
	edited := k
	edited.Portfile = []byte("version 1.7\nrevision 0\n")
	_, hit, err = memo.Lookup(ctx, edited)
	require.NoError(t, err)
	assert.False(t, hit, "a changed Portfile re-arms the memo the moment it is saved")

	require.NoError(t, memo.Forget(ctx, k))
	_, hit, err = memo.Lookup(ctx, k)
	require.NoError(t, err)
	assert.False(t, hit, "and forgetting one is a miss again")
}

// The gate: a decline the network decided is refused, and nothing lands
// on the ref. Both halves matter — a store that returned an error and
// wrote anyway would pass a test that only read the error.
func TestMemoWillNotStoreANetworkDecidedDecline(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	memo := OpenMemo(repo)
	k := testKey(t)

	for _, refused := range []*plan.Decline{
		// The resolution itself failing. Unreachable from here by
		// construction too, since the memo is consulted after resolution.
		{Type: plan.LatestUnresolved, Detail: "livecheck found nothing"},
		// refresh-checksums' already-current: the single most dangerous
		// memo in the tree. Remembering it would permanently suppress the
		// re-rolled-distfile detection the verb exists for.
		{Type: plan.AlreadyCurrent, Detail: "recorded checksums match what upstream serves",
			Determined: plan.ByNetwork},
		// A producer of an otherwise storable kind that knows better.
		{Type: plan.TargetNotReached, Detail: "upstream moved under us", Determined: plan.ByNetwork},
		// Silence on a kind whose producers disagree.
		{Type: plan.VendoredBlock, Detail: "a patch file vetoes the lockfile"},
		// Riders the key does not name.
		{Type: plan.SubportsChanged, Detail: "1 added, 0 removed", Withheld: []string{"modeline"}},
		nil,
	} {
		err := memo.Store(ctx, k, refused)
		require.ErrorIs(t, err, ErrNotMemoizable)
	}

	_, hit, err := memo.Lookup(ctx, k)
	require.NoError(t, err)
	assert.False(t, hit, "the refusals wrote nothing")
	shas, err := repo.NotesList(ctx, PlanNotesRef)
	require.NoError(t, err)
	assert.Empty(t, shas, "and the ref does not exist at all")
}

// A note this build cannot read is a miss, never an error: the memo is
// a cache under D8, and the worst an unreadable one costs is the work
// it would have saved.
func TestUnreadableMemosAreMisses(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	memo := OpenMemo(repo)
	k := testKey(t)
	name, err := memo.name(ctx, k)
	require.NoError(t, err)

	for _, body := range []string{
		"not json at all\n",
		`{"format":999,"code":"subports-changed"}`,
		`{"format":1,"code":"a-kind-this-build-never-heard-of"}`,
		`{"format":1,"code":"unknown-decline"}`,
	} {
		require.NoError(t, repo.NoteWrite(ctx, PlanNotesRef, name, []byte(body)))
		got, hit, err := memo.Lookup(ctx, k)
		require.NoError(t, err, "%q", body)
		assert.False(t, hit, "%q read as a hit", body)
		assert.Nil(t, got)
	}
}

// A repository with no commit cannot say how wide an object name is,
// and the memo refuses loudly rather than guessing forty.
//
// Guessing is the failure worth refusing: a forty-hex key in a sha256
// repository is git's fatal band, not a miss, so every store and every
// lookup would error from then on with a message about resolving a ref.
// There is also nothing lost by refusing — a repository with no commit
// has nothing to annotate either.
func TestMemoRefusesARepositoryThatCannotSizeAKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, "git", "init", "--quiet", dir)
	require.NoError(t, cmd.Run())
	repo, err := git.Open(ctx, realTools, dir)
	require.NoError(t, err)

	_, _, err = OpenMemo(repo).Lookup(ctx, testKey(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object format", "the refusal names what it could not learn")

	err = OpenMemo(repo).Store(ctx, testKey(t), &plan.Decline{Type: plan.SubportsChanged})
	assert.Error(t, err, "and it refuses the write for the same reason")
}
