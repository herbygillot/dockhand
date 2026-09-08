package publish

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// versions is the evaluation seam, scripted: an identity per rev. app
// implements the real one over the Tcl evaluator, and this is what the
// seam exists for — a publication's direction is arguable without a
// MacPorts installation behind it.
type versions map[string]macports.Identity

func (v versions) IdentityAt(_ context.Context, rev, _ string) (macports.Identity, error) {
	id, ok := v[rev]
	if !ok {
		return macports.Identity{}, assertErr
	}
	return id, nil
}

// gathered stands a real repository, a real store and a real change up,
// and returns the resolved ref beside them. change.Ref has no literal —
// the only way to obtain one is Resolve, which found it in the store AND
// in the repository — so a Gather test is a test over a real tree by
// construction.
func gathered(t *testing.T) (*git.Repo, *statestore.Store, change.Ref, string) {
	t.Helper()
	repo := gittest.PortsTree(t, tools)
	base, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", base,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	st := statestore.Open(repo)
	plantChange(t, st, record.Change{
		ID: "chg-1", State: record.ChangeMinted, Branch: "dockhand/jq-1.8", Tip: sha,
		Content: "tree-1", Destination: record.ToPublished, ClosesTicket: "12345",
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq", Intent: "bump", Target: "1.8"}},
		Base:     record.Base{Sha: base, CommittedAt: clock.Add(-48 * time.Hour)},
	})
	ref, err := change.Resolve(t.Context(), repo, st, "dockhand/jq-1.8")
	require.NoError(t, err)
	return repo, st, ref, base
}

// GATHER IS THE FACTS STAGE THE GRID OMITTED, and it reads the change
// and its attempts from the STATE REF — never from the exported note,
// which publish cannot even obtain, since it does not import ledger.
//
// Under ForgeAsCached it asks the forge NOTHING, which is what makes
// `status` cheap; and the facts it produces are refused by Authorize
// (ErrNotFresh) so that a cached standing can never reach a decision.
func TestGatherReadsTheStateRefAndUnderAsCachedAsksNothing(t *testing.T) {
	repo, st, ref, base := gathered(t)
	plantAttempt(t, st, record.Attempt{
		ID: "att-1", Change: "chg-1", Sha: ref.Tip(), Content: "tree-1",
		Platform: "Sequoia", Phase: record.Finished, Started: clock,
		Runs: map[string]record.Run{"jq": {State: record.Passed, Content: "tree-1", At: clock}},
	})
	env := Env{Repo: repo, State: st, Forge: func(context.Context, ...string) (string, error) {
		t.Fatal("ForgeAsCached asked the forge something")
		return "", nil
	}}

	f, err := Gather(t.Context(), env, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)

	assert.Equal(t, record.ChangeID("chg-1"), f.Change.ID)
	assert.Len(t, f.Attempts, 1)
	assert.Equal(t, "dockhand/jq-1.8", f.Branch)
	assert.Equal(t, ref.Tip(), f.Tip)
	assert.Equal(t, []string{ref.Tip()}, f.Own, "one commit beyond the primary branch")
	assert.Equal(t, "jq: update to 1.8", f.Title, "the minted commit's own subject")
	assert.Equal(t, gh.MaxPRBody, f.BodyLimit)
	assert.True(t, f.Spent.Counted())

	// change.Behind's answer, whose consumer this is: the branch is not
	// behind its base and no upstream commit has touched its portdir.
	assert.True(t, f.Drift.Compared)
	assert.Zero(t, f.Drift.BehindBy)
	assert.False(t, f.Drift.OverTree)
	assert.Equal(t, base, f.Drift.Base.Sha)

	// Nothing was recorded about a publication, so the cached standing is
	// the honest empty pair: nobody asked, and there is nothing to serve.
	assert.False(t, f.Forge.Fresh)
	assert.True(t, f.Forge.AsOf.IsZero())

	// And the gate refuses it, which is the property the whole field is
	// for.
	_, _, err = Authorize(f, Pace{})
	require.ErrorIs(t, err, ErrNotFresh)
}

// A POLICY NOBODY CHOSE IS REFUSED (rule 7), one stage before the gate:
// the difference between asking the forge and serving what a cycle
// cached is a decision a caller makes, and a zero that quietly meant
// either would put a cached standing in front of a gate that must never
// see one.
func TestGatherRefusesAForgePolicyNobodyChose(t *testing.T) {
	repo, st, ref, _ := gathered(t)
	_, err := Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeUnset, Asks{}, record.Human, clock)
	require.ErrorIs(t, err, ErrForgePolicyUnset)
}

