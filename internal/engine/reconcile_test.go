package engine

// The read side, as one pass. What these hold is the shape the two
// renderings depend on: which phase runs when, what each verb pays for,
// and where the prose a side effect produces ends up.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
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

func TestReconcileCarriesTheAutocleansProseWithItsBranch(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
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

func TestReconcileNoCleanWithholdsTheDeletionAndKeepsTheVerdict(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{NoClean: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.False(t, rep.Branches[0].Retire.Cleaned)
	assert.Empty(t, rep.Branches[0].Prose)
	assert.True(t, rep.Branches[0].Retire.PR.Merged,
		"what a merged PR means does not depend on being asked to act on it")
	assert.True(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"))
}

func TestReconcileRetireOnlyObservesNothing(t *testing.T) {
	repo, sha := promotedRepo(t)
	ctx := context.Background()
	runningNote(t, repo, sha, "fake-1")
	// A provider that would answer "passed" if it were ever asked. The
	// sweep must not ask: polling a worker cannot change whether a pull
	// request merged.
	fake := &verifytest.Fake{States: map[string]verify.Status{
		"fake-1": {State: verify.Passed, Handle: "fake-1"}}}
	forge := &forgeFake{prs: `[{"number":9,"state":"open","html_url":"https://x/9"}]`}
	eng := testState(t, repo, fake)
	eng.Gh = forge.run

	rep, err := eng.Reconcile(ctx, ReconcileOpts{RetireOnly: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.Empty(t, rep.Branches[0].Tip, "the sweep reads no standing")
	assert.Nil(t, rep.Branches[0].Note)
	assert.Empty(t, fake.Released, "and settles nothing")
	assert.Equal(t, verdict.RetireOpen,
		verdict.DecideRetire(rep.Branches[0].Retire.Promoted, rep.Branches[0].Retire.PR))
}

func TestReconcileRetireOnlyReadsTheLandedBytesOnlyWhenMerged(t *testing.T) {
	repo, _ := promotedRepo(t)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng := testState(t, repo, &verifytest.Fake{})
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{RetireOnly: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.True(t, rep.Branches[0].Retire.Cleaned)
	assert.False(t, rep.Branches[0].Landed,
		"the branch's bump is not on the primary branch, and the sweep says so")
}

func TestReconcileStatesAnUnanswerableLookupWhereEachVerbReadsIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ReconcileOpts
		read func(render.BranchReport) string
	}{
		{"the report keeps the standing and adds the failure", ReconcileOpts{},
			func(b render.BranchReport) string { return b.Retire.Err }},
		{"the sweep has nothing else to say, so the failure is the line", ReconcileOpts{RetireOnly: true},
			func(b render.BranchReport) string { return b.SweepErr }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := promotedRepo(t)
			eng := testState(t, repo, &verifytest.Fake{})
			eng.Gh = func(context.Context, ...string) (string, error) {
				return "", fmt.Errorf("gh api: HTTP 502 from api.github.com")
			}

			rep, err := eng.Reconcile(context.Background(), tc.opts)
			require.NoError(t, err, "one unreachable pull request must not end the pass")
			require.Len(t, rep.Branches, 1)
			assert.Contains(t, tc.read(rep.Branches[0]), "HTTP 502")
		})
	}
}

func TestReconcileRetiresBeforeItDrains(t *testing.T) {
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

	rep, err := eng.Reconcile(ctx, ReconcileOpts{Drain: true})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.True(t, rep.Branches[0].Retire.Cleaned)
	assert.Empty(t, fake.Submitted, "nothing is started on a branch this pass deleted")
	assert.Empty(t, rep.Drain)
}

func TestReplaceInFlightAnnouncesThenReportsTheDemolition(t *testing.T) {
	// The fourth place a discard's report lands, and the one with no
	// golden above it: --force announces the replacement and then owes
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

	assert.Equal(t, "replacing in-flight dockhand/jq-1.8 (--force)\n", errb.String(),
		"the announcement is the verb's and stays on stderr")
	assert.Contains(t, out.String(), "discarded dockhand/jq-1.8 (",
		"and the deletion still says so on stdout")
}

func TestReconcileOnAnEmptyNamespaceNamesTheRepository(t *testing.T) {
	repo := gittest.PortsTree(t, realTools)
	eng := testState(t, repo, &verifytest.Fake{})

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{Drain: true})
	require.NoError(t, err)

	assert.Equal(t, repo.Root, rep.Repository,
		"run from the wrong checkout, `no branches` is true and useless")
	assert.Empty(t, rep.Branches)
	assert.False(t, rep.Now.IsZero(), "the pass still reads its clock")
}
