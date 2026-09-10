package app

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

var (
	tools = tool.NewFinder(nil)
	clock = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
)

func now() time.Time { return clock }

func me() record.OwnerID {
	return record.OwnerID{Root: "/w/ports", Host: "mac", PID: 100, Since: clock}
}

// fixture is a two-portdir ports tree. It is gittest.Init rather than
// gittest.PortsTree because a sweep needs two targets and git.GraftTree
// writes into a tree that already has the parent directory — measured:
// grafting sysutils/oniguruma/Portfile into a tree holding only
// sysutils/jq answers `no entry "oniguruma" in tree`.
func fixture(t *testing.T) (*git.Repo, *statestore.Store) {
	t.Helper()
	repo := gittest.Init(t, tools, "", map[string]string{
		"sysutils/jq/Portfile":     "version 1.7\n",
		"devel/oniguruma/Portfile": "version 6.8\n",
	})
	return repo, statestore.Open(repo)
}

// portdirs is where each fixture port lives, so a Prepared names the
// directory the tree actually has.
var portdirs = map[string]string{"jq": "sysutils/jq", "oniguruma": "devel/oniguruma"}

// preparedBump is one target's content as the pool would deliver it. It
// is a literal rather than a change.Prepare over a plan, because what
// these tests are about is the ORDER of the writes an operation makes,
// and a preparation that needed an evaluator would prove something about
// change instead.
func preparedBump(t *testing.T, repo *git.Repo, port, version string) change.Prepared {
	t.Helper()
	base, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	at, err := repo.CommittedAt(t.Context(), base)
	require.NoError(t, err)
	dir := portdirs[port]
	return change.Prepared{
		Portdir: change.TreePath(dir),
		Subjects: []record.Subject{{
			Port: port, Names: []string{port}, Portdir: dir,
			Intent: "bump", Target: version,
		}},
		Files:   []change.File{{Path: "Portfile", Content: []byte("version " + version + "\n")}},
		Base:    record.Base{Sha: base, CommittedAt: at},
		Intent:  "bump",
		Summary: port + ": update to " + version,
	}
}

func changeOp(repo *git.Repo, st *statestore.Store) Change {
	return Change{Repo: repo, State: st, Me: me(), Now: now}
}

// A --no-verify BUMP WRITES THE RECORD AND CREATES THE BRANCH IN ONE
// BATCH, and enqueues nothing. The Ref comes back from a Resolve after
// the Amend, which is the design's claim that app holds no Ref it did
// not resolve: if the batch had not created the branch, this fails here
// rather than by inspection.
func TestChangeMintsRecordAndBranchTogetherUnderNoVerify(t *testing.T) {
	repo, st := fixture(t)
	res, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"),
		Delivery: Branch,
		Slug:     "jq-1.8",
	})
	require.NoError(t, err)
	assert.Equal(t, Minted, res.Did)
	assert.Equal(t, 0, res.Exit(), "a branch that exists is success, verified or not")
	assert.Equal(t, "dockhand/jq-1.8", res.Ref.Branch())
	assert.Empty(t, res.Attempt, "--no-verify enqueues nothing")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, s.Changes, 1)
	require.Empty(t, s.Attempts)
	assert.True(t, repo.HasBranch(t.Context(), "dockhand/jq-1.8"))
}

// A HOST WITH NO PROVIDER MINTS AND SAYS UNVERIFIED. The presence check
// runs BEFORE the mint Amend, so nothing is enqueued and no permanently
// Queued attempt is left behind; the exit is 0 with an advisory, not 61.
func TestChangeWithNoProviderMintsWithoutEnqueueing(t *testing.T) {
	repo, st := fixture(t)
	res, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"),
		Delivery: Enqueue,
		Platform: platform.Releases[0],
		Slug:     "jq-1.8",
	})
	require.NoError(t, err)
	assert.Equal(t, Minted, res.Did)
	require.NotNil(t, res.Deferred)
	assert.Equal(t, NoProvider, res.Deferred.Reason)
	assert.Equal(t, 0, res.Exit(), "an unverified branch is an advisory, not a pending state")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, s.Attempts, "a draft left a permanently Queued attempt here")
}

// A SECOND BUMP OF ONE PORT UNDER Refuse IS EXIT 11 AND WRITES NOTHING.
func TestChangeRefusesAStandingBranch(t *testing.T) {
	repo, st := fixture(t)
	op := changeOp(repo, st)
	_, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	_, err = op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.9"), Delivery: Branch, Slug: "jq-1.9",
	})
	require.ErrorIs(t, err, change.ErrInFlight)

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Len(t, s.Changes, 1, "the refusal is before the commit; nothing was written")
}

