package engine

// `clean --superseded`, and the ruling underneath it: this is the ONLY
// thing in the tool that removes a branch for having been superseded.
//
// The negative half matters more than the positive one. A supersede is
// dockhand's own inference from two branch names about one port, made
// without asking anybody, and the whole point of the ruling is that no
// pass acts on it — so what has to be proven is that the sweep, the
// report and the machine's publish slot all leave a superseded branch
// exactly where it is.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// supersededPair mints jq twice, which is what writes SupersededBy on
// the older branch: the mint is the one moment where which of two
// branches is the newer is not a guess.
func supersededPair(t *testing.T) (*git.Repo, *Engine, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	errb := &bytes.Buffer{}
	eng := testEngine(t, repo, &verifytest.Fake{}, &bytes.Buffer{}, errb)
	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))
	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.9"),
		Policy{Destination: record.ToBranch}))
	return repo, eng, errb
}

func TestCleanSupersededRemovesTheReplacedBranchAndKeepsTheNewer(t *testing.T) {
	ctx := context.Background()
	repo, eng, _ := supersededPair(t)

	said, err := eng.CleanSuperseded(ctx, repo)
	require.NoError(t, err)
	text := proseText(said)
	assert.Contains(t, text, "removed — superseded by dockhand/jq-1.9")
	assert.Contains(t, text, "discarded dockhand/jq-1.8")

	_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.Error(t, err, "the replaced branch is gone")
	_, err = repo.RevParse(ctx, "dockhand/jq-1.9")
	require.NoError(t, err, "the change now is untouched")
}

// The ruling's negative half. `clean` asks GitHub one question — did the
// pull request merge — and being superseded is not an answer to it.
func TestTheOrdinarySweepNeverRemovesASupersededBranch(t *testing.T) {
	ctx := context.Background()
	repo, eng, _ := supersededPair(t)
	// A forge the sweep can resolve. Neither branch is pushed, so no
	// lookup is made — what is being proven is that the pass walks past a
	// superseded branch, not what GitHub said about one.
	gittest.BareFork(t, repo, "herbygillot", "herby")
	eng.Gh = func(context.Context, ...string) (string, error) { return "[]", nil }

	for _, o := range []ReconcileOpts{{RetireOnly: true}, {}, {Drain: true}} {
		_, err := eng.Reconcile(ctx, o)
		require.NoError(t, err)
		_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
		require.NoError(t, err, "%+v left the superseded branch alone", o)
	}
}

// And a hold stops even the verb that means it. The flag is a person
// saying they want superseded branches gone; a hold is a person saying
// they want THIS one kept, and the more specific instruction wins.
func TestCleanSupersededKeepsAHeldBranchAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	repo, eng, _ := supersededPair(t)
	old, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	require.NoError(t, eng.Hold(ctx, repo, "dockhand/jq-1.8", "keeping it for a bisect", holdAt))

	said, err := eng.CleanSuperseded(ctx, repo)
	require.NoError(t, err)
	assert.Contains(t, proseText(said), "keeping it for a bisect")
	assert.Contains(t, proseText(said), "kept —")

	at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, old, at)
}

// The fork copy is left alone. A superseded branch may have been
// promoted, its copy may back a pull request somebody is reading, and
// deleting the copy closes that pull request — which is ring 3, and not
// this verb's to spend.
func TestCleanSupersededLeavesTheForkCopyStanding(t *testing.T) {
	ctx := context.Background()
	repo, eng, _ := supersededPair(t)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))

	said, err := eng.CleanSuperseded(ctx, repo)
	require.NoError(t, err)
	text := proseText(said)
	assert.Contains(t, text, "the fork copy on \"herby\" is untouched")
	assert.NotContains(t, text, "removed dockhand/jq-1.8 from")
}

// A namespace with nothing superseded in it says so rather than saying
// nothing: a sweep that printed an empty answer reads as a sweep that
// did not run.
func TestCleanSupersededSaysSoWhenThereIsNothingToRemove(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	require.NoError(t, ledger.Open(repo).Write(ctx, mintedNote(t, repo, sha)))

	said, err := testState(t, repo, nil).CleanSuperseded(ctx, repo)
	require.NoError(t, err)
	assert.Contains(t, proseText(said), "no superseded branches removed")
}

// Every line the sweep produces about a branch it kept or removed is on
// stdout, so `clean --superseded | grep removed` is a thing a person can
// write. Only the demolition's own advisories go to stderr.
func TestCleanSupersededPutsItsRowsOnStdout(t *testing.T) {
	ctx := context.Background()
	repo, eng, _ := supersededPair(t)
	said, err := eng.CleanSuperseded(ctx, repo)
	require.NoError(t, err)

	var rows int
	for _, l := range said {
		if strings.Contains(l.Text, "superseded by") {
			rows++
			assert.Equal(t, render.ToOut, l.Stream)
		}
	}
	assert.Equal(t, 1, rows)
}

// THE RULING's last unguarded road: the drain.
//
// A supersede marks the older branch's record and cancels nothing — it
// happens at ANOTHER branch's mint, and SupersedeStale only stops runs
// whose own branch moved — so a replaced branch keeps whatever runs it
// had queued. The publish slot already skipped it and the sweep already
// left it alone; the pump did not, so `bump jq --to 1.8` followed by
// `bump jq --to 1.9` left the machine spending a VM slot and an hour on
// a change a newer branch had already replaced, every pass, forever.
func TestTheDrainNeverStartsASupersededBranchsQueuedRun(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Destination = record.ToVerdict
	n.SupersededBy = "dockhand/jq-1.9"
	started(&n, "Testos", "", record.Run{State: record.Queued, Detail: "no slot free"})
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	errb := &bytes.Buffer{}
	eng := testEngine(t, repo, fake, &bytes.Buffer{}, errb)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	assert.Empty(t, fake.Submitted, "a replaced change takes no slot")
	// Silent, like the destination skip and unlike the hold: a supersede
	// is a fact the branch's own status line already states, and a pass
	// reprinting it every ten minutes would be noise about a decision
	// nobody has to make.
	assert.Empty(t, errb.String())

	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, runOf(again, "Testos").State,
		"the run is left as it was: nothing else touches a superseded branch")
}

// And again under the submit lock, for the same reason the hold is
// asked twice: the lock is held across a re-read precisely so a peer's
// write between the walk and the submit is honoured, and a sibling
// minted in that window is exactly such a write.
func TestASupersedeLandingMidPassIsHonouredUnderTheLock(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Destination = record.ToVerdict
	started(&n, "Testos", "", record.Run{State: record.Queued, Detail: "no slot free"})
	// The note the walk read named no sibling; the note under the lock
	// does.
	n.SupersededBy = "dockhand/jq-1.9"
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	eng := testEngine(t, repo, fake, &bytes.Buffer{}, &bytes.Buffer{})
	stop := eng.pumpRun(ctx, repo, "dockhand/jq-1.8", sha,
		[]Member{{Port: "jq", Portdir: "sysutils/jq"}},
		runRef{Port: "jq", Portdir: "sysutils/jq", Release: "Testos"},
		platform.Release{Name: "Testos", Darwin: 99})

	assert.False(t, stop, "one replaced branch does not end the pass for every other one")
	assert.Empty(t, fake.Submitted, "the re-read is what stops it, and it stopped it")
}
