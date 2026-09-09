package change

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// realCommit is a commit object the batch will accept as a create line's
// new value: update-ref refuses an oid the repository does not have, so
// a fixture cannot hand it a word.
func realCommit(t *testing.T, repo *git.Repo) (string, record.ContentID) {
	t.Helper()
	sha := primary(t, repo)
	tree, err := repo.RevParse(context.Background(), sha+"^{tree}")
	require.NoError(t, err)
	return sha, record.ContentID(tree)
}

// byHand writes a commit and moves branch to it the way a person's `git
// commit` does — outside every verb this package uses, and past the
// create-only fixture in gittest.
func byHand(t *testing.T, repo *git.Repo, branch, parent, content, message string) string {
	t.Helper()
	ctx := context.Background()
	tree, err := repo.GraftTree(ctx, parent, []git.File{{Path: "sysutils/jq/Portfile", Content: []byte(content)}})
	require.NoError(t, err)
	sha, err := repo.CommitTree(ctx, tree, []string{parent}, message)
	require.NoError(t, err)
	if branch != "" {
		plant(t, repo, "update-ref", "refs/heads/"+branch, sha)
	}
	return sha
}

// minted is the fixture every mutator test starts from: a prepared bump,
// an unreferenced commit over the primary branch, and one Amend that
// writes the record and creates the branch together.
func minted(t *testing.T, repo *git.Repo, st *statestore.Store, id record.ChangeID, branch string) record.Change {
	t.Helper()
	ctx := context.Background()
	base := primary(t, repo)
	sha, content, err := Commit(ctx, repo, prepared(t, repo), base)
	require.NoError(t, err)
	var c record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		c, err = MintIn(tx, Minting{
			ID: id, Branch: branch, Tip: sha, Content: content, Slug: "jq-1.8",
			Subjects:    []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq", Target: "1.8"}},
			Crossing:    record.StableToStable,
			Destination: record.ToBranch,
			Base:        record.Base{Sha: base},
			Prov:        Provenance{AskedBy: record.Human, Via: record.MintedSingle},
		}, now)
		return err
	}))
	return c
}

func TestMintInWritesTheRecordAndTheBranchInOneCommit(t *testing.T) {
	repo, st := newRepo(t)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	assert.Equal(t, record.ChangeMinted, c.State)
	assert.Equal(t, c.Tip, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"),
		"the record names a commit its ref carries, by construction")
	state, err := st.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, c.Tip, state.Changes["chg-01"].Tip)
	assert.Nil(t, state.Changes["chg-01"].Hold, "a stable-to-stable crossing is born free")
}

func TestMintInRefusesAnIncompleteMinting(t *testing.T) {
	_, st := newRepo(t)
	full := Minting{ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: "abc", Content: "def", Destination: record.ToBranch}
	for _, c := range []struct {
		name string
		with func(m *Minting)
	}{
		{"no id", func(m *Minting) { m.ID = "" }},
		{"no tip", func(m *Minting) { m.Tip = "" }},
		{"no content", func(m *Minting) { m.Content = "" }},
		{"no branch", func(m *Minting) { m.Branch = "" }},
		{"nowhere to go", func(m *Minting) { m.Destination = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := full
			c.with(&m)
			err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
				_, err := MintIn(tx, m, now)
				return err
			})
			assert.ErrorIs(t, err, ErrIncomplete)
		})
	}
}

func TestMintInRefusesABranchAStandingRecordBinds(t *testing.T) {
	repo, st := newRepo(t)
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
		_, err := MintIn(tx, Minting{
			ID: "chg-02", Branch: "dockhand/jq-1.8", Tip: "abc", Content: "def", Destination: record.ToBranch,
		}, now)
		return err
	})
	assert.ErrorIs(t, err, ErrStanding, "the record-level judgment, inside the closure and under the flock")
}

