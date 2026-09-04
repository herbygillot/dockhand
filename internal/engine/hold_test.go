package engine

// Holds: the verbs that write one, the auto-hold a prerelease target
// earns at birth, and the roads that obey it.
//
// The roads are what these tests are really about. A hold that only the
// verb knew about would be a note somebody wrote and nothing read, and
// each of them is a place where the code's natural shape is to act.

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// holdAt is the moment a test's hold went on, pinned so a refusal's
// sentence is the same twice.
var holdAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// heldRepo is engineRepo with the branch's record already holding.
func heldRepo(t *testing.T, reason string) (*git.Repo, string) {
	t.Helper()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Hold = &record.Hold{Reason: reason, At: holdAt}
	require.NoError(t, ledger.Open(repo).Write(context.Background(), n))
	return repo, sha
}

func TestHoldWritesTheReasonAndUnholdTakesItOff(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	require.NoError(t, ledger.Open(repo).Write(ctx, mintedNote(t, repo, sha)))
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	eng := testEngine(t, repo, nil, out, errb)

	require.NoError(t, eng.Hold(ctx, repo, "jq", "waiting on upstream", holdAt))
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	require.NotNil(t, n.Hold)
	assert.Equal(t, "waiting on upstream", n.Hold.Reason)
	assert.Equal(t, holdAt, n.Hold.At.UTC(), "the clock read is the caller's, not record's")
	assert.Contains(t, out.String(), "held dockhand/jq-1.8: waiting on upstream")

	require.NoError(t, eng.Unhold(ctx, repo, "jq"))
	n, err = ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Nil(t, n.Hold, "released, not emptied: the pointer is the whole distinction")
}

// A second hold does not silently rewrite the sentence the first one
// left. The reason is a person's account of why the change stops, and
// overwriting it loses the account with nothing said.
func TestHoldingAHeldBranchIsRefusedAndKeepsTheFirstReason(t *testing.T) {
	ctx := context.Background()
	repo, sha := heldRepo(t, "waiting on upstream")
	eng := testState(t, repo, nil)

	err := eng.Hold(ctx, repo, "jq", "a different reason", holdAt.Add(time.Hour))
	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.Equal(t, exitcode.Held, held.DockhandExit())
	assert.Equal(t, "held", held.Code())
	assert.Contains(t, err.Error(), "waiting on upstream")

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "waiting on upstream", n.Hold.Reason)
	assert.Equal(t, holdAt, n.Hold.At.UTC())
}

// Releasing a hold that was never placed is refused rather than reported
// as a success, so a script cannot read "the hold is lifted" out of a
// branch nothing was holding.
func TestUnholdingAnUnheldBranchIsADecline(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	require.NoError(t, ledger.Open(repo).Write(ctx, mintedNote(t, repo, sha)))

	err := testState(t, repo, nil).Unhold(ctx, repo, "jq")
	var not *NotHeldError
	require.ErrorAs(t, err, &not)
	assert.Equal(t, exitcode.PlanDeclined, not.DockhandExit())
	assert.Equal(t, "not-held", not.Code())
}

// A hold with no reason still holds, and says the absence rather than
// rendering it as an empty clause. That is the whole point of the field
// being a pointer.
func TestAHoldWithNoReasonIsStillAHold(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	require.NoError(t, ledger.Open(repo).Write(ctx, mintedNote(t, repo, sha)))
	eng := testState(t, repo, nil)

	require.NoError(t, eng.Hold(ctx, repo, "jq", "", holdAt))
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	require.NotNil(t, n.Hold)

	err = GateHold(n, "dockhand/jq-1.8", "the publication")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reason given")
}

// A hold refuses a PERSON. Every other gate in its neighbourhood is nil
// for a human by construction — a human standing there is the whole
// argument for the looser rule — and this one has the opposite argument:
// the commonest use is somebody stopping themselves, and a hold that
// `dockhand promote` walked past would be a note nobody had to obey.
//
// And it refuses before the network: a held branch must cost no gh round
// trip and no push.
func TestAHoldRefusesThePublicationAndSpendsNoForgeCall(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Hold = &record.Hold{Reason: "waiting on upstream", At: holdAt}
	started(&n, "Testos", "fake-1", record.Run{State: record.Passed, Linted: true, Evidence: "built"})
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	var asked [][]string
	eng := testState(t, repo, nil)
	eng.Gh = func(_ context.Context, args ...string) (string, error) {
		asked = append(asked, args)
		return "", nil
	}
	err := eng.Promote(ctx, repo, "jq", PromoteOpts{})
	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.Equal(t, "the publication", held.Withheld)
	assert.Empty(t, asked, "a held branch is refused before ForkRemote, the lookups and the push")
	assert.Empty(t, repo.TrackedRemote(ctx, "dockhand/jq-1.8"), "and nothing was pushed")
}