// --replace SUPERSEDES THE OLD CHANGE, WITHDRAWS ITS QUEUED WORK AND
// DELETES ITS BRANCH IN THE SAME BATCH THAT CREATES THE NEW ONE. This is
// the one order the design writes out, so it is the one a test pins.
func TestChangeReplaceSupersedesInTheMintAmend(t *testing.T) {
	repo, st := fixture(t)
	op := changeOp(repo, st)
	first, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	second, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.9"), Delivery: Branch, Slug: "jq-1.9",
		Replace: Replace,
	})
	require.NoError(t, err)
	assert.Equal(t, Minted, second.Did)

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	old := s.Changes[string(first.Ref.ID())]
	assert.Equal(t, record.ChangeSuperseded, old.State)
	assert.False(t, old.Bound(), "the name is released by Bound(), not by clearing the field")
	assert.Equal(t, "dockhand/jq-1.8", old.Branch, "a closed record keeps the name it had")
	assert.False(t, repo.HasBranch(t.Context(), "dockhand/jq-1.8"), "the delete line rode in the mint's batch")
	assert.True(t, repo.HasBranch(t.Context(), "dockhand/jq-1.9"))
}

// DISCARD CLOSES THE RECORD AND DELETES THE BRANCH IN ONE AMEND, on a
// host with no provider at all: nothing is held, so nothing needs one.
func TestDiscardClosesAndDemolishesWithNoProvider(t *testing.T) {
	repo, st := fixture(t)
	minted, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	d := Discard{Repo: repo, State: st, Me: me(), Now: now, Invoker: record.Human}
	res, err := d.Run(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, minted.Ref.ID(), res.Closed)
	assert.Equal(t, []string{change.BranchRef("dockhand/jq-1.8")}, res.Deleted)

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, record.ChangeDiscarded, s.Changes[string(minted.Ref.ID())].State)
	assert.False(t, repo.HasBranch(t.Context(), "dockhand/jq-1.8"))
}

// A MACHINE NEVER DEMOLISHES AN ADOPTED CHANGE, and the refusal comes
// before anything is stopped.
func TestDiscardMachineRoadRefusesAnAdoptedChange(t *testing.T) {
	d := Discard{Invoker: record.Machine}
	_, err := d.run(context.Background(), record.Change{ID: "chg-1", MintedVia: record.MintedAdopted}, nil)
	require.ErrorIs(t, err, ErrMachineMayNotDemolish)
}

// A SWEEP COMMITS AS IT GOES AND WITHHOLDS THE TARGET THAT WOULD PASS
// THE CAP — with no record, no branch and no attempt for it, and a typed
// reason rather than a sentence.
func TestSurveyAdmitsPerTargetAndWithholdsTheRest(t *testing.T) {
	repo, st := fixture(t)
	planned := []Planned{
		{Target: "sysutils/jq", Prepared: preparedBump(t, repo, "jq", "1.8"), Slug: "jq-1.8"},
		{Target: "devel/oniguruma", Prepared: preparedBump(t, repo, "oniguruma", "6.9"), Slug: "oniguruma-6.9"},
	}
	i := 0
	s := Survey{Repo: repo, State: st, Me: me(), Now: now}
	sw, err := s.Run(t.Context(), SurveyRequest{
		Next: func(context.Context) (Planned, bool) {
			if i >= len(planned) {
				return Planned{}, false
			}
			i++
			return planned[i-1], true
		},
		Delivery:  Branch,
		Admission: run.Admission{Set: true, MaxQueued: 10, MaxPerPass: 1},
		InFlight:  Advance,
	})
	require.NoError(t, err)
	require.Len(t, sw.Rows, 2)
	assert.Equal(t, Minted, sw.Rows[0].Result.Did)
	require.NotNil(t, sw.Rows[1].Withheld, "the second target is over MaxPerPass")
	assert.Equal(t, run.OverPassCap, sw.Rows[1].Withheld.Why)
	assert.Equal(t, 0, sw.Exit(), "a withheld target is not a pending attempt")

	state, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Len(t, state.Changes, 1, "a withheld target has no record")
	assert.False(t, repo.HasBranch(t.Context(), "dockhand/oniguruma-6.9"))
}