func TestMintInMeetsAForeignBranchAtTheBatch(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	// A branch a hand made, with no record at all: the record-level judge
	// has nothing to say and the create line is what refuses.
	plant(t, repo, "branch", "dockhand/jq-1.8")

	sha, content, err := Commit(ctx, repo, prepared(t, repo), primary(t, repo))
	require.NoError(t, err)
	err = st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := MintIn(tx, Minting{
			ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: sha, Content: content, Destination: record.ToBranch,
		}, now)
		return err
	})
	require.ErrorIs(t, err, git.ErrRefMoved,
		"a foreign hand created this ref is not the same finding as a record binds this name")
	state, err := st.Read(ctx)
	require.ErrorIs(t, err, statestore.ErrNoState, "nothing was written, the record included")
	assert.Empty(t, state.Changes)
}

func TestMintInBearsTheHoldFromTheCrossingAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		crossing record.Crossing
		held     bool
	}{
		{record.StableToStable, false},
		{record.PrereleaseLateral, false},
		{record.PrereleaseToStable, false},
		{record.StableToPrerelease, true},
		{record.CrossingUnknown, true},
	} {
		t.Run(string(c.crossing)+"/", func(t *testing.T) {
			repo, st := newRepo(t)
			tip, content := realCommit(t, repo)
			var got record.Change
			require.NoError(t, st.Amend(context.Background(), func(tx *statestore.Txn) error {
				var err error
				got, err = MintIn(tx, Minting{
					ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: tip, Content: content,
					Crossing: c.crossing, Destination: record.ToBranch,
				}, now)
				return err
			}))
			if !c.held {
				assert.Nil(t, got.Hold)
				return
			}
			require.NotNil(t, got.Hold)
			assert.Equal(t, record.HoldCrossing, got.Hold.Origin,
				"the born hold is a crossing's, so it never withholds the build this same invocation asked for")
			require.NoError(t, Held(got, ActVerify, record.Human))
			assert.ErrorIs(t, Held(got, ActPublish, record.Machine), ErrHeld)
		})
	}
}

func TestMintInStampsPlanFindingsIntoTheRecord(t *testing.T) {
	repo, st := newRepo(t)
	tip, content := realCommit(t, repo)
	var got record.Change
	require.NoError(t, st.Amend(context.Background(), func(tx *statestore.Txn) error {
		var err error
		got, err = MintIn(tx, Minting{
			ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: tip, Content: content, Destination: record.ToBranch,
			Findings: []plan.Finding{{
				// THE PLANNER'S OWN CONSTANT, not a literal. This test read
				// "instruction" — a word no planner has ever written — and
				// passed for the whole overhaul while every real
				// instruction-comment finding failed the mint. A test that
				// spells the input itself proves the conversion and not the
				// vocabulary; see finding_test.go for the census that does.
				Kind: intent.FindingInstruction, Ports: []string{"jq"}, Source: "Portfile:12", Quote: "# bump oniguruma too",
				Candidates: []plan.Candidate{{Port: "oniguruma6", Reason: "the comment names it"}},
			}},
		}, now)
		return err
	}))
	require.Len(t, got.Findings, 1)
	assert.Equal(t, record.KindInstruction, got.Findings[0].Kind, "the plan's word becomes the note's enum at one site")
	assert.Equal(t, record.Proposed, got.Findings[0].Disposition, "a planner that states nothing has made a question")
	assert.Equal(t, now, got.Findings[0].At)
	require.Len(t, got.Findings[0].Candidates, 1)
	assert.Equal(t, "oniguruma6", got.Findings[0].Candidates[0].Port)
}

func TestMintInRefusesAFindingKindNoBuildCanClassify(t *testing.T) {
	_, st := newRepo(t)
	err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
		_, err := MintIn(tx, Minting{
			ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: "abc", Content: "def", Destination: record.ToBranch,
			Findings: []plan.Finding{{Kind: "something-a-later-build-cannot-read"}},
		}, now)
		return err
	})
	assert.ErrorIs(t, err, ErrUnknownFinding)
}