// THE CACHED ROAD SERVES THE ROW AND FABRICATES NO FORGE STATE WORD.
// record.Publication carries an Outcome, which is what dockhand
// concluded; gh.PullRequest carries the forge's own spellings, which is
// what the forge said. Filling the second from the first would put a
// sentence in a report that no forge ever uttered.
func TestGatherAsCachedServesTheRowAndNoForgeWords(t *testing.T) {
	repo, st, ref, _ := gathered(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", By: record.Human,
		Number: 77, URL: "https://example.invalid/pull/77", Outcome: record.Open,
		Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: clock.Add(-time.Hour)}}})

	f, err := Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)
	assert.True(t, f.Forge.OwnFound)
	assert.Equal(t, 77, f.Forge.Own.Number)
	assert.Equal(t, clock.Add(-time.Hour), f.Forge.AsOf)
	assert.Empty(t, f.Forge.Own.State, "no state word the forge never said")
	assert.Empty(t, f.Forge.Own.MergedAt)
	assert.False(t, f.Forge.Fresh)
}

// THE FROM SIDE IS UPSTREAM MAIN'S TIP AND NOT THE CHANGE'S BASE, which
// is why direction is observed at PUBLICATION rather than carried from
// the mint: the install a downgrade strands is whatever main ships at
// merge.
func TestDirectionIsReadOverUpstreamMainsTip(t *testing.T) {
	repo, st, ref, _ := gathered(t)
	primary, err := repo.PrimaryBranch(t.Context())
	require.NoError(t, err)
	env := Env{Repo: repo, State: st, Eval: versions{
		primary:   {Epoch: "0", Version: "1.7", Revision: "0"},
		ref.Tip(): {Epoch: "0", Version: "1.8", Revision: "0"},
	}}

	f, err := Gather(t.Context(), env, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)
	require.NoError(t, f.Direction.Err)
	assert.True(t, f.Direction.Movement.Compared)
	assert.True(t, f.Direction.Movement.Moved)
	assert.True(t, f.Direction.Movement.Upgrades)
	assert.False(t, f.Direction.EpochOwed)
}

// A DOWNGRADE OWES AN EPOCH, and EpochOwed is base's own predicate —
// the version string changed and base would skip the install — computed
// from the REALIZED delta, so a planner that could not emit the edit
// still cannot publish unattended.
func TestADowngradeOwesAnEpochAndRefusesTheMachine(t *testing.T) {
	repo, st, ref, _ := gathered(t)
	primary, err := repo.PrimaryBranch(t.Context())
	require.NoError(t, err)
	env := Env{Repo: repo, State: st, Eval: versions{
		primary:   {Epoch: "0", Version: "1.9", Revision: "0"},
		ref.Tip(): {Epoch: "0", Version: "1.8", Revision: "0"},
	}}
	f, err := Gather(t.Context(), env, ref, ForgeAsCached, Asks{}, record.Machine, clock)
	require.NoError(t, err)
	assert.True(t, f.Direction.EpochOwed)

	// And it refuses, over a fact set that is otherwise a clean machine
	// publication of a change change.Judge called Simple.
	f.Forge.Fresh, f.Unattended, f.Simplicity = true, GrantSimpleBumps, change.Simple
	f.Attempts = []record.Attempt{{
		ID: "att-1", Change: "chg-1", Sha: f.Tip, Content: "tree-1", Platform: "Sequoia",
		Phase: record.Finished, Started: clock,
		Runs: map[string]record.Run{"jq": {State: record.Passed}},
	}}
	_, _, err = Authorize(f, DefaultPace)
	require.ErrorIs(t, err, ErrEpochOwed)
}

// AN EVALUATION THAT COULD NOT BE MADE IS AN ANSWER AND NOT AN INCIDENT.
// A person may publish a change to anything they could type, including a
// downgrade, on "the operator typed it" — so a gather that returned an
// error here would stop the road that does not care.
func TestAnUnwiredEvaluatorIsAFactAndNotAFailure(t *testing.T) {
	repo, st, ref, _ := gathered(t)
	f, err := Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)
	require.ErrorIs(t, f.Direction.Err, ErrDirectionUnknown)
	assert.False(t, f.Direction.Movement.Compared)
}