// The pump must not spend a slot and an hour of the machine on a change
// somebody said stops.
func TestAHoldStopsTheDrainFromStartingAnything(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Destination = record.ToVerdict
	started(&n, "Testos", "", record.Run{State: record.Queued, Detail: "no slot free"})
	n.Hold = &record.Hold{Reason: "waiting on upstream", At: holdAt}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	eng := testEngine(t, repo, fake, out, errb)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	assert.Empty(t, fake.Submitted, "a held change takes no slot")
	assert.Contains(t, errb.String(), "the verification is withheld")

	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, runOf(again, "Testos").State,
		"the run is left as it was: a hold is about what happens next")
}

// The authoritative check is the one under the submit lock, not the one
// in the walk. The pump holds that lock across a re-read precisely so a
// peer's write between the walk and the submit is honoured, and somebody
// running `dockhand hold` while a status pass walks the namespace is the
// ordinary way a hold lands in that window.
//
// Driven at pumpRun directly, because the window cannot be opened from
// outside without timing: the walk's own check would answer first.
func TestAHoldPlacedDuringThePassIsHonouredUnderTheLock(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := mintedNote(t, repo, sha)
	n.Destination = record.ToVerdict
	started(&n, "Testos", "", record.Run{State: record.Queued, Detail: "no slot free"})
	// The note the walk read had no hold; the note under the lock does.
	n.Hold = &record.Hold{Reason: "held mid-pass", At: holdAt}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	eng := testEngine(t, repo, fake, out, errb)
	stop := eng.pumpRun(ctx, repo, "dockhand/jq-1.8", sha,
		[]Member{{Port: "jq", Portdir: "sysutils/jq"}},
		runRef{Port: "jq", Portdir: "sysutils/jq", Release: "Testos"},
		platform.Release{Name: "Testos", Darwin: 99})

	assert.False(t, stop, "one held branch does not end the pass for every other one")
	assert.Empty(t, fake.Submitted, "the re-read is what stops it, and it stopped it")
	assert.Contains(t, errb.String(), "held mid-pass")
}

// The hold withholds the DELETION and leaves the verdict alone — the
// same shape --no-clean already has. The audit row still closes, because
// a merge is the change's outcome whether or not the branch survived it.
func TestAHoldWithholdsTheDeletionWithoutChangingTheVerdict(t *testing.T) {
	ctx := context.Background()
	repo, sha := heldRepo(t, "keeping the branch for a bisect")
	gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))

	eng := testState(t, repo, nil)
	eng.Gh = mergedPRGh("herbygillot", 91)
	// A publication row to close. Settle is a no-op over a mint sha this
	// dockhand never published, so without one the audit half of this
	// test would pass for the wrong reason.
	require.NoError(t, eng.Publish(ctx, repo, Publication{MintSha: sha, Branch: "dockhand/jq-1.8",
		Port: "jq", Target: "1.8", PRNumber: 91, Invoker: record.Human}))

	rep, err := eng.Reconcile(ctx, ReconcileOpts{})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	assert.True(t, b.Retire.PR.Merged, "the verdict is unchanged: GitHub says it merged")
	assert.False(t, b.Retire.Cleaned, "and the branch is still here")
	assert.Contains(t, proseText(b.Prose), "the deletion is withheld")

	_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err, "the branch survived the pass")

	// The bookkeeping happened anyway. A hold that also stopped the audit
	// would make held changes disappear from the one record that counts
	// merges.
	rows, err := ledger.Open(repo).Outcomes(ctx, sha)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	assert.Equal(t, record.Merged, rows[len(rows)-1].Outcome)
}

// The sweep obeys it too, and on the same terms. `clean` and the report
// reach one verdict by one code path; a hold that only `status` honoured
// would be the split the reconciler exists to have ended.
func TestTheSweepObeysAHoldToo(t *testing.T) {
	ctx := context.Background()
	repo, _ := heldRepo(t, "keeping the branch for a bisect")
	gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))

	eng := testState(t, repo, nil)
	eng.Gh = mergedPRGh("herbygillot", 91)
	rep, err := eng.Reconcile(ctx, ReconcileOpts{RetireOnly: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.False(t, rep.Branches[0].Retire.Cleaned)
	assert.Contains(t, proseText(rep.Branches[0].Prose), "the deletion is withheld")
	_, err = repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
}

// A change minted against a prerelease is born held. The intents will
// plan one — asking for an rc by name is legitimate — but nothing
// carries it onward on its own.
func TestAPrereleaseTargetIsHeldAtMint(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		target string
		held   bool
	}{
		{"1.9", false},
		{"2.0-rc1", true},
		{"3.0.0-beta", true},
		{"2026.9.1-pr5150.5", true},
		{"1.8.1", false},
	} {
		repo := gittest.PortsTree(t, realTools)
		eng := testState(t, repo, &verifytest.Fake{})
		require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", c.target),
			Policy{Destination: record.ToBranch}))

		tip, err := repo.RevParse(ctx, "dockhand/jq-"+c.target)
		require.NoError(t, err)
		n, err := ledger.Open(repo).Read(ctx, tip)
		require.NoError(t, err)
		if !c.held {
			assert.Nil(t, n.Hold, "%s is a release", c.target)
			continue
		}
		require.NotNil(t, n.Hold, "%s is prerelease-style", c.target)
		assert.Contains(t, n.Hold.Reason, c.target)
		assert.False(t, n.Hold.At.IsZero(), "the moment is recorded with the reason")
	}
}