func TestExtendInMovesTheRecordAndTheRefTogether(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	// The cohort commit: a second commit over the branch's own tip.
	cohort := prepared(t, repo)
	cohort.Summary = "jq: revbump 1 dependent of jq 1.8"
	sha, content, err := Commit(ctx, repo, cohort, c.Tip)
	require.NoError(t, err)

	var got record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		got, err = ExtendIn(tx, Extension{
			ID: "chg-01", ExpectedTip: c.Tip, Tip: sha, Content: content,
			Subjects: []record.Subject{{Port: "oniguruma6", Portdir: "textproc/oniguruma6"}},
		}, now)
		return err
	}))
	assert.Equal(t, record.ChangeExtended, got.State)
	assert.Equal(t, sha, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"))
	require.Len(t, got.Subjects, 2)
	assert.Equal(t, "jq", got.Subjects[0].Port, "the headline stays where it is; a union never renames the change")
}

func TestExtendInRefusesATipAPeerMoved(t *testing.T) {
	repo, st := newRepo(t)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
		_, err := ExtendIn(tx, Extension{ID: "chg-01", ExpectedTip: c.Tip + "x", Tip: "abc", Content: "def"}, now)
		return err
	})
	assert.ErrorIs(t, err, ErrTipMoved)
}

func TestExtendInRefusesAClosedOrBranchlessRecord(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-01", record.ChangeDiscarded, "", now)
	}))
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := ExtendIn(tx, Extension{ID: "chg-01", ExpectedTip: c.Tip, Tip: "abc", Content: "def"}, now)
		return err
	})
	assert.ErrorIs(t, err, ErrNoRecord, "an extension of a closed change is a resurrection")
}

func TestFollowInAssertsTheBranchIsWhereThePersonLeftIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	// A person's own commit on the dockhand branch.
	hand := byHand(t, repo, "dockhand/jq-1.8", c.Tip, "version 1.8.1\n", "by hand")
	tree, err := repo.RevParse(ctx, hand+"^{tree}")
	require.NoError(t, err)

	var got record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		got, err = FollowIn(tx, "chg-01", hand, record.ContentID(tree),
			Provenance{AskedBy: record.Human, Via: record.MintedAdopted}, now)
		return err
	}))
	assert.Equal(t, record.ChangeExtended, got.State)
	assert.Equal(t, hand, got.Tip)
	assert.Equal(t, record.MintedAdopted, got.MintedVia, "a follow is an adoption of a person's work, and says so")
	assert.Equal(t, hand, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"), "the assert line moves nothing")
}

func TestFollowInsAssertLineRefusesABranchThatMovedAgain(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	stale := byHand(t, repo, "", c.Tip, "version 1.8.1\n", "a tip nobody has")

	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := FollowIn(tx, "chg-01", stale, "deadbeef", Provenance{}, now)
		return err
	})
	// Measured on git 2.55: a no-op update whose expected old is wrong is
	// refused, which is what makes an assert line an assertion.
	require.ErrorIs(t, err, git.ErrRefMoved)
	assert.Equal(t, c.Tip, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"))
}

func TestAdoptInAssertsABranchAndPinsASnapshot(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	tip := gittest.Commit(t, repo, "dockhand/hand-made", primary(t, repo), "sysutils/jq/Portfile", "version 2.0\n", "by hand")

	var branchy, snapshot record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		var err error
		branchy, err = AdoptIn(tx, Adoption{
			ID: "chg-01", Branch: "dockhand/hand-made", Tip: tip, Content: "t1",
			Prov: Provenance{AskedBy: record.Human},
		}, now)
		return err
	}))
	assert.Equal(t, record.MintedAdopted, branchy.MintedVia, "a machine may never demolish what this writes")
	assert.Empty(t, branchy.Pin, "a branch keeps a record's tip alive; a pin is for the record that has none")
	assert.Equal(t, tip, refValueOf(t, repo, "refs/heads/dockhand/hand-made"))

	// The branchless half: a working-tree snapshot, pinned by the batch
	// that records it and not one moment earlier.
	dir := repo.Root + "/sysutils/jq"
	sha, content, err := Snapshot(ctx, repo, "chg-02", dir)
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		snapshot, err = AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content}, now)
		return err
	}))
	assert.Equal(t, PinRef("chg-02"), snapshot.Pin, "the record SAYS whether a pin exists (rule 7)")
	assert.Equal(t, sha, refValueOf(t, repo, PinRef("chg-02")))
}

