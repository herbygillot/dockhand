package run

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder every fixture opens with. These tests drive a real
// repository through a real statestore for the reason lease's and
// statestore's own tests do: what the mutators prove is an ordering of
// writes against a compare-and-set, and a fake store would prove
// something about the fake.
var tools = tool.NewFinder(nil)

var sequoia = func() platform.Release {
	r, _ := platform.ByName("Sequoia")
	return r
}()

// newFixture is a ports-tree-shaped repository with no state ref yet,
// and the note ledger over the same repository. The two come back
// together because Finish writes the state and then exports the note
// from it, and a test that opened two repositories would be proving
// something about neither.
func newFixture(t *testing.T) (*statestore.Store, *ledger.Ledger) {
	t.Helper()
	repo := gittest.PortsTree(t, tools)
	return statestore.Open(repo), ledger.Open(repo)
}

func newStore(t *testing.T) *statestore.Store {
	t.Helper()
	st, _ := newFixture(t)
	return st
}

func owner() record.OwnerID {
	return record.OwnerID{Root: "/w/ports", Host: "mac", PID: 100, Since: clock}
}

// plantChange and plantAttempt write another lifecycle's records
// straight into the store. A fixture reaching for PutChange is exactly
// what rule 4 forbids in production code and exactly what a fixture must
// do here: this package needs a change to exist in order to prove what
// it does about the attempts hanging off one.
func plantChange(t *testing.T, st *statestore.Store, c record.Change) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutChange(c)
		return nil
	}))
}

func plantAttempt(t *testing.T, st *statestore.Store, a record.Attempt) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutAttempt(a)
		return nil
	}))
}

func specOf(ports ...string) Spec {
	s := Spec{Content: "sha256:c", Platform: sequoia}
	for _, p := range ports {
		s.Roster = append(s.Roster, member(p))
	}
	return s
}

// AN ATTEMPT MUST NAME THE COMMIT IT BUILDS. The pre-mint gate that had
// no sha is deleted, and an attempt no drain could materialize is a row
// that waits forever.
func TestEnqueueInRefusesAnAttemptWithNothingToBuild(t *testing.T) {
	st := newStore(t)
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		_, err := EnqueueIn(tx, Enqueue{Change: "chg", Content: "sha256:c", Spec: specOf("jq")}, clock)
		return err
	})
	require.ErrorIs(t, err, ErrNoSha)

	// Nothing landed at all: the refusal aborts the closure before any
	// commit, so this repository has never had a state ref.
	_, rerr := st.Read(t.Context())
	assert.ErrorIs(t, rerr, statestore.ErrNoState)
}

// A QUEUED ATTEMPT CARRIES THE ASK THE ENQUEUER MADE, so the drain that
// starts it hours later honours the person's --test and --keep-env and
// not the dispatcher's flags.
func TestEnqueueInCarriesTheAskAndBothIdentities(t *testing.T) {
	st := newStore(t)
	var got record.Attempt
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		var err error
		got, err = EnqueueIn(tx, Enqueue{
			Change: "chg-1", Sha: "cafe", Content: "sha256:c",
			Spec: specOf("jq"), Platform: sequoia,
			Ask: record.Ask{Test: true, KeepEnv: true}, EnqueuedBy: owner(),
		}, clock)
		return err
	}))

	assert.True(t, got.Queued())
	assert.Equal(t, record.Requested, got.Phase)
	assert.Empty(t, got.Lease, "a queued attempt has no environment")
	assert.True(t, got.Ask.Test)
	assert.True(t, got.Ask.KeepEnv)
	assert.Equal(t, owner(), got.EnqueuedBy)
	assert.Equal(t, owner(), got.Owner, "the enqueuer holds it until something starts it")
	assert.Equal(t, []string{"jq"}, got.Members())
	assert.NotEmpty(t, got.Spec, "the adoption key is computed at enqueue")

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Len(t, s.Attempts, 1)
}

// THE SPEC ID COVERS WHAT WAS VERIFIED AND NOT HOW A CALLER WATCHED IT.
// Putting a per-invocation flag in it would mean `bump --verify --trace`
// could not adopt the attempt `bump --verify` left running.
func TestSpecIDIgnoresHowACallerWatched(t *testing.T) {
	base := specOf("jq")
	watched := base
	watched.KeepEnv, watched.Trace = true, true
	assert.Equal(t, base.ID(), watched.ID())
}

