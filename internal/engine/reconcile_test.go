package engine

// The read side, as one pass. What these hold is the shape the two
// renderings depend on: which phase runs when, what each verb pays for,
// and where the prose a side effect produces ends up.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// forgeFake answers the two calls a retirement judgment makes and
// counts them, which is how the laziness contract is held: an
// all-unpromoted namespace must reach GitHub not at all.
type forgeFake struct {
	prs   string
	calls int
}

func (g *forgeFake) run(_ context.Context, args ...string) (string, error) {
	g.calls++
	switch {
	case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "/pulls?head="):
		if g.prs == "" {
			return "[]", nil
		}
		return g.prs, nil
	}
	return "", fmt.Errorf("forgeFake: unscripted call %v", args)
}

// promotedRepo is engineRepo with the branch pushed to a fork, which is
// what makes it promoted and therefore worth asking GitHub about.
func promotedRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	repo, sha := engineRepo(t)
	gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(context.Background(), "herby", "dockhand/jq-1.8"))
	return repo, sha
}

// cutRepo is engineRepo with the branch tracking the fork the way a
// branch cut from a remote-tracking base does — `git switch -c foo
// herby/main` — and never pushed: branch.<name>.remote names the fork,
// and no copy of the branch exists anywhere but here. This is the
// field shape that read as promoted for as long as the gate asked
// TrackedRemote. The primary branch goes to the fork first so there is
// a remote-tracking base to cut against, as the field's origin/master
// is; --set-upstream-to then writes the two config keys switch -c
// would have.
func cutRepo(t *testing.T) *git.Repo {
	t.Helper()
	repo, _ := engineRepo(t)
	ctx := context.Background()
	gittest.BareFork(t, repo, "herbygillot", "herby")
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Push(ctx, "herby", primary))
	out, err := exec.Command("git", "-C", repo.Root, "branch", "--set-upstream-to=herby/"+primary, "dockhand/jq-1.8").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Equal(t, "herby", repo.TrackedRemote(ctx, "dockhand/jq-1.8"), "the trap this fixture models")
	return repo
}

// A tracked upstream is not a push. The branch tracks the fork the way
// `git switch -c` leaves one cut from a remote-tracking base, and was
// never pushed: the pass must not call it promoted, and must not spend
// a forge call finding out that it has no pull request — the cost the
// gate exists to avoid.
func TestReconcileDoesNotCallATrackedButUnpushedBranchPromoted(t *testing.T) {
	repo := cutRepo(t)
	forge := &forgeFake{}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.False(t, rep.Branches[0].Retire.Promoted)
	assert.Empty(t, rep.Branches[0].Retire.Line(), "nothing to say about a pull request that cannot exist")
	assert.Zero(t, forge.calls)
}

