package engine

// The consult point: where a run asks the decline memo, and what it
// does with the answer.
//
// What is proven here is that the memo changes only what a run had to
// spend. A hit hands back the decline the first run made, unchanged, so
// a single-target invocation reads identically whether it was
// remembered or re-derived; a miss costs nothing but the lookup; and
// every way the memo can fail — no repository, no environment digest, a
// decline nobody may keep — leaves the run answering exactly what it
// would have answered without one.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
)

// memoFixture is an engine over a ports-tree-shaped repository, with
// the portdir and a complete key for the port that lives in it.
func memoFixture(t *testing.T) (*Engine, *git.Repo, string, ledger.MemoKey) {
	t.Helper()
	repo, _ := engineRepo(t)
	eng := testState(t, repo, nil)
	portdir := filepath.Join(repo.Root, "sysutils", "jq")
	env, err := MemoEnv(ledger.Env{
		PortGroupDir: filepath.Join(repo.Root, "_resources", "port1.0", "group"),
		MacPorts:     "2.11.5",
		Prefix:       "/opt/local",
		Platform:     "macosx_24_arm64",
		Shim:         "shim-3",
	})
	require.NoError(t, err)
	return eng, repo, portdir, ledger.MemoKey{
		Env:      env,
		Intent:   "bump-revision",
		Params:   MemoParams(intent.Params{Reason: "openssl3 abi"}),
		Portfile: []byte("version 1.7\n"),
	}
}

// counted is a planner stand-in that says how often it actually ran.
// The whole value of the memo is in that number.
func counted(answer error) (func(context.Context) (*plan.Plan, error), *int) {
	runs := 0
	return func(context.Context) (*plan.Plan, error) {
		runs++
		return nil, answer
	}, &runs
}

// The plain case: a miss plans and keeps, a hit replays and does not
// plan. The decline that comes back is the one the first run made,
// sentence and code and exit band alike — the memo is invisible in the
// output and visible only in the work.
func TestMemoizedKeepsAPortfileDeterminedDeclineAndReplaysIt(t *testing.T) {
	ctx := context.Background()
	eng, _, portdir, k := memoFixture(t)
	declined := &plan.Decline{
		Type:   plan.RevisionShapeAmbiguous,
		Detail: "the port evaluates to revision 3 with no revision line to increment",
	}

	run, runs := counted(declined)
	p, err := eng.Memoized(ctx, k, portdir, run)
	assert.Nil(t, p)
	require.ErrorIs(t, err, declined)
	assert.Equal(t, 1, *runs, "a miss plans")

	run, runs = counted(errors.New("this planner must not be reached"))
	p, err = eng.Memoized(ctx, k, portdir, run)
	assert.Nil(t, p)
	assert.Equal(t, 0, *runs, "a hit does not plan")

	var hit *plan.Decline
	require.ErrorAs(t, err, &hit)
	assert.Equal(t, declined.Error(), hit.Error(), "the same sentence")
	assert.Equal(t, declined.Code(), hit.Code(), "the same reason")
	assert.Equal(t, declined.DockhandExit(), hit.DockhandExit(), "the same exit band")
}

// The gate, at the consult point this time: a network-decided decline
// is planned again on every run, forever. Nothing about the memo may
// stand between a re-rolled distfile and the verb that catches it.
func TestMemoizedNeverKeepsANetworkDecidedDecline(t *testing.T) {
	ctx := context.Background()
	eng, repo, portdir, k := memoFixture(t)

	for _, refused := range []*plan.Decline{
		{Type: plan.LatestUnresolved, Detail: "livecheck found nothing"},
		{Type: plan.AlreadyCurrent, Detail: "recorded checksums match what upstream serves",
			Determined: plan.ByNetwork},
	} {
		run, runs := counted(refused)
		_, err := eng.Memoized(ctx, k, portdir, run)
		require.ErrorIs(t, err, refused)
		require.Equal(t, 1, *runs)

		run, runs = counted(refused)
		_, err = eng.Memoized(ctx, k, portdir, run)
		require.ErrorIs(t, err, refused)
		assert.Equal(t, 1, *runs, "%s was remembered", refused.Type.Code())
	}

	shas, err := repo.NotesList(ctx, ledger.PlanNotesRef)
	require.NoError(t, err)
	assert.Empty(t, shas, "and nothing reached the ref at all")
}