// AND IT COVERS EVERYTHING THAT CHANGES WHAT WAS VERIFIED.
func TestSpecIDMovesWithEveryAskThatChangesTheQuestion(t *testing.T) {
	base := specOf("jq")
	for name, mutate := range map[string]func(Spec) Spec{
		"content":     func(s Spec) Spec { s.Content = "sha256:other"; return s },
		"platform":    func(s Spec) Spec { s.Platform, _ = platform.ByName("Sonoma"); return s },
		"test":        func(s Spec) Spec { s.Test = true; return s },
		"from source": func(s Spec) Spec { s.FromSource = []string{"jq"}; return s },
		"roster":      func(s Spec) Spec { s.Roster = append(s.Roster, member("gdal")); return s },
		"withheld":    func(s Spec) Spec { s.Withheld = []Withheld{{Port: "gdal", Why: "conflicts"}}; return s },
		"requires":    func(s Spec) Spec { s.Requires = [][]string{nil, {"jq"}}; return s },
		"forced":      func(s Spec) Spec { s.Roster[0].Forced = "gdal"; return s },
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, base.ID(), mutate(specOf("jq")).ID())
		})
	}
}

// THE STAGED PATH IS OUTSIDE THE KEY. A drain re-deriving the spec from
// the attempt hours later stages into a fresh temp root, and a key that
// included a path would match nothing across processes.
func TestSpecIDIsBlindToWhereAMemberWasStaged(t *testing.T) {
	here := specOf("jq")
	elsewhere := specOf("jq")
	elsewhere.Roster[0].Portdir = "/var/folders/T/dockhand-9182/stage/lang/jq"
	assert.Equal(t, here.ID(), elsewhere.ID())
}

