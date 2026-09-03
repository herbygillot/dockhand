package engine

// Ruling 4: what a realization does about a branch already standing,
// when the caller is a sweep.
//
// The two answers are not one answer with a flag. Advance is asked
// about the branch this change would have minted — the change is
// already there, so nothing is written and nothing is checked, which is
// what makes rerunning an interrupted sweep cheap on the ports it
// already did. Supersede is asked about an older change to the same
// port — that branch is set aside, not demolished, and the mint says
// which so the sweep's row can name it.
//
// Neither may ever destroy anything. --replace exists for that and it
// takes one port on purpose.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// runPlan is Run with the realization dropped, for the cases that are
// about what the repository looks like afterwards rather than about
// what came back.
func runPlan(t *testing.T, ctx context.Context, eng *Engine, p *plan.Plan, o Policy) error {
	t.Helper()
	_, err := eng.Run(ctx, p, o)
	return err
}

// sweepEngine is a mint fixture with its streams in hand: a ports tree,
// a fake verifier, and the two buffers a caller inspects.
func sweepEngine(t *testing.T) (*git.Repo, *Engine, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	repo := gittest.PortsTree(t, realTools)
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	return repo, testEngine(t, repo, &verifytest.Fake{}, out, errb), out, errb
}

// The same change met twice. The first mints; the second, under a
// sweep's Advance, finds the branch already carrying it and says so
// instead of refusing — and writes nothing, so the branch is still the
// one the first run made.
func TestAdvanceStandsDownOnItsOwnBranch(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	first, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"), Policy{Destination: record.ToBranch})
	require.NoError(t, err)
	require.Equal(t, BranchMinted, first.Realization)

	again, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch, OnInFlight: Advance})
	require.NoError(t, err, "a standing branch is an answer to a sweep, not a refusal")
	assert.Equal(t, BranchStood, again.Realization)
	assert.Equal(t, "dockhand/jq-1.8", again.Branch)
	assert.Empty(t, again.Sha, "nothing was minted, so there is no commit to name")

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, first.Sha, tip, "the standing branch was left exactly where it was")
}

// Refuse is unchanged by any of it: the single-port road still refuses
// over a standing branch, with the type whose remedy is a different
// verb.
func TestRefuseStillRefusesOverAStandingBranch(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	_, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"), Policy{Destination: record.ToBranch})
	var inFlight *BranchInFlightError
	require.ErrorAs(t, err, &inFlight)
	assert.Equal(t, "dockhand/jq-1.8", inFlight.Branch)
}

// Advance never touches the plan against the repository, which is the
// whole of what makes it cheap. A plan whose precondition no longer
// holds would be a drift refusal on any other road; here the branch is
// already there, so there is nothing to materialize and nothing to
// drift.
func TestAdvanceAnswersBeforeTheDriftCheck(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	drifted := bumpPlan(t, repo, "bump", "1.8")
	drifted.PortfileSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := eng.Run(ctx, drifted, Policy{Destination: record.ToBranch, OnInFlight: Refuse})
	require.ErrorIs(t, err, plan.ErrDrift, "the ordinary road resolves the plan and finds the drift")

	stood, err := eng.Run(ctx, drifted, Policy{Destination: record.ToBranch, OnInFlight: Advance})
	require.NoError(t, err)
	assert.Equal(t, BranchStood, stood.Realization)
}

// A newer change to the same port under Supersede: the new branch is
// minted, the older one is left standing with the field saying what
// replaced it, and the mint names it so a sweep's row can.
func TestSupersedeMintsBesideAndNamesWhatItSetAside(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	older, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"), Policy{Destination: record.ToBranch})
	require.NoError(t, err)

	newer, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.9"),
		Policy{Destination: record.ToBranch, OnInFlight: Supersede})
	require.NoError(t, err)
	require.Equal(t, BranchMinted, newer.Realization)
	assert.Equal(t, []string{"dockhand/jq-1.8"}, newer.Superseded)

	assert.True(t, repo.HasBranch(ctx, "dockhand/jq-1.8"),
		"the superseded branch is an end state of its own; nothing discards it")
	n, err := ledger.Open(repo).Read(ctx, older.Sha)
	require.NoError(t, err)
	assert.Equal(t, "dockhand/jq-1.9", n.SupersededBy)
}

// A first mint under Supersede has nothing to set aside and says so by
// naming nothing — the field is evidence, not decoration.
func TestSupersedeNamesNothingWhenNothingStood(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	done, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch, OnInFlight: Supersede})
	require.NoError(t, err)
	assert.Equal(t, BranchMinted, done.Realization)
	assert.Empty(t, done.Superseded)
}

// Supersede meeting the branch it would have minted — the race, or a
// sweep rerun against itself — stands down rather than failing. It is
// the same answer Advance gives, reached by the other road.
func TestSupersedeStandsDownOnAnIdenticalBranch(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	done, err := eng.Run(ctx, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch, OnInFlight: Supersede})
	require.NoError(t, err)
	assert.Equal(t, BranchStood, done.Realization)
}

// What a plan with no edits realizes, on every road: nothing, reported
// as nothing rather than as a mint of an empty commit.
func TestNothingRealizedOnAnEmptyPlan(t *testing.T) {
	ctx := context.Background()
	repo, eng, _, _ := sweepEngine(t)

	empty := bumpPlan(t, repo, "bump", "1.8")
	empty.Edits = nil

	done, err := eng.Run(ctx, empty, Policy{Destination: record.ToBranch, OnInFlight: Supersede})
	require.NoError(t, err)
	assert.Equal(t, NothingRealized, done.Realization)
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
}