// A SWEEP MEETING ITS OWN STANDING BRANCH LEAVES IT ALONE. That is what
// resume-by-rerun is: the row stands, nothing is written twice.
func TestSurveyAdvancesPastItsOwnStandingBranch(t *testing.T) {
	repo, st := fixture(t)
	_, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	i := 0
	s := Survey{Repo: repo, State: st, Me: me(), Now: now}
	sw, err := s.Run(t.Context(), SurveyRequest{
		Next: func(context.Context) (Planned, bool) {
			if i > 0 {
				return Planned{}, false
			}
			i++
			return Planned{Target: "sysutils/jq", Prepared: preparedBump(t, repo, "jq", "1.9"), Slug: "jq-1.9"}, true
		},
		Delivery:  Branch,
		Admission: run.Admission{Set: true, MaxQueued: 10, MaxPerPass: 10},
		InFlight:  Advance,
	})
	require.NoError(t, err)
	require.Len(t, sw.Rows, 1)
	assert.Equal(t, Stood, sw.Rows[0].Result.Did)
	assert.False(t, repo.HasBranch(t.Context(), "dockhand/jq-1.9"))
}

// THE ENQUEUE'S ROSTER MUST EQUAL THE DRAIN'S, or a SpecID computed at
// enqueue addresses an attempt no re-derivation can find. This is the
// property Adoptable depends on and the one the sketch's Spec literal —
// content, platform and the asks, with no roster — would have broken.
func TestRosterOfAgreesWithRunRosterForAFreshMint(t *testing.T) {
	subjects := []record.Subject{
		{Port: "jq", Names: []string{"jq"}},
		{Port: "py-foo", Names: []string{"py312-foo", "py313-foo"}},
	}
	c := record.Change{ID: "chg-1", Subjects: subjects}
	members, withheld := run.Roster(c, record.Attempt{})
	assert.Equal(t, members, rosterOf(subjects))
	assert.Empty(t, withheld)

	enqueued := run.Spec{Content: "tree-1", Roster: rosterOf(subjects)}
	drained := run.Spec{Content: "tree-1", Roster: members}
	assert.Equal(t, enqueued.ID(), drained.ID(), "the queue carries an identity a later process can re-derive")
}

// THE EXIT TABLE IS THE ONE PLACE A RESULT BECOMES A CODE.
func TestResultExitBands(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Result
		want int
	}{
		{"a started build has no verdict yet", Result{Did: Started}, 0},
		{"a minted branch stands", Result{Did: Minted}, 0},
		{"an unverified branch is an advisory", Result{Did: Minted, Deferred: &Deferral{Reason: NoProvider}}, 0},
		{"a queued attempt is pending", Result{Did: Queued}, 60},
		{"a full machine is nobody's problem", Result{Did: Queued, Deferred: nil}, 60},
		{"no environment is provisioning's", Result{Did: Queued, Deferred: &Deferral{Reason: NoEnvironment}}, 61},
		{"a provider that errored is still pending", Result{Did: Queued, Deferred: &Deferral{Reason: ProviderError}}, 60},
		{"a pass", Result{Did: Stood, Verdict: record.Passed}, 0},
		{"a failure", Result{Did: Stood, Verdict: record.Failed}, 70},
		{"blocked behind a sibling", Result{Did: Stood, Verdict: record.Blocked}, 71},
		{"unsupported", Result{Did: Stood, Verdict: record.Unsupported}, 72},
		{"canceled ends without concluding", Result{Did: Stood, Verdict: record.Canceled}, 73},
		{"an environment that could not answer", Result{Did: Stood, Verdict: record.Errored}, 73},
		// APART FROM 73, because the remedy is not 73's. Nothing is wrong
		// with this machine, and a script that reads a fault as an
		// environment problem retries forever against a guest that was
		// never the problem.
		{"dockhand's own tooling did not answer", Result{Did: Stood, Verdict: record.Faulted}, 74},
		{"nothing realized", Result{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.res.Exit())
		})
	}
}

// A SWEEP'S PARTITION IS ITS OWN: declines and verdicts are quiet, a
// hard error is 83, and a queued attempt is 60.
func TestSweepExitIsItsOwnPartition(t *testing.T) {
	quiet := Sweep{Rows: []Row{{Decline: errors.New("already current")}, {Result: Result{Did: Stood, Verdict: record.Failed}}}}
	assert.Equal(t, 0, quiet.Exit())

	pending := Sweep{Rows: []Row{{Result: Result{Did: Queued}}}}
	assert.Equal(t, 60, pending.Exit())

	hard := Sweep{Rows: []Row{{Result: Result{Did: Queued}}, {Hard: errors.New("boom")}}}
	assert.Equal(t, 83, hard.Exit(), "a hard error outranks a pending one")
}