func TestAdoptInRefusesABranchAStandingRecordBinds(t *testing.T) {
	repo, st := newRepo(t)
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Branch: "dockhand/jq-1.8", Tip: "abc", Content: "def"}, now)
		return err
	})
	assert.ErrorIs(t, err, ErrStanding)
}

func TestCloseInIsTheOneWriterOfEveryClosedState(t *testing.T) {
	for _, to := range []record.ChangeState{
		record.ChangeSuperseded, record.ChangeDiscarded, record.ChangePublished, record.ChangeAbandoned,
	} {
		t.Run(string(to), func(t *testing.T) {
			repo, st := newRepo(t)
			minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
			require.NoError(t, st.Amend(context.Background(), func(tx *statestore.Txn) error {
				return CloseIn(tx, "chg-01", to, "", now)
			}))
			state, err := st.Read(context.Background())
			require.NoError(t, err)
			c := state.Changes["chg-01"]
			assert.Equal(t, to, c.State)
			require.NotNil(t, c.Closed)
			assert.False(t, c.Bound())
			assert.Equal(t, "dockhand/jq-1.8", c.Branch, "the record keeps the name it had as history")
			assert.Equal(t, c.Tip, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"),
				"a close does not touch the branch; its deletion is DemolishIn's")
		})
	}
}

func TestCloseInRefusesWhatIsNotAClosedState(t *testing.T) {
	repo, st := newRepo(t)
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	err := st.Amend(context.Background(), func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-01", record.ChangeMinted, "", now)
	})
	assert.ErrorIs(t, err, ErrNotAClosedState)
}

func TestCloseInDeletesThePinItsRecordNames(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	sha, content, err := Snapshot(ctx, repo, "chg-02", repo.Root+"/sysutils/jq")
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content}, now)
		return err
	}))
	require.NotEmpty(t, refValueOf(t, repo, PinRef("chg-02")))

	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-02", record.ChangeDiscarded, "", now)
	}))
	assert.Empty(t, refValueOf(t, repo, PinRef("chg-02")),
		"the pin's life IS the record's open life, so the one writer of every closed state is the one deleter of every pin")
}

func TestCloseInRefusesWhileAPublicationIsOpen(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{ID: "pub-01", Change: "chg-01", Outcome: record.Open})
		return nil
	}))
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-01", record.ChangeDiscarded, "", now)
	})
	require.ErrorIs(t, err, ErrPublicationOpen, "a merged or open pull request outlives the branch that carried it")

	// Settled, and the close proceeds.
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{ID: "pub-01", Change: "chg-01", Outcome: record.Merged})
		return CloseIn(tx, "chg-01", record.ChangePublished, "", now)
	}))
}

func TestCloseInAndSupersedeInRefuseWhatAPeerAlreadyEnded(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-01", record.ChangePublished, "", now)
	}))
	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-01", record.ChangeSuperseded, "chg-02", now)
	}), ErrNotBound, "without this the closure would restamp a Published record Superseded")
	assert.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return SupersedeIn(tx, "chg-01", "chg-02", now)
	}), ErrNotBound)
}

func TestSupersedeInMarksAndLeavesOpen(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return SupersedeIn(tx, "chg-01", "chg-02", now)
	}))
	state, err := st.Read(ctx)
	require.NoError(t, err)
	got := state.Changes["chg-01"]
	assert.Equal(t, c.State, got.State, "the old change stays open with its publication")
	assert.Equal(t, "chg-02", got.SupersededBy)
	assert.False(t, got.Bound(), "the name is released by Bound() reading SupersededBy, not by blanking Branch")
	assert.Equal(t, "dockhand/jq-1.8", got.Branch)
}