// ADOPTION IS BY IDENTITY, AND A ZERO IDENTITY ADOPTS NOTHING: a caller
// holding a zero SpecID must not take over somebody else's build by
// accident.
func TestAdoptableRefusesAZeroIdentityOnEitherSide(t *testing.T) {
	standing := record.Attempt{ID: "a-1", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sequoia", Phase: record.Requested}
	pool := []record.Attempt{standing}

	_, ok := Adoptable(pool, "", "spec-1", sequoia, clock)
	assert.False(t, ok)
	_, ok = Adoptable(pool, "sha256:c", "", sequoia, clock)
	assert.False(t, ok)
	_, ok = Adoptable([]record.Attempt{{ID: "a-2", Phase: record.Requested}}, "sha256:c", "spec-1", sequoia, clock)
	assert.False(t, ok, "an attempt with no identity of its own matches nothing")

	got, ok := Adoptable(pool, "sha256:c", "spec-1", sequoia, clock)
	require.True(t, ok)
	assert.Equal(t, "a-1", got.ID)
}

// A FAILED ATTEMPT IS NOT ADOPTABLE. A person re-asking for a
// verification of a tip that failed is asking for it again; handing them
// the failure they already have would make the verb unable to retry.
func TestAdoptableTakesAPassOverAnOpenOneAndNeverAFailure(t *testing.T) {
	failed := record.Attempt{ID: "a-failed", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sequoia", Phase: record.Finished, Started: clock,
		Runs: map[string]record.Run{"jq": {State: record.Failed}}}
	open := record.Attempt{ID: "a-open", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sequoia", Phase: record.Requested, Started: clock.Add(time.Minute)}
	pass := record.Attempt{ID: "a-pass", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sequoia", Phase: record.Finished, Started: clock,
		Runs: map[string]record.Run{"jq": {State: record.Passed}}}

	_, ok := Adoptable([]record.Attempt{failed}, "sha256:c", "spec-1", sequoia, clock)
	assert.False(t, ok)

	got, ok := Adoptable([]record.Attempt{failed, open, pass}, "sha256:c", "spec-1", sequoia, clock)
	require.True(t, ok)
	assert.Equal(t, "a-pass", got.ID, "an earned verdict costs nothing and beats a build in flight")
}

// A COHORT WITH ONE FAILED MEMBER IS NOT A PROOF OF THE CHANGE, so it is
// not a verdict a re-queue may inherit.
func TestAdoptableRefusesACohortThatOnlyPartlyPassed(t *testing.T) {
	partial := record.Attempt{ID: "a-part", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sequoia", Phase: record.Finished, Started: clock,
		Runs: map[string]record.Run{"libwidget": {State: record.Passed}, "gdal": {State: record.Failed}}}
	_, ok := Adoptable([]record.Attempt{partial}, "sha256:c", "spec-1", sequoia, clock)
	assert.False(t, ok)
}

// A DIFFERENT PLATFORM IS A DIFFERENT QUESTION: a pass on Sonoma is not
// evidence about Sequoia.
func TestAdoptableWillNotCrossPlatforms(t *testing.T) {
	sonoma, _ := platform.ByName("Sonoma")
	standing := record.Attempt{ID: "a-1", Content: "sha256:c", Spec: "spec-1",
		Platform: "Sonoma", Phase: record.Requested}
	_, ok := Adoptable([]record.Attempt{standing}, "sha256:c", "spec-1", sequoia, clock)
	assert.False(t, ok)
	_, ok = Adoptable([]record.Attempt{standing}, "sha256:c", "spec-1", sonoma, clock)
	assert.True(t, ok)
}

// THE ROSTER IS READ OFF THE RECORD, so the bump that queued a cohort,
// the drain and a later verify all seat the guest identically — and a
// forced member goes LAST, with the sibling it deactivates named on it.
func TestRosterSeatsAWithheldMemberOutAndAForcedMemberLast(t *testing.T) {
	c := record.Change{
		ID: "chg-1",
		Subjects: []record.Subject{
			{Port: "libwidget", Names: []string{"libwidget"}},
			{Port: "gdal", Names: []string{"gdal"}},
			{Port: "gdal-devel"},
		},
		Findings: []record.Finding{{
			Kind: record.KindABIDependents, Disposition: record.Accepted,
			Candidates: []record.Candidate{
				{Port: "gdal", Proposed: true},
				{Port: "gdal-devel", Proposed: true, Solo: true, Over: "gdal",
					Reason: "conflicts with gdal — bumped here, and not built"},
			},
		}},
	}

	members, held := Roster(c, record.Attempt{})
	require.Len(t, members, 2)
	assert.Equal(t, "libwidget", members[0].Port, "the headline leads")
	assert.Equal(t, []string{"libwidget"}, members[0].Names)
	require.Len(t, held, 1)
	assert.Equal(t, "gdal-devel", held[0].Port)
	assert.Contains(t, held[0].Why, "conflicts with gdal")

	// The same records with the person's override: the member is seated
	// last and carries the sibling the guest deactivates before it.
	c.Findings[0].Candidates[1].Forced = true
	members, held = Roster(c, record.Attempt{})
	require.Len(t, members, 3)
	assert.Empty(t, held)
	assert.Equal(t, "gdal-devel", members[2].Port)
	assert.Equal(t, "gdal", members[2].Forced)
}

// A MEMBER WHOSE PORTDIR IS NOT KNOWN HERE IS NOT A DEFECT: this reads
// records, and the staged path is the Stager's, joined by port at start.
// That split is what lets a re-derived SpecID equal the enqueued one.
func TestRosterNamesNoStagedPath(t *testing.T) {
	c := record.Change{ID: "chg", Subjects: []record.Subject{{Port: "jq"}}}
	members, _ := Roster(c, record.Attempt{})
	require.Len(t, members, 1)
	assert.Empty(t, members[0].Portdir)
}

// EVERY QUEUED ATTEMPT OF A CLOSING CHANGE IS FINISHED IN THE SAME
// AMEND, or they count against the queue cap for the life of the
// repository — and a drain would stage a demolished change's tip by sha
// and boot a guest for it.
func TestWithdrawInFinishesEveryQueuedAttemptOfOneChange(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-queued", Change: "chg-1", Sha: "cafe",
		Phase: record.Requested, Roster: record.Roster{Seats: []record.Seat{{Port: "jq"}}}})
	live := record.Attempt{ID: "a-live", Change: "chg-1", Sha: "cafe",
		Phase: record.Active, Lease: "req-1", Roster: record.Roster{Seats: []record.Seat{{Port: "jq"}}}}
	plantAttempt(t, st, live)
	plantAttempt(t, st, record.Attempt{ID: "a-other", Change: "chg-2", Sha: "beef",
		Phase: record.Requested, Roster: record.Roster{Seats: []record.Seat{{Port: "gdal"}}}})

	var withdrawn []record.Attempt
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		withdrawn = WithdrawIn(tx, "chg-1", record.InterruptCanceled, owner(), clock)
		return nil
	}))

	require.Len(t, withdrawn, 1, "only the queued ones; live work is Finish's, through the judge")
	assert.Equal(t, "a-queued", withdrawn[0].ID)

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	got := s.Attempts["a-queued"]
	assert.True(t, got.Settled())
	require.NotNil(t, got.Interrupt)
	assert.Equal(t, record.InterruptCanceled, got.Interrupt.Why)
	assert.Equal(t, record.Canceled, got.Runs["jq"].State)
	assert.Equal(t, record.Active, s.Attempts["a-live"].Phase, "a running build is not withdrawn")
	assert.True(t, s.Attempts["a-other"].Queued(), "another change's queue is untouched")

	assert.Equal(t, 2, Count(s).Attempts(),
		"the withdrawn attempt stops counting against MaxQueued; the live one and the other change's still do")
}

// THE TYPED CAUSE DECIDES THE WORD. A supersede and a discard are
// different things to read on a record months later, and nothing
// recovers either by reading a sentence.
func TestWithdrawInWritesTheStateItsTypedCauseNames(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, minted("chg-1"))
	plantAttempt(t, st, record.Attempt{ID: "a-1", Change: "chg-1", Sha: "cafe",
		Phase: record.Requested, Roster: record.Roster{Seats: []record.Seat{{Port: "jq"}}}})

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		WithdrawIn(tx, "chg-1", record.InterruptSuperseded, owner(), clock)
		return nil
	}))

	s, err := st.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, record.InterruptSuperseded, s.Attempts["a-1"].Interrupt.Why)
	assert.Equal(t, record.Superseded, s.Attempts["a-1"].Runs["jq"].State)
}