// A decline that arrives wrapped had something added to it on the way
// out — an exit band, a resolution's own verdict — and a hit would
// replay the decline alone. So it is not kept.
func TestMemoizedWillNotKeepAWrappedDecline(t *testing.T) {
	ctx := context.Background()
	eng, _, portdir, k := memoFixture(t)
	inner := &plan.Decline{Type: plan.SubportsChanged, Detail: "1 added, 0 removed"}
	wrapped := fmt.Errorf("upstream said so: %w", inner)

	run, runs := counted(wrapped)
	_, err := eng.Memoized(ctx, k, portdir, run)
	require.ErrorIs(t, err, inner)
	require.Equal(t, 1, *runs)

	run, runs = counted(wrapped)
	_, err = eng.Memoized(ctx, k, portdir, run)
	require.ErrorIs(t, err, inner)
	assert.Equal(t, 1, *runs, "the wrapper's own words would have been lost")
}

// A plan is not a decline, and only declines are remembered: there is
// nothing to replay when the answer is work to do.
func TestMemoizedKeepsNothingWhenThePlannerSucceeds(t *testing.T) {
	ctx := context.Background()
	eng, repo, portdir, k := memoFixture(t)
	made := &plan.Plan{Format: plan.Format, Intent: "bump-revision", Port: "jq"}

	for range 2 {
		runs := 0
		p, err := eng.Memoized(ctx, k, portdir, func(context.Context) (*plan.Plan, error) {
			runs++
			return made, nil
		})
		require.NoError(t, err)
		assert.Same(t, made, p)
		assert.Equal(t, 1, runs)
	}
	shas, err := repo.NotesList(ctx, ledger.PlanNotesRef)
	require.NoError(t, err)
	assert.Empty(t, shas)
}

// No environment digest is a missing key, not a degraded one: the memo
// stands aside entirely rather than keying an answer on a gap.
func TestMemoizedStandsAsideWithoutAnEnvironmentDigest(t *testing.T) {
	ctx := context.Background()
	eng, repo, portdir, k := memoFixture(t)
	k.Env = ""
	declined := &plan.Decline{Type: plan.TargetNotReached, Detail: "revision would not become 4"}

	for range 2 {
		run, runs := counted(declined)
		_, err := eng.Memoized(ctx, k, portdir, run)
		require.ErrorIs(t, err, declined)
		assert.Equal(t, 1, *runs)
	}
	shas, err := repo.NotesList(ctx, ledger.PlanNotesRef)
	require.NoError(t, err)
	assert.Empty(t, shas, "an unkeyed memo writes nothing")
}

// A ports tree that is not a checkout — which is what an
// rsync-delivered tree is — has nowhere to keep a memo, and the run
// pays full price without noticing.
func TestMemoizedStandsAsideWithoutARepository(t *testing.T) {
	ctx := context.Background()
	eng, _, portdir, k := memoFixture(t)
	eng.RepoFor = func(context.Context, string) (*git.Repo, error) { return nil, git.ErrNotARepo }
	declined := &plan.Decline{Type: plan.ChecksumsNotLocated, Detail: "no literals"}

	for range 2 {
		run, runs := counted(declined)
		_, err := eng.Memoized(ctx, k, portdir, run)
		require.ErrorIs(t, err, declined)
		assert.Equal(t, 1, *runs)
	}
}

// A portdir outside the repository the run anchored on cannot be named
// relative to it, so it gets no memo rather than a memo under a name
// that means something else.
func TestMemoizedStandsAsideForAPortdirOutsideTheRepository(t *testing.T) {
	ctx := context.Background()
	eng, _, _, k := memoFixture(t)
	declined := &plan.Decline{Type: plan.TransformedStyle, Detail: "perl5 carrier"}

	for range 2 {
		run, runs := counted(declined)
		_, err := eng.Memoized(ctx, k, t.TempDir(), run)
		require.ErrorIs(t, err, declined)
		assert.Equal(t, 1, *runs)
	}
}

