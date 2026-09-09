package cli

import (
	"bytes"
	"context"
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
)

// A CHANGE IS CUT FROM UPSTREAM'S NEWEST TIP, so the branch a maintainer
// is handed is as recent as the project is — a checkout a week behind
// used to produce a branch a week behind, which merges badly and reviews
// against stale neighbours, and which nothing in dockhand's output said.
func TestTheBaseIsUpstreamsTrackingRefWhenFetchingIsAllowed(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	var err bytes.Buffer
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), Err: &err, repo: repo}

	// No remote at all, so the upstream cannot be established — and the
	// point of this case is that a bump still plans. "origin" is a
	// convention, not a fact, so a checkout that cannot say which remote
	// is the project must not have one picked for it.
	ref, ferr := s.BaseRef(context.Background(), true)
	require.NoError(t, ferr, "an upstream that could not be established is not a planning error")

	primary, perr := repo.PrimaryBranch(context.Background())
	require.NoError(t, perr)
	assert.Equal(t, primary, ref, "it falls back to the local primary")
	assert.Contains(t, err.String(), "not fetching",
		"and it says so: a base quietly older than it claims is the silence rule 7 forbids")
	assert.Contains(t, err.String(), "may be behind")
}

// --no-fetch TAKES THE LOCAL PRIMARY AND ASKS NOTHING. It is spelled as
// a refusal because fetching is the default: a branch cut from a stale
// base is the defect, and the person who wants the stale one is making
// the unusual ask.
func TestNoFetchTakesTheLocalPrimaryAndSaysNothing(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	var err bytes.Buffer
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), Err: &err, repo: repo}

	ref, ferr := s.BaseRef(context.Background(), false)
	require.NoError(t, ferr)
	primary, perr := repo.PrimaryBranch(context.Background())
	require.NoError(t, perr)
	assert.Equal(t, primary, ref)
	assert.Empty(t, err.String(), "declining the network is not a failure to report")
}

// ONE FETCH PER INVOCATION, WHATEVER IT IS PLANNING. A sweep plans
// hundreds of ports through a worker pool; one round trip per port would
// be hundreds of them to learn the same fact, and — worse — the ports at
// the end of a long sweep would be based on a different commit from the
// ones at the start. One invocation mints one base.
func TestTheBaseIsResolvedOncePerInvocation(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	var err bytes.Buffer
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), Err: &err, repo: repo}

	for range 5 {
		_, ferr := s.BaseRef(context.Background(), true)
		require.NoError(t, ferr)
	}
	assert.Equal(t, 1, bytes.Count(err.Bytes(), []byte("not fetching")),
		"the failure was reported once, so the attempt was made once")
}

// AND THE TWO REMEDIES ARE NAMED SEPARATELY, because basing on a fetched
// upstream added a second cause of drift that the person did nothing to
// create: the port moved under them. A message naming only their own
// edits would send them hunting for one that does not exist.
func TestDriftNamesTheRemedyThatFitsTheBase(t *testing.T) {
	assert.Contains(t, driftRemedy(true), "git pull")
	assert.Contains(t, driftRemedy(true), "edits on your primary branch")
	assert.NotContains(t, driftRemedy(false), "git pull",
		"without a fetch the base is their own branch, so the only cause is their own edit")
}

// A VERB THAT KEEPS RECORDS REFUSES A DIRECTORY THAT IS NOT A PORTS
// TREE, and it used to answer about whichever repository the person
// happened to be standing in.
//
// Acquire opened "." whenever no tree had been discovered. In a git
// repository that is not a ports tree — dockhand's own source checkout,
// say — `status` then read that repository's absent state ref and said
// "dockhand has recorded nothing in this checkout yet", and `purge` said
// "removed 0 branch(es)". Both sentences are true about the wrong place,
// and a person told their work is clean does not go looking for it.
//
// `outdated` in the same directory exited 40 and named the problem, so
// one invocation knew and the next reported a clean checkout. That
// difference is the bug: rule 7 says "I could not find out" must never
// arrive as "there is none".
func TestARepositoryThatIsNotAPortsTreeIsRefused(t *testing.T) {
	// A git repository with no _resources/port1.0/group: a real one, so
	// what is being proven is the ports-tree question and not a missing
	// repository.
	plain := t.TempDir()
	gittest.Init(t, testFinder(), plain, map[string]string{"README.md": "not a ports tree\n"})
	require.NoError(t, os.RemoveAll(filepath.Join(plain, "_resources")))
	t.Chdir(plain)

	s := &Services{Tools: testFinder(), Err: &bytes.Buffer{}}
	err := s.Acquire(t.Context(), app.Needs{Repo: true})
	require.Error(t, err, "the records live in the tree's checkout; there is no tree here")
	require.ErrorIs(t, err, tree.ErrNotPortsTree)
	assert.Equal(t, exitcode.NotPortsTree, ExitCode(err),
		"and it is the same band `outdated` gives from the same directory")
}

// AND A PORTS TREE IS STILL ACQUIRED, which is the half that must not
// break: the check is a stat on the tree root and never a second opinion
// about the repository.
func TestAPortsTreeCheckoutIsAcquired(t *testing.T) {
	repo := gittest.PortsTree(t, testFinder())
	s := &Services{TreeRoot: repo.Root, Tools: testFinder(), Err: &bytes.Buffer{}}
	require.NoError(t, s.Acquire(t.Context(), app.Needs{Repo: true}))

	got, err := s.Repo()
	require.NoError(t, err)
	assert.Equal(t, repo.Root, got.Root)
	_, err = s.State()
	require.NoError(t, err, "a repository implies the store")

	// The tree OBJECT is not acquired by asking for a repository: n.Tree
	// is what pays for the index.
	_, terr := s.Tree()
	assert.Error(t, terr, "the check is a stat, not an acquisition")
}
