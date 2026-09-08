package cli

import (
	"bytes"
	"context"
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