func TestTheReplaceClosureIsOneBatch(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	old := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	sha, content, err := Commit(ctx, repo, prepared(t, repo), primary(t, repo))
	require.NoError(t, err)

	// Supersede, demolish the old branch, mint the new one — in that
	// order and in one Amend, so the old name is released before MintIn's
	// ErrStanding check and create line would meet it.
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := CloseIn(tx, "chg-01", record.ChangeSuperseded, "chg-02", now); err != nil {
			return err
		}
		if err := DemolishIn(tx, "chg-01", now); err != nil {
			return err
		}
		_, err := MintIn(tx, Minting{
			ID: "chg-02", Branch: "dockhand/jq-1.9", Tip: sha, Content: content, Destination: record.ToBranch,
		}, now)
		return err
	}))
	assert.Empty(t, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"))
	assert.Equal(t, sha, refValueOf(t, repo, "refs/heads/dockhand/jq-1.9"),
		"one batch: the old branch's deletion and the new branch's creation land together")
	state, err := st.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "chg-02", state.Changes[string(old.ID)].SupersededBy)
	assert.False(t, state.Changes[string(old.ID)].Bound())
	assert.True(t, state.Changes["chg-02"].Bound())
}

func TestASameNameReplaceIsOneCoalescedLine(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	// A second commit for the same branch name — the shipped slug carries
	// the version, so a same-version bump re-mints the same name.
	next := byHand(t, repo, "", primary(t, repo), "version 1.8\n# again\n", "again")
	tree, err := repo.RevParse(ctx, next+"^{tree}")
	require.NoError(t, err)

	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := CloseIn(tx, "chg-01", record.ChangeSuperseded, "chg-02", now); err != nil {
			return err
		}
		if err := DemolishIn(tx, "chg-01", now); err != nil {
			return err
		}
		_, err := MintIn(tx, Minting{
			ID: "chg-02", Branch: "dockhand/jq-1.8", Tip: next, Content: record.ContentID(tree),
			Destination: record.ToBranch,
		}, now)
		return err
	}), "git refuses two lines for one ref in a batch; the store coalesces the delete and the create")
	assert.Equal(t, next, refValueOf(t, repo, "refs/heads/dockhand/jq-1.8"))
}

func TestDemolishInRefusesABoundChangeAndABranchlessOne(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return DemolishIn(tx, "chg-01", now)
	}), ErrNotClosed, "no road can delete a branch the store still calls live")

	sha, content, err := Snapshot(ctx, repo, "chg-02", repo.Root+"/sysutils/jq")
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content}, now)
		return err
	}))
	assert.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := CloseIn(tx, "chg-02", record.ChangeDiscarded, "", now); err != nil {
			return err
		}
		return DemolishIn(tx, "chg-02", now)
	}), ErrBranchless, "a snapshot's pin is already CloseIn's; there is no branch line to queue")
}

func TestDemolishInRefusesTheWholeBatchWhenTheBranchMovedUnderIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	// A person's commit on the branch between the road's read and the
	// Amend: the delete line's expected value is the record's Tip.
	byHand(t, repo, "dockhand/jq-1.8", c.Tip, "version 1.8.2\n", "mine")

	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := CloseIn(tx, "chg-01", record.ChangeDiscarded, "", now); err != nil {
			return err
		}
		return DemolishIn(tx, "chg-01", now)
	})
	require.ErrorIs(t, err, git.ErrRefMoved)
	state, err := st.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.ChangeMinted, state.Changes["chg-01"].State,
		"the close is refused with the deletion: a discard never deletes work it did not resolve")
}

func TestPinLostInLetsTheCloseQueueNoLineForADeletedPin(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	sha, content, err := Snapshot(ctx, repo, "chg-02", repo.Root+"/sysutils/jq")
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content}, now)
		return err
	}))
	// A hand deletes the pin. Measured on git 2.55: a delete line for an
	// absent ref refuses the WHOLE batch, so the close has to be told.
	plant(t, repo, "update-ref", "-d", PinRef("chg-02"))

	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, "chg-02", record.ChangeDiscarded, "", now)
	}), git.ErrRefMoved)

	d := &TipDisagreement{ID: "chg-02", Ref: PinRef("chg-02"), Recorded: sha, Absent: true}
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := PinLostIn(tx, "chg-02", d, now); err != nil {
			return err
		}
		return CloseIn(tx, "chg-02", record.ChangeDiscarded, "", now)
	}))
	state, err := st.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, state.Changes["chg-02"].Pin)
	assert.Equal(t, sha, state.Changes["chg-02"].Tip, "Tip is left as it was: the record still names the commit")
}