// The converse: a branch pushed bare — `git push herby dockhand/jq-1.8`,
// no -u — tracks nothing and is promoted all the same. The pass finds
// the copy by its remote-tracking ref and reads the fork owner from the
// remote holding it, so the lookup reaches the forge and its answer is
// judged rather than the branch reading as one with no pull request.
func TestReconcileJudgesABranchPushedBare(t *testing.T) {
	repo, _ := engineRepo(t)
	ctx := context.Background()
	gittest.BareFork(t, repo, "herbygillot", "herby")
	out, err := exec.Command("git", "-C", repo.Root, "push", "--quiet", "herby", "dockhand/jq-1.8").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Empty(t, repo.TrackedRemote(ctx, "dockhand/jq-1.8"), "a bare push writes no tracking config")

	forge := &forgeFake{prs: `[{"number":9,"state":"open","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(ctx, ReconcileOpts{})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	assert.True(t, b.Retire.Promoted)
	assert.Equal(t, 1, forge.calls, "one lookup, with the owner read from the remote that holds the copy")
	assert.True(t, b.Retire.PR.Open)
	assert.Equal(t, 9, b.Retire.PR.Number)
}

func TestReconcileAsksNoForgeQuestionOfAnUnpromotedNamespace(t *testing.T) {
	repo, _ := engineRepo(t)
	forge := &forgeFake{}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	assert.Zero(t, forge.calls,
		"a branch that was never pushed has no pull request, and finding that out must cost nothing")
	require.Len(t, rep.Branches, 1)
	assert.False(t, rep.Branches[0].Retire.Promoted)
	assert.Equal(t, "unverified", rep.Branches[0].Drift, "the standing is still observed")
}

func TestCycleCarriesTheRetirementsProseWithItsBranch(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{Retire: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	assert.True(t, b.Retire.Cleaned)
	// The order is the demolition's own, and it is the order a reader
	// sees: the fork copy's fate, then the deletion. Grouping by stream
	// would print the same lines and tell a different story.
	require.Len(t, b.Prose, 2)
	assert.Equal(t, render.ToErr, b.Prose[0].Stream)
	assert.Equal(t, `removed dockhand/jq-1.8 from "herby"`, b.Prose[0].Text)
	assert.Equal(t, render.ToOut, b.Prose[1].Stream)
	assert.Contains(t, b.Prose[1].Text, "discarded dockhand/jq-1.8 (")
	assert.False(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"))
}

// D27: `status` acts on nothing. The same merged verdict `cycle` deletes
// on is reached, recorded in the audit, and reported with the verb that
// would act — and the branch, its fork copy and its guest are all where
// they were.
func TestStatusReportsAMergedPullRequestAndNamesCycle(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	assert.False(t, b.Retire.Cleaned)
	assert.Empty(t, b.Prose)
	assert.True(t, b.Retire.PR.Merged, "the verdict is reached whether or not anybody acts on it")
	assert.Equal(t, "PR #9 merged — `dockhand cycle` retires the branch", b.Retire.Line())
	assert.True(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"))
}

func TestCycleKeepMergedWithholdsTheDeletionAndSaysSo(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{Retire: true, KeepMerged: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	assert.False(t, b.Retire.Cleaned)
	assert.Empty(t, b.Prose)
	assert.True(t, b.Retire.PR.Merged,
		"what a merged PR means does not depend on being asked to act on it")
	assert.Equal(t, "PR #9 merged — kept: --keep-merged", b.Retire.Line(),
		"no kept case is silent: the line says why the branch is still there")
	assert.True(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"))
}

func TestReconcileStatesAnUnanswerableLookupBesideTheStanding(t *testing.T) {
	repo, _ := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = func(context.Context, ...string) (string, error) {
		return "", fmt.Errorf("gh api: HTTP 502 from api.github.com")
	}

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err, "one unreachable pull request must not end the pass")
	require.Len(t, rep.Branches, 1)
	assert.Contains(t, rep.Branches[0].Retire.Err, "HTTP 502")
	assert.Equal(t, "unverified", rep.Branches[0].Drift, "the standing is kept and the failure is added beside it")
}

func TestCycleRetiresBeforeItDrains(t *testing.T) {
	// The branch's PR merged and its run is deferred. Drained first, the
	// pass would boot a guest for a branch it is about to delete and
	// release the worker it had just started; retired first, there is no
	// branch left to drain.
	repo, sha := promotedRepo(t)
	ctx := context.Background()
	require.NoError(t, testState(t, repo, nil).recordRun(ctx, repo, sha, "jq", "Testos",
		record.Run{State: record.Queued, Detail: "all slots busy"}, ""))
	fake := &verifytest.Fake{}
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, fake)
	eng.Gh = forge.run

	rep, err := eng.Reconcile(ctx, ReconcileOpts{Retire: true, Drain: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.True(t, rep.Branches[0].Retire.Cleaned)
	assert.Empty(t, fake.Submitted, "nothing is started on a branch this pass deleted")
	assert.Empty(t, rep.Drain)
}

// A machine without tart starts nothing, and says so: `cycle`'s report
// names `dockhand cycle` beside every queued run, so a cycle that
// could not start one owes the reader the reason beside it. Said once
// for the pass and only when a queue is waiting — a tart-less machine
// with nothing queued has nothing to explain.
func TestCycleWithoutTartSaysWhyTheQueueWaits(t *testing.T) {
	ctx := context.Background()
	noTart := tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return "", errors.New("tart stubbed absent")
		}
		return exec.LookPath(name)
	})

	t.Run("a queue waits", func(t *testing.T) {
		repo, sha := engineRepo(t)
		fake := &verifytest.Fake{}
		eng := testState(t, repo, fake)
		eng.Tools = noTart
		require.NoError(t, eng.recordRun(ctx, repo, sha, "jq", "Testos",
			record.Run{State: record.Queued, Detail: "all 2 verification slots are busy"}, ""))

		rep, err := eng.Reconcile(ctx, ReconcileOpts{Retire: true, Drain: true})
		require.NoError(t, err)

		assert.Empty(t, fake.Submitted)
		require.Len(t, rep.Drain, 1)
		assert.Equal(t, "nothing started: tart is not on PATH; queued runs on 1 branch(es) wait for it", rep.Drain[0].Text)
	})

	t.Run("nothing queued, nothing said", func(t *testing.T) {
		repo, _ := engineRepo(t)
		eng := testState(t, repo, &verifytest.Fake{})
		eng.Tools = noTart

		rep, err := eng.Reconcile(ctx, ReconcileOpts{Retire: true, Drain: true})
		require.NoError(t, err)
		assert.Empty(t, rep.Drain)
	})
}

func TestReplaceInFlightAnnouncesThenReportsTheDemolition(t *testing.T) {
	// The fourth place a discard's report lands, and the one with no
	// golden above it: --replace announces the replacement and then owes
	// the user the demolition's own account of what went. When Discard
	// printed for itself this was free; now it is a line this caller has
	// to place, and losing it would be silent.
	repo, _ := engineRepo(t)
	ctx := context.Background()
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, eng.replaceInFlight(ctx, repo, primary, "dockhand/jq-1.8"))

	assert.Equal(t, "replacing in-flight dockhand/jq-1.8 (--replace)\n", errb.String(),
		"the announcement is the verb's and stays on stderr")
	assert.Contains(t, out.String(), "discarded dockhand/jq-1.8 (",
		"and the deletion still says so on stdout")
}

func TestReconcileOnAnEmptyNamespaceNamesTheRepository(t *testing.T) {
	repo := gittest.PortsTree(t, realTools)
	eng := testState(t, repo, &verifytest.Fake{})

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{Retire: true, Drain: true})
	require.NoError(t, err)

	assert.Equal(t, repo.Root, rep.Repository,
		"run from the wrong checkout, `no branches` is true and useless")
	assert.Empty(t, rep.Branches)
	assert.False(t, rep.Now.IsZero(), "the pass still reads its clock")
}

// D27, the index entry for the engine's half: `status` observes and
// settles; `cycle` acts. One pass with nothing turned on, over a
// namespace carrying a running job the provider will call passed, a
// queued run, and a pushed branch whose pull request merged: the job is
// settled and its guest released — the last step of the verdict being
// written — and nothing else moves. No submit, no deletion, no push to
// the fork, no forge write. Then the same pass as `cycle`, which does
// all of it.
func TestStatusObservesAndSettlesWhileCycleActs(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	fork := gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "dockhand/jq-1.8"))
	// A running job on the merged branch, and a queued run on a second
	// branch nothing will delete.
	runningNote(t, repo, sha, "fake-1")
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	queued := gittest.Commit(t, repo, "dockhand/jq-1.9", primary, "sysutils/jq/Portfile",
		"version 1.9\n", "jq: update to 1.9")
	require.NoError(t, testState(t, repo, nil).recordRun(ctx, repo, queued, "jq", "Testos",
		record.Run{State: record.Queued, Detail: "all 2 verification slots are busy"}, ""))
	fake := &verifytest.Fake{States: map[string]verify.Status{
		"fake-1": {State: verify.Passed, Handle: "fake-1"}}}
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	forkRefs := func() string {
		out, err := exec.Command("git", "-C", fork, "for-each-ref").Output()
		require.NoError(t, err)
		return string(out)
	}
	before := forkRefs()

	eng := testState(t, repo, fake)
	eng.Gh = forge.run
	eng.Tools = pumpTools(t)

	rep, err := eng.Reconcile(ctx, ReconcileOpts{})
	require.NoError(t, err)

	// Settled: the guest went back, because that is the verdict being
	// written and not an act of its own.
	assert.Equal(t, []string{"fake-1"}, fake.Released, "the settled job's guest, and only that")
	require.Len(t, rep.Branches, 2)
	assert.Equal(t, record.Passed, runOf(*rep.Branches[0].Note, "Testos").State)
	// And nothing acted on: no submit, no deletion, no push, no drain.
	assert.Empty(t, fake.Submitted, "status starts nothing")
	assert.True(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "status deletes nothing")
	assert.Equal(t, before, forkRefs(), "status pushes nothing")
	assert.Empty(t, rep.Drain)
	assert.Empty(t, rep.Branches[0].Prose)
	assert.Equal(t, "PR #9 merged — `dockhand cycle` retires the branch", rep.Branches[0].Retire.Line(),
		"where work is waiting, status names the verb")
	assert.Equal(t, record.Queued, runOf(*rep.Branches[1].Note, "Testos").State)
	assert.Contains(t, render.RecordLines(*rep.Branches[1].Note, rep.Now)[0], "`dockhand cycle` starts it")

	// The same pass as cycle: the merged branch goes, locally and off the
	// fork, and the queued run is started.
	rep, err = eng.Reconcile(ctx, ReconcileOpts{Retire: true, Drain: true})
	require.NoError(t, err)
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
	assert.NotEqual(t, before, forkRefs(), "the fork copy is removed with the branch")
	require.Len(t, fake.Submitted, 1, "the drain started the queued run")
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports)
	assert.NotEmpty(t, rep.Drain)
}

// D27's pure read: `status --no-update` is the ledger and nothing else.
// A provider that fails the test if composed, a forge that fails it if
// asked, a running run that stays running, a pushed branch whose pull
// request reads as not checked rather than as absent — and the report
// says at the top that nothing was polled.
func TestStatusNoUpdateReadsTheLedgerAndAsksNobody(t *testing.T) {
	ctx := context.Background()
	repo, sha := promotedRepo(t)
	runningNote(t, repo, sha, "fake-1")

	eng := testState(t, repo, nil)
	fail := func(what string) func(context.Context) (verify.Verifier, error) {
		return func(context.Context) (verify.Verifier, error) {
			t.Fatalf("--no-update composed %s", what)
			return nil, nil
		}
	}
	eng.Verifier, eng.Lister = fail("the verifier"), fail("the lister")
	eng.Gh = func(_ context.Context, args ...string) (string, error) {
		t.Fatalf("--no-update asked the forge: %v", args)
		return "", nil
	}

	rep, err := eng.Reconcile(ctx, ReconcileOpts{NoUpdate: true})
	require.NoError(t, err)

	assert.True(t, rep.AsRecorded)
	require.Len(t, rep.Branches, 1)
	b := rep.Branches[0]
	require.NotNil(t, b.Note)
	assert.Equal(t, record.Running, runOf(*b.Note, "Testos").State, "the note is shown as written")
	assert.True(t, b.Retire.Promoted, "pushed is a local ref, and may be read")
	assert.True(t, b.Retire.Unasked)
	assert.Equal(t, "promoted; PR not checked (--no-update)", b.Retire.Line())
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, runOf(again, "Testos").State, "and nothing was written")
}

// D27's fold-in: a branch outside dockhand/ whose tip carries a verify
// note is observed — listed, settled, its pull request judged, its
// queued run drained — and never deleted, whatever its pull request
// did. `verify` on such a branch closes with "`dockhand status` follows
// it", and this is what makes that true.
func TestAHandMadeBranchWithANoteIsShownSettledAndNeverDeleted(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	hand := gittest.Commit(t, repo, "erasure-test", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	// A hand-made branch with no note is not observed; a note on the
	// primary lists nothing.
	gittest.Commit(t, repo, "unrelated-work", primary, "sysutils/jq/Portfile",
		"version 2.0\n", "jq: update to 2.0")
	head, err := repo.RevParse(ctx, primary)
	require.NoError(t, err)
	runningNote(t, repo, head, "fake-primary")
	fork := gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(ctx, "herby", "erasure-test"))
	forge := &forgeFake{prs: `[{"number":12,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/12"}]`}

	t.Run("listed and settled by status", func(t *testing.T) {
		runningNote(t, repo, hand, "fake-1")
		fake := &verifytest.Fake{States: map[string]verify.Status{
			"fake-1": {State: verify.Passed, Handle: "fake-1"}}}
		eng := testState(t, repo, fake)
		eng.Gh = forge.run

		rep, err := eng.Reconcile(ctx, ReconcileOpts{})
		require.NoError(t, err)

		require.Len(t, rep.Branches, 1, "the noted hand-made branch, and neither the unnoted one nor the primary")
		b := rep.Branches[0]
		assert.Equal(t, "erasure-test", b.Branch)
		assert.False(t, b.Minted)
		assert.Equal(t, record.Passed, runOf(*b.Note, "Testos").State, "settled like any other")
		assert.Equal(t, []string{"fake-1"}, fake.Released)
		assert.True(t, b.Retire.PR.Merged, "its pull request is judged like any other")
		assert.Equal(t, "PR #12 merged — not a dockhand branch, so nothing here removes it", b.Retire.Line())
	})

	t.Run("drained by cycle and never deleted", func(t *testing.T) {
		require.NoError(t, testState(t, repo, nil).recordRun(ctx, repo, hand, "jq", "Testos",
			record.Run{State: record.Queued, Detail: "all 2 verification slots are busy"}, ""))
		fake := &verifytest.Fake{}
		eng := testState(t, repo, fake)
		eng.Gh = forge.run
		eng.Tools = pumpTools(t)
		out, err := exec.Command("git", "-C", fork, "for-each-ref").Output()
		require.NoError(t, err)

		rep, err := eng.Reconcile(ctx, ReconcileOpts{Retire: true, Drain: true})
		require.NoError(t, err)

		require.Len(t, rep.Branches, 1)
		assert.False(t, rep.Branches[0].Retire.Cleaned)
		assert.Empty(t, rep.Branches[0].Prose)
		assert.True(t, repo.HasBranch(ctx, "erasure-test"), "deletion stays in the namespace")
		after, err := exec.Command("git", "-C", fork, "for-each-ref").Output()
		require.NoError(t, err)
		assert.Equal(t, string(out), string(after), "and the fork copy stands too")
		require.Len(t, fake.Submitted, 1, "the queued run on a hand-made branch is started")
		assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports)
	})

	t.Run("cycle --keep-merged says the reason that is true", func(t *testing.T) {
		eng := testState(t, repo, &verifytest.Fake{})
		eng.Gh = forge.run

		rep, err := eng.Reconcile(ctx, ReconcileOpts{Retire: true, KeepMerged: true})
		require.NoError(t, err)

		require.Len(t, rep.Branches, 1)
		assert.Equal(t, "PR #12 merged — not a dockhand branch, so nothing here removes it", rep.Branches[0].Retire.Line(),
			"a branch no flag could have deleted is not said to be kept by one")
		assert.True(t, repo.HasBranch(ctx, "erasure-test"))
	})
}