// A CHANGE WITH NO BRANCH HAS NOTHING TO PUSH. A branchless snapshot's
// verification is the whole of what it was for.
func TestGatherRefusesABranchlessChange(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	st := statestore.Open(repo)
	base, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		_, err := change.AdoptIn(tx, change.Adoption{
			ID: "chg-snap", Tip: base, Content: "tree-x",
			Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}},
		}, clock)
		return err
	}))
	ref, err := change.Resolve(t.Context(), repo, st, "chg-snap")
	require.NoError(t, err)
	_, err = Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.ErrorIs(t, err, ErrNoBranch)
}

func plantAttempt(t *testing.T, st *statestore.Store, a record.Attempt) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutAttempt(a)
		return nil
	}))
}

// A BRANCH'S OWN COMMITS ARE MEASURED AGAINST WHAT IT WAS BASED ON, and
// for the whole of the overhaul they were measured against the
// checkout's local primary branch instead. While every change was cut
// from that branch the two were the same commit and the error could not
// show; D29 bases a change on upstream's freshly fetched tip, so a
// checkout ten commits behind made Own eleven commits.
//
// MEASURED ON TWO LIVE PULL REQUESTS. Both came out titled "debianutils:
// Update to 5.24" — an upstream commit neither change touched — because
// title() takes Own's last entry, which rev-list order makes the OLDEST
// once the range is wrong. The same count drives body.go's `single`, so
// both commit-guideline boxes went unchecked on changes carrying exactly
// one commit each. One wrong range, three wrong statements to a reviewer.
func TestGatherMeasuresOwnCommitsAgainstTheRecordedBaseNotTheLocalPrimary(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	primary, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)

	// Upstream moves on under a checkout that stands still — the shape a
	// fetch produces, and the shape that made this visible.
	// The fixture tree holds one portdir, so the upstream commits touch
	// files inside it. What they touch is immaterial: the point is that
	// they are commits the change did not make.
	// Each fixture commit lands its own branch name: gittest.Commit uses a
	// `create` line, which refuses a name already in flight.
	up1 := gittest.Commit(t, repo, "upstream-a", primary,
		"sysutils/jq/UPSTREAM-A", "a\n", "other: update to 1")
	up2 := gittest.Commit(t, repo, "upstream-b", up1,
		"sysutils/jq/UPSTREAM-B", "b\n", "debianutils: Update to 5.24")

	// The change is cut from upstream's tip, not from the local primary.
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", up2,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	st := statestore.Open(repo)
	plantChange(t, st, record.Change{
		ID: "chg-1", State: record.ChangeMinted, Branch: "dockhand/jq-1.8", Tip: sha,
		Content: "tree-1", Destination: record.ToPublished,
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq", Intent: "bump", Target: "1.8"}},
		Base:     record.Base{Sha: up2, CommittedAt: clock.Add(-time.Hour)},
	})
	ref, err := change.Resolve(t.Context(), repo, st, "dockhand/jq-1.8")
	require.NoError(t, err)

	f, err := Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)

	assert.Equal(t, []string{sha}, f.Own,
		"the branch adds one commit to what it was based on; the fetch's commits are not its work")
	assert.Equal(t, "jq: update to 1.8", f.Title,
		"the title is the change's own commit, never an upstream commit the fetch brought in")
}

// AND A CHANGE WITH NO RECORDED BASE FALLS BACK TO THE PRIMARY, which is
// an adopted branch dockhand did not mint: "what it was based on" is
// genuinely not recorded, and the checkout's own branch is the best
// available answer.
func TestGatherFallsBackToThePrimaryForAChangeWithNoBase(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	primary, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	st := statestore.Open(repo)
	plantChange(t, st, record.Change{
		ID: "chg-1", State: record.ChangeMinted, Branch: "dockhand/jq-1.8", Tip: sha,
		Content: "tree-1", Destination: record.ToPublished,
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq", Intent: "bump", Target: "1.8"}},
	})
	ref, err := change.Resolve(t.Context(), repo, st, "dockhand/jq-1.8")
	require.NoError(t, err)

	f, err := Gather(t.Context(), Env{Repo: repo, State: st}, ref, ForgeAsCached, Asks{}, record.Human, clock)
	require.NoError(t, err)
	assert.Equal(t, []string{sha}, f.Own)
	assert.Equal(t, "jq: update to 1.8", f.Title)
}