func TestPinLostInReportsAMovedPinAndNeverWritesOverIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	sha, content, err := Snapshot(ctx, repo, "chg-02", repo.Root+"/sysutils/jq")
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content}, now)
		return err
	}))
	moved := &TipDisagreement{ID: "chg-02", Ref: PinRef("chg-02"), Recorded: sha, Found: "abc"}
	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return PinLostIn(tx, "chg-02", moved, now)
	}), ErrRefStands)
	assert.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return PinLostIn(tx, "chg-02", nil, now)
	}), ErrRefStands)
}

func TestAnswerInIsTheOneWriterOfEveryDisposition(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return ProposeIn(tx, "chg-01", record.Finding{
			Kind: record.KindABIDependents, Criterion: "install name moved",
			Candidates: []record.Candidate{{Port: "mise", Proposed: true}},
		}, now)
	}))

	seated := []record.Candidate{{Port: "mise", Proposed: true, Reason: "as seated"}}
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return AnswerIn(tx, "chg-01", record.KindABIDependents, record.Accepted, seated, now)
	}))
	state, err := st.Read(ctx)
	require.NoError(t, err)
	f := state.Changes["chg-01"].Findings[0]
	assert.Equal(t, record.Accepted, f.Disposition)
	assert.Equal(t, "as seated", f.Candidates[0].Reason, "the record carries what was actually seated")

	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return AnswerIn(tx, "chg-01", record.KindABIDependents, record.Dismissed, nil, now)
	}), ErrNoProposal, "an answer is given once")
	assert.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return AnswerIn(tx, "chg-01", record.KindABIDependents, record.Proposed, nil, now)
	}), ErrNotAnAnswer)
}

func TestProposeInReplacesAnUnansweredProposalAndNeverAnAnsweredOne(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	propose := func(criterion string) {
		require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
			return ProposeIn(tx, "chg-01", record.Finding{Kind: record.KindABIDependents, Criterion: criterion}, now)
		}))
	}
	propose("first measurement")
	propose("second measurement")
	state, err := st.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Changes["chg-01"].Findings, 1, "a later proposal supersedes an earlier one nobody answered")
	assert.Equal(t, "second measurement", state.Changes["chg-01"].Findings[0].Criterion)

	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return AnswerIn(tx, "chg-01", record.KindABIDependents, record.Dismissed, nil, now)
	}))
	propose("third measurement")
	state, err = st.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.Dismissed, state.Changes["chg-01"].Findings[0].Disposition, "an answer given is not re-asked")
	assert.Equal(t, "second measurement", state.Changes["chg-01"].Findings[0].Criterion)
}

func TestHoldInAndUnholdIn(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return HoldIn(tx, "chg-01", "waiting on upstream", record.OwnerID{Host: "here"}, now)
	}))
	state, err := st.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Changes["chg-01"].Hold)
	assert.Equal(t, record.HoldPerson, state.Changes["chg-01"].Hold.Origin)

	require.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return HoldIn(tx, "chg-01", "again", record.OwnerID{}, now)
	}), ErrHeld, "a second hold would delete a person's sentence with nothing said")

	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return UnholdIn(tx, "chg-01", now)
	}))
	assert.ErrorIs(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return UnholdIn(tx, "chg-01", now)
	}), ErrNotHeld)
}

func TestHoldInReplacesACrossingsHold(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	tip, content := realCommit(t, repo)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := MintIn(tx, Minting{
			ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: tip, Content: content,
			Crossing: record.StableToPrerelease, Destination: record.ToBranch,
		}, now)
		return err
	}))
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return HoldIn(tx, "chg-01", "I want to read it first", record.OwnerID{}, now)
	}))
	state, err := st.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.HoldPerson, state.Changes["chg-01"].Hold.Origin,
		"a person's hold withholds strictly more, and the reason a person typed is the one status should show")
	assert.Equal(t, "I want to read it first", state.Changes["chg-01"].Hold.Reason)

	// And `unhold` lifts a born hold, which is what makes the typed origin
	// the shape that matches the surface.
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		return UnholdIn(tx, "chg-01", now)
	}))
}