// Two ports in one repository do not share an answer, and neither do
// two subports of one Portfile.
func TestMemoizedKeysEachTargetApart(t *testing.T) {
	ctx := context.Background()
	eng, repo, portdir, k := memoFixture(t)
	declined := &plan.Decline{Type: plan.SubportsChanged, Detail: "1 added, 0 removed"}

	run, _ := counted(declined)
	_, err := eng.Memoized(ctx, k, portdir, run)
	require.ErrorIs(t, err, declined)

	sub := k
	sub.Subport = "jq-devel"
	run, runs := counted(declined)
	_, err = eng.Memoized(ctx, sub, portdir, run)
	require.ErrorIs(t, err, declined)
	assert.Equal(t, 1, *runs, "a subport is its own question")

	other := filepath.Join(repo.Root, "sysutils", "jo")
	run, runs = counted(declined)
	_, err = eng.Memoized(ctx, k, other, run)
	require.ErrorIs(t, err, declined)
	assert.Equal(t, 1, *runs, "and so is another portdir")

	// Each of the three is now remembered under its own name, which is
	// what says the keys were kept apart rather than simply not kept.
	for _, at := range []struct {
		portdir string
		key     ledger.MemoKey
	}{{portdir, k}, {portdir, sub}, {other, k}} {
		run, runs = counted(errors.New("this planner must not be reached"))
		_, err = eng.Memoized(ctx, at.key, at.portdir, run)
		var hit *plan.Decline
		require.ErrorAs(t, err, &hit)
		assert.Equal(t, 0, *runs, "%s missed its own memo", at.portdir)
	}
}

// The flags that change what a planner answers are in the key, and
// --recheck is the one that has to be: it reaches an UnexpectedChange
// no plain bump can reach, and that decline's kind is memoizable. Keyed
// without it, a re-derivation's refusal would be replayed at an
// ordinary bump of the same port.
func TestMemoParamsSeparatesTheFlagsThatChangeTheAnswer(t *testing.T) {
	base := intent.Params{Version: "1.8.2"}
	plain := MemoParams(base)

	recheck := base
	recheck.Recheck = true
	assert.NotEqual(t, plain, MemoParams(recheck), "--recheck is a different question")

	version := base
	version.Version = "1.8.3"
	assert.NotEqual(t, plain, MemoParams(version))

	reason := base
	reason.Reason = "openssl3 abi"
	assert.NotEqual(t, plain, MemoParams(reason))

	closes := base
	closes.ClosesTicket = "12345"
	assert.NotEqual(t, plain, MemoParams(closes))

	riders := base
	riders.Riders = intent.RidersNone
	assert.NotEqual(t, plain, MemoParams(riders))

	// What is deliberately not in it: how the port was named, whether
	// the version was resolved or typed, and the run's own discoveries.
	same := base
	same.Target = "sysutils/jq"
	same.Latest = true
	same.Tools = realTools
	same.Dependents = []string{"libjq"}
	assert.Equal(t, plain, MemoParams(same),
		"the same question asked differently is the same question")

	assert.Equal(t, plain, MemoParams(base), "and the rendering is stable")
}

// The rider policies are named, not numbered: the numbers are
// declaration order, and reordering the constants must not silently
// change what every stored key meant.
func TestMemoParamsNamesTheRiderPolicy(t *testing.T) {
	assert.Equal(t, "along", riderWord(intent.RidersAlong))
	assert.Equal(t, "only", riderWord(intent.RidersOnly))
	assert.Equal(t, "none", riderWord(intent.RidersNone))
	assert.Equal(t, "policy-7", riderWord(intent.RiderPolicy(7)), "an unnamed policy misses rather than colliding")
}

// The environment digest refuses a component left blank, and the
// refusal names the memo so a caller can see which of its facts it
// forgot to gather.
func TestMemoEnvRefusesAnIncompleteEnvironment(t *testing.T) {
	_, err := MemoEnv(ledger.Env{MacPorts: "2.11.5"})
	require.ErrorIs(t, err, ledger.ErrEnvIncomplete)
	assert.Contains(t, err.Error(), "memo:")
}