// And the auto-hold SAYS SO, which is the half a person who did not ask
// for it needs most.
//
// `dockhand hold` announces itself and so does `unhold`; the one hold
// nobody typed was the one placed in silence, so `bump jq --to 2.0-rc1`
// looked entirely ordinary and the news arrived days later as an exit 23
// over a hold nobody remembered placing. The line names the reason and
// the verb that releases it, and it names what the hold does NOT stop —
// the verification this same invocation asked for still runs, which is
// the half a reader would otherwise get wrong.
func TestThePrereleaseHoldIsAnnouncedWhenItIsPlaced(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	eng := testEngine(t, repo, &verifytest.Fake{}, out, errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "2.0-rc1"),
		Policy{Destination: record.ToBranch}))

	said := errb.String()
	assert.Contains(t, said, "held at mint:")
	assert.Contains(t, said, "2.0-rc1 is prerelease-style",
		"the reason is quoted, not paraphrased")
	assert.Contains(t, said, "`dockhand unhold dockhand/jq-2.0-rc1` releases it",
		"and the verb that releases it is named with the branch")
	assert.Contains(t, said, "the verification this invocation asked for still runs")
	assert.NotContains(t, out.String(), "held at mint",
		"the hold is prose about acting, and prose goes to stderr")
}

// An ordinary release says nothing, because nothing was held. A line
// about a hold that was not placed would be worse than silence.
func TestAnOrdinaryMintAnnouncesNoHold(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	eng := testEngine(t, repo, &verifytest.Fake{}, out, errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.9"),
		Policy{Destination: record.ToBranch}))
	assert.NotContains(t, errb.String(), "held at mint")
}

// The auto-hold does not refuse the verification the same invocation
// asked for. A person who typed the verb asked for that build, and a
// hold that refused to test a change dockhand had just written for them
// would be answering a question nobody put. What it governs is what
// happens with nobody there.
func TestTheMintHoldStillLetsTheAskedForVerificationRun(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "2.0-rc1"),
		Policy{Destination: record.ToVerdict, On: platform.Release{Name: "Testos", Darwin: 99}}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-2.0-rc1")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	require.NotNil(t, n.Hold, "the change is held")
	assert.NotEmpty(t, fake.Submitted, "and the build the person asked for still ran")
}

// The hold survives a second commit. Extend already carried the field;
// what this pins is that the auto-hold reaches the same road, so a
// prerelease branch cannot be released by adding a fixup to it.
func TestAHoldIsNotShedByGrowingTheBranch(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	eng := testState(t, repo, &verifytest.Fake{})
	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "2.0-rc1"),
		Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-2.0-rc1")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	require.NotNil(t, n.Hold)

	grown, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch:      "dockhand/jq-2.0-rc1",
		ExpectedTip: tip,
		Commit:      oneFile("sysutils/jq/Portfile", "version 2.0-rc1\n# fixup\n", "jq: fix the checksum"),
	})
	require.NoError(t, err)
	after, err := ledger.Open(repo).Read(ctx, grown)
	require.NoError(t, err)
	require.NotNil(t, after.Hold, "a hold a person or a mint placed is not shed by a commit")
	assert.Equal(t, n.Hold.Reason, after.Hold.Reason)
}

// mergedPRGh answers the head-ref lookup with one merged pull request
// and the login query with a name — the two calls a retirement makes.
func mergedPRGh(login string, number int) func(context.Context, ...string) (string, error) {
	return func(_ context.Context, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "api" && args[1] == "user":
			return login + "\n", nil
		case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "/pulls?head="):
			return `[{"number":` + strconv.Itoa(number) + `,"title":"jq: update to 1.8","state":"closed",` +
				`"merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/pull"}]`, nil
		}
		return "", nil
	}
}

// proseText flattens a branch's prose for a contains assertion.
func proseText(lines []render.Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}