func TestEveryMutatorRefusesAnIdTheStateDoesNotHold(t *testing.T) {
	_, st := newRepo(t)
	ctx := context.Background()
	for name, call := range map[string]func(*statestore.Txn) error{
		"ExtendIn": func(tx *statestore.Txn) error {
			_, err := ExtendIn(tx, Extension{ID: "nope", ExpectedTip: "a", Tip: "b", Content: "c"}, now)
			return err
		},
		"FollowIn": func(tx *statestore.Txn) error {
			_, err := FollowIn(tx, "nope", "a", "b", Provenance{}, now)
			return err
		},
		"CloseIn":     func(tx *statestore.Txn) error { return CloseIn(tx, "nope", record.ChangeDiscarded, "", now) },
		"SupersedeIn": func(tx *statestore.Txn) error { return SupersedeIn(tx, "nope", "chg-02", now) },
		"DemolishIn":  func(tx *statestore.Txn) error { return DemolishIn(tx, "nope", now) },
		"AnswerIn": func(tx *statestore.Txn) error {
			return AnswerIn(tx, "nope", record.KindABIDependents, record.Accepted, nil, now)
		},
		"ProposeIn": func(tx *statestore.Txn) error {
			return ProposeIn(tx, "nope", record.Finding{Kind: record.KindInstruction}, now)
		},
		"HoldIn":    func(tx *statestore.Txn) error { return HoldIn(tx, "nope", "", record.OwnerID{}, now) },
		"UnholdIn":  func(tx *statestore.Txn) error { return UnholdIn(tx, "nope", now) },
		"PinLostIn": func(tx *statestore.Txn) error { return PinLostIn(tx, "nope", nil, now) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, st.Amend(ctx, call), ErrNoRecord)
		})
	}
}

// A CLOSED RECORD DOES NOT CLAIM A PIN THAT CLOSING IT DELETED.
// record.Change.Pin is a field rather than a rule precisely so that "the
// record has to SAY whether a pin exists", and CloseIn queued the ref's
// delete line while leaving the field set — so the record said one
// existed, about a ref this very function had removed.
//
// change.Resolve believes the field. It looked the ref up, found
// nothing, and answered ErrTipDisagrees: a foreign hand moved something.
// Measured in the field: after `dockhand discard` of a snapshot, every
// later `dockhand verify <port>` for that port exited 45 forever, never
// reaching the adopt road at all.
func TestClosingASnapshotStopsTheRecordClaimingItsPin(t *testing.T) {
	repo, store := newRepo(t)
	ctx := t.Context()
	const id record.ChangeID = "chg-snap"

	// A REAL PIN, because CloseIn's delete line is a compare-and-set: the
	// ref must stand at the recorded tip or the whole batch refuses, and
	// a fixture that skipped it would prove nothing about the road.
	tip, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	require.NoError(t, store.Amend(ctx, func(tx *statestore.Txn) error {
		tx.PutChange(record.Change{
			ID: id, State: record.ChangeMinted, Tip: tip, Content: "tree-1",
			Pin: PinRef(id), Destination: record.ToVerdict,
		})
		return tx.Ref(PinRef(id), tip, "")
	}))
	require.NoError(t, store.Amend(ctx, func(tx *statestore.Txn) error {
		return CloseIn(tx, id, record.ChangeDiscarded, "", time.Now())
	}))

	st, err := store.Read(ctx)
	require.NoError(t, err)
	c := st.Changes[string(id)]
	require.True(t, c.State.Closed(), "the change is closed")
	assert.Empty(t, c.Pin,
		"the pin's life IS the record's open life; the field must not outlive the ref")

	// And the ref really is gone, so the field and the world agree.
	_, rerr := repo.RevParse(ctx, PinRef(id))
	assert.Error(t, rerr, "closing deletes the pin it stops claiming")
}