// ATTENTION IS AN ALLOW-LIST OF THE QUIET FAMILIES. A refusal nobody
// argued into the list is loud, which is the failure mode this shape is
// chosen for.
func TestPassAttentionIsAnAllowList(t *testing.T) {
	peer := Pass{Refusals: []Refusal{{Err: change.ErrNotBound}}}
	assert.False(t, peer.Attention())
	assert.Equal(t, 0, peer.Exit())

	held := Pass{Refusals: []Refusal{{Err: ErrMachineMayNotDemolish}}}
	assert.False(t, held.Attention())

	novel := Pass{Refusals: []Refusal{{Err: errors.New("something new")}}}
	assert.True(t, novel.Attention())
	assert.Equal(t, 84, novel.Exit())
}

// A COHORT'S VERDICT IS THE WORST ITS MEMBERS EARNED, and an attempt
// that settled with no runs at all is Errored rather than Passed.
func TestVerdictOfIsWorstFirstAndNeverGuessesAPass(t *testing.T) {
	assert.Equal(t, record.Errored, verdictOf(record.Attempt{}))

	mixed := record.Attempt{Runs: map[string]record.Run{
		"jq":    {State: record.Passed},
		"onig":  {State: record.Blocked},
		"other": {State: record.Failed},
	}}
	assert.Equal(t, record.Failed, verdictOf(mixed))

	withheld := record.Attempt{Runs: map[string]record.Run{
		"jq":   {State: record.Passed},
		"held": {State: record.Withheld},
	}}
	assert.Equal(t, record.Passed, verdictOf(withheld), "a member kept out of the roster says nothing about the change")
}

// THE MACHINE'S TWO REFUSALS, and a person passing both.
func TestMayDemolish(t *testing.T) {
	adopted := record.Change{MintedVia: record.MintedAdopted}
	assert.False(t, mayDemolish(adopted, record.Machine))
	assert.True(t, mayDemolish(adopted, record.Human), "a person may discard what they adopted")

	person := record.Change{Hold: &record.Hold{Origin: record.HoldPerson}}
	assert.False(t, mayDemolish(person, record.Human), "a person's hold withholds every act for every invoker")

	crossing := record.Change{Hold: &record.Hold{Origin: record.HoldCrossing}}
	assert.False(t, mayDemolish(crossing, record.Machine))
	assert.True(t, mayDemolish(crossing, record.Human))
}

// A BRANCHLESS RECORD IS NAMED BY ITS PIN, because a port may carry a
// snapshot and a branch change at once.
func TestResolveTargetNamesAPinnedRecordByItsPin(t *testing.T) {
	assert.Equal(t, "dockhand/jq-1.8", change.TargetFor(record.Change{Branch: "dockhand/jq-1.8"}))
	assert.Equal(t, change.PinRef("chg-1"), change.TargetFor(record.Change{ID: "chg-1"}))
}

// NEEDS IS COMPUTED FROM THE WHOLE REQUEST: a verifier only where an
// attempt may start, a forge only where a publication may open.
func TestNeedsFollowsTheDelivery(t *testing.T) {
	doc := ChangeRequest{Delivery: Document}.Needs()
	assert.False(t, doc.Verifier)
	assert.False(t, doc.Forge)
	assert.True(t, doc.Repo, "even --plan reads the base commit's Portfile")

	queued := ChangeRequest{Delivery: Enqueue, Fetches: true}.Needs()
	assert.True(t, queued.Verifier)
	assert.False(t, queued.Forge)
	assert.True(t, queued.Fetcher, "whether the network is read is the intent's fact, not the delivery's")

	pr := ChangeRequest{Delivery: PullRequest}.Needs()
	assert.True(t, pr.Forge)
}

// THE ZERO RESIDENCY IS UNKNOWN, not "nobody is resident": a process
// that never probed must not appoint itself the judge of somebody else's
// job (rule 7).
func TestZeroResidencyIsUnknown(t *testing.T) {
	var r Residency
	assert.Equal(t, ResidencyUnknown, r.State)
}

// A CHANGE BOUND FOR A PULL REQUEST SAYS SO ON THE RECORD, which is what
// the machine slot reads to know a change is its business at all.
//
// AND THE OTHER TWO ARE NOT ONE DESTINATION. They were — every delivery
// but --to-pr recorded ToBranch — which made ToBranch disagree with its
// own doc ("ToBranch is --no-verify"), and a reader took the doc at its
// word: publish.unrunCause told reviewers a branch had been minted with
// a flag nobody typed, and shadowed the honest sentence for the case it
// had actually met.
//
// The three are the tool's own verbs: bump stops at a branch, verify
// stops at a verdict, promote goes all the way.
func TestDestinationIsWrittenAtMint(t *testing.T) {
	assert.Equal(t, record.ToPublished, destination(PullRequest), "--to-pr")
	assert.Equal(t, record.ToBranch, destination(Branch), "--no-verify stops at the branch")
	assert.Equal(t, record.ToVerdict, destination(Enqueue), "and the default asks for a verdict")
}

// A SUPERSEDED CHANGE IS NOT A CLOSED ONE, and discard must close it.
//
// The guard here first asked Bound(), which is false for TWO reasons —
// "the record is closed" and "a newer sibling superseded it while its
// publication stayed open" — where only the first is a record with
// nothing left to close. So `discard` on a superseded change printed
// "discarded <id>", exited 0, and left the record MINTED: standing in
// every listing as an open change whose branch no longer existed, with
// nothing but `purge` able to clear it. That is the exact defect the
// guard was added to fix, reintroduced by reaching for the nearest
// predicate rather than the one the reasoning names.
func TestDiscardClosesASupersededChange(t *testing.T) {
	repo, st := fixture(t)
	first, err := changeOp(repo, st).Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"), Delivery: Branch, Slug: "jq-1.8",
	})
	require.NoError(t, err)

	// The shape a --replace leaves when the old change's PUBLICATION IS
	// STILL OPEN: supersedeIn releases the name and leaves the record
	// minted, where a --replace with nothing published closes it outright.
	// Planted directly, because reproducing it through the operation
	// would mean standing up a live pull request.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		cur := tx.State().Changes[string(first.Ref.ID())]
		cur.SupersededBy = "dockhand/jq-1.9"
		tx.PutChange(cur)
		return nil
	}))

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	old := s.Changes[string(first.Ref.ID())]
	require.NotEmpty(t, old.SupersededBy, "the fixture must actually supersede")
	require.False(t, old.State.Closed(), "a superseded change is still open — that is the whole point")
	require.False(t, old.Bound(), "and it is not Bound(), which is what made the wrong guard look right")

	d := Discard{Repo: repo, State: st, Me: me(), Now: now, Invoker: record.Human}
	_, err = d.Run(t.Context(), string(first.Ref.ID()))
	require.NoError(t, err)

	s, err = st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, record.ChangeDiscarded, s.Changes[string(first.Ref.ID())].State,
		"discard reported success, so the record must not still be open")
}

// A REFUSED REPLACEMENT LEAVES THE WORLD AS IT FOUND IT. --replace over
// a branch that is checked out is exit 46, and the refusal used to come
// AFTER the old change's verification had been canceled: the person was
// told to switch away, and the in-flight work they were told nothing
// about was already gone. Reported by GPT with a reproduction.
//
// The first change must really ENQUEUE, or there is nothing for the
// cancel to destroy and this passes against the very ordering it exists
// to refuse — which is what the first draft of it did.
func TestChangeRefusedReplacementCancelsNothing(t *testing.T) {
	repo, st := fixture(t)
	prov := &verifytest.Fake{}
	first := enqueued(t, repo, st, prov, "jq", "1.8", "jq-1.8")

	before, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, before.Attempts, 1, "the fixture premise: work is in flight")
	var was record.Attempt
	for _, a := range before.Attempts {
		was = a
	}
	require.NotEqual(t, record.Finished, was.Phase, "and it has not finished")

	// Check the branch out in a worktree, which is what makes the
	// replacement refusable.
	wt := filepath.Join(t.TempDir(), "held")
	out, werr := exec.Command("git", "-C", repo.Root, "worktree", "add", "--quiet", wt, "dockhand/jq-1.8").CombinedOutput()
	require.NoError(t, werr, string(out))

	op := Change{
		Repo: repo, State: st, Ledger: ledger.Open(repo), Stage: &stager{}, Local: quiet{},
		Verifier: has(prov), Me: me(), Now: now,
	}
	_, err = op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.9"), Delivery: Enqueue,
		Platform: sequoia, Slug: "jq-1.9", Replace: Replace,
	})
	require.ErrorIs(t, err, change.ErrCheckedOut)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, after.Attempts, 1)
	var still record.Attempt
	for _, a := range after.Attempts {
		still = a
	}
	assert.Equal(t, was.Phase, still.Phase,
		"a refused replacement must not cancel the verification it refused to replace")
	assert.Equal(t, record.ChangeMinted, after.Changes[string(first.Ref.ID())].State,
		"nor close the change")
	assert.True(t, repo.HasBranch(t.Context(), "dockhand/jq-1.8"), "nor take its branch")
	assert.Len(t, after.Changes, 1, "and nothing new was minted")
}
