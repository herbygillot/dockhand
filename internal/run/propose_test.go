package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/artifact"
	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// local is the seam app implements over the reverse index and the
// maintainer's Portfile cues. run holds neither, which is why it is an
// interface — and why a test can hand the propose step its answers.
type local struct {
	rows   []portindex.Dependent
	unread []portindex.Unread
	err    error
	quotes []dependents.Instruction
	asked  []string
	read   []string
}

func (l *local) Dependents(_ context.Context, port string) ([]portindex.Dependent, []portindex.Unread, error) {
	l.asked = append(l.asked, port)
	return l.rows, l.unread, l.err
}

func (l *local) Instructions(_ context.Context, sha, portdir string) ([]dependents.Instruction, error) {
	l.read = append(l.read, sha+":"+portdir)
	return l.quotes, nil
}

// manifest is a one-library installation, so the delta below has
// something to compare.
func manifest(version, install string) *artifact.Manifest {
	return &artifact.Manifest{
		Port:    "libwidget",
		Version: version,
		Dylibs: []artifact.Dylib{{
			Path: "/opt/local/lib/" + install, InstallName: "/opt/local/lib/" + install,
			CompatVersion: "1.0.0", CurrentVersion: "1.0.0",
		}},
	}
}

func passedEvidence(t *testing.T) (Evidence, Judgment) {
	t.Helper()
	e := evidenceOf([]Member{member("libwidget")}, verify.Status{State: verify.Passed}, "built\n")
	e.Spec.Roster[0].Portdir = "/stage/libwidget"
	e.Manifests = map[string]Manifests{"libwidget": {
		Baseline:  manifest("1.0", "libwidget.2.dylib"),
		Candidate: manifest("1.1", "libwidget.3.dylib"),
		Source:    "archive",
	}}
	return e, Judge(e)
}

func dependentRow(name string) portindex.Dependent {
	return portindex.Dependent{Name: name, Portdir: "graphics/" + name, Keys: []string{"depends_lib"}}
}

// THE PROPOSAL RESTS ON A MEASUREMENT AND ON DECLARED DEPENDENTS, and it
// is made once, when the evidence is fresh.
func TestProposeCohortProposesWhereTheInterfaceMoved(t *testing.T) {
	e, j := passedEvidence(t)
	lo := &local{rows: []portindex.Dependent{dependentRow("gdal")}}

	f, ok, err := proposeCohort(t.Context(), lo, stateWith(nil, nil), minted("chg-1"), settledOn("libwidget"), e, j)
	require.NoError(t, err)
	require.True(t, ok, "an install name that moved is what a revbump rests on")
	assert.Equal(t, record.KindABIDependents, f.Kind)
	assert.Equal(t, record.Proposed, f.Disposition, "a finding proposes and never executes")
	assert.Equal(t, []string{"gdal"}, f.Ports)
	assert.NotEmpty(t, f.Criterion, "the measurement is stated in words a reader can check")
	assert.Equal(t, []string{"libwidget"}, lo.asked, "the headline is what is measured")
}

// A HEADLINE THAT DID NOT PASS IS NOT MEASURED. A comparison against the
// installation a failed build left behind measures the change against a
// half-finished tree, and the proposal on it would ask a person to
// revbump the world over a build that never worked.
func TestProposeCohortDoesNothingForAHeadlineThatFailed(t *testing.T) {
	e := evidenceOf([]Member{member("libwidget")}, verify.Status{State: verify.Failed},
		"Error: Failed to build libwidget: boom\n")
	lo := &local{rows: []portindex.Dependent{dependentRow("gdal")}}

	_, ok, err := proposeCohort(t.Context(), lo, stateWith(nil, nil), minted("chg-1"), settledOn("libwidget"), e, Judge(e))
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, lo.asked, "the index is not even read")
}

// A PROPOSAL A PERSON HAS ALREADY ANSWERED IS NOT MADE AGAIN. The
// cohort's own verification settles against the same content, so asking
// again is asking somebody twice.
func TestProposeCohortDoesNotReaskAnAnsweredProposal(t *testing.T) {
	e, j := passedEvidence(t)
	c := minted("chg-1")
	c.Findings = []record.Finding{{Kind: record.KindABIDependents, Disposition: record.Dismissed}}

	_, ok, err := proposeCohort(t.Context(), &local{rows: []portindex.Dependent{dependentRow("gdal")}},
		stateWith(nil, nil), c, settledOn("libwidget"), e, j)
	require.NoError(t, err)
	assert.False(t, ok)
}

// AN INDEX THAT COULD NOT BE READ RECORDS NOTHING AND SAYS SO. Rule 7:
// no finding is not a finding of "no dependents", and a fresh checkout
// with no PortIndex must not read identically to a leaf port.
func TestProposeCohortRefusesRatherThanConcludingNoDependents(t *testing.T) {
	e, j := passedEvidence(t)
	boom := errors.New("no PortIndex in this tree")

	_, ok, err := proposeCohort(t.Context(), &local{err: boom}, stateWith(nil, nil), minted("chg-1"), settledOn("libwidget"), e, j)
	require.ErrorIs(t, err, boom)
	assert.False(t, ok)
}

// A PORT NOTHING DEPENDS ON IS NEVER MEASURED: the measurement's one
// consumer is the cohort decision, and a finding on every bump in the
// tree would be a record nobody reads.
func TestProposeCohortSaysNothingWhereNothingDependsOnTheHeadline(t *testing.T) {
	e, j := passedEvidence(t)
	_, ok, err := proposeCohort(t.Context(), &local{}, stateWith(nil, nil), minted("chg-1"), settledOn("libwidget"), e, j)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A DEPENDENT ALREADY IN FLIGHT IS EXAMINED AND LEFT OUT: two branches
// revbumping one port is two revisions and a conflict at merge. The
// in-flight fact is read off the STATE, and a change never reads ITSELF
// as in flight.
func TestProposeCohortExcludesADependentAnotherChangeIsAlreadyCarrying(t *testing.T) {
	e, j := passedEvidence(t)
	other := minted("chg-2")
	other.Subjects = []record.Subject{{Port: "gdal"}}

	f, ok, err := proposeCohort(t.Context(), &local{rows: []portindex.Dependent{dependentRow("gdal")}},
		stateWith([]record.Change{minted("chg-1"), other}, nil), minted("chg-1"), settledOn("libwidget"), e, j)
	require.NoError(t, err)
	assert.False(t, ok, "the one candidate is excluded, so there is nothing to put forward")
	assert.Empty(t, f.Ports)
}

// A CHANGE DOES NOT PROPOSE REVBUMPING WHAT IT ALREADY CARRIES. The
// members of a cohort are subjects of the change being settled, so a
// second pass over the same content would propose bumping ports this
// very commit has already bumped.
func TestProposeCohortExcludesTheMembersTheChangeAlreadyCarries(t *testing.T) {
	e, j := passedEvidence(t)
	c := minted("chg-1")
	c.Subjects = []record.Subject{{Port: "libwidget"}, {Port: "gdal"}}

	_, ok, err := proposeCohort(t.Context(), &local{rows: []portindex.Dependent{dependentRow("gdal")}},
		stateWith([]record.Change{c}, nil), c, settledOn("libwidget"), e, j)
	require.NoError(t, err)
	assert.False(t, ok)
}

// settledOn is the settled attempt proposeCohort reads its commit from:
// the cues come out of git at the attempt's sha, not off a host path.
func settledOn(port string) record.Attempt {
	return record.Attempt{ID: "att-1", Change: "chg-1", Sha: "cafe",
		Roster: record.Roster{Seats: []record.Seat{{Port: port}}}}
}

// THE CUES COME FROM THE COMMIT, NOT FROM THE WORKING DIRECTORY.
//
// Instructions used to take a host path, and the path handed to it on
// every settlement that replays a frozen roster is EMPTY — run.Roster
// seats members with a port and names and no portdir — so
// os.ReadFile(portdir + "/Portfile") read whatever Portfile was under
// the process's working directory, and the error was discarded.
func TestProposeCohortReadsTheCuesAtTheAttemptsCommit(t *testing.T) {
	e, j := passedEvidence(t)
	lo := &local{rows: []portindex.Dependent{dependentRow("gdal")}}
	c := minted("chg-1")
	c.Subjects = []record.Subject{{Port: "libwidget", Portdir: "devel/libwidget"}}

	_, _, err := proposeCohort(t.Context(), lo, stateWith(nil, nil), c, settledOn("libwidget"), e, j)
	require.NoError(t, err)
	assert.Equal(t, []string{"cafe:devel/libwidget"}, lo.read,
		"the attempt's sha and the change's own tree-relative portdir")
}

// THE ANALYSIS IS REPLAYABLE FROM THE RECORD. Its evidence survives
// settlement on record.Run, so a failure needs somewhere to be recorded
// and a pass that picks it up — which is what it never had.
func TestAnalyseRetriesFromTheSettledRecordAndClearsWhatItOwed(t *testing.T) {
	st := newStore(t)
	c := changeOf("chg-1", "libwidget")
	c.Subjects[0].Portdir = "devel/libwidget"
	plantChange(t, st, c)

	until := clock.Add(-time.Minute) // the backoff has passed
	a := record.Attempt{ID: "att-1", Change: "chg-1", Sha: "cafe", Phase: record.Finished,
		Roster:   record.Roster{Seats: []record.Seat{{Port: "libwidget"}}},
		Analysis: &record.Analysis{Phase: record.Uncertain, At: clock, Attempts: 1, NotBefore: &until},
		Runs: map[string]record.Run{"libwidget": {
			State: record.Passed, At: clock,
			Baseline: manifest("1.0", "libwidget.2.dylib"),
			Manifest: manifest("1.1", "libwidget.3.dylib"),
		}},
	}
	plantAttempt(t, st, a)

	owed := Owed(current(t, st), clock)
	require.Len(t, owed, 1, "a settled attempt past its backoff is owed")

	lo := &local{rows: []portindex.Dependent{dependentRow("gdal")}}
	require.NoError(t, Analyse(t.Context(), st, lo, owed[0], func() time.Time { return clock }))

	after := current(t, st)
	require.NotNil(t, after.Attempts["att-1"].Analysis)
	assert.Equal(t, record.Finished, after.Attempts["att-1"].Analysis.Phase)
	assert.False(t, after.Attempts["att-1"].AnalysisOwed())
	assert.Empty(t, Owed(after, clock), "and it is no longer picked up")

	// The proposal the retry produced is on the change, which is the
	// whole point of retrying.
	require.Len(t, after.Changes["chg-1"].Findings, 1)
	assert.Equal(t, record.KindABIDependents, after.Changes["chg-1"].Findings[0].Kind)
}

// A retry that fails again backs off further rather than being re-walked
// on every tick, which is the same shape Release and Step carry.
func TestAnalyseBacksOffWhenItFailsAgain(t *testing.T) {
	st := newStore(t)
	plantChange(t, st, changeOf("chg-1", "libwidget"))
	until := clock.Add(-time.Minute)
	plantAttempt(t, st, record.Attempt{ID: "att-1", Change: "chg-1", Sha: "cafe", Phase: record.Finished,
		Roster:   record.Roster{Seats: []record.Seat{{Port: "libwidget"}}},
		Analysis: &record.Analysis{Phase: record.Uncertain, At: clock, Attempts: 1, NotBefore: &until},
		Runs: map[string]record.Run{"libwidget": {
			State: record.Passed, At: clock,
			Baseline: manifest("1.0", "libwidget.2.dylib"),
			Manifest: manifest("1.1", "libwidget.3.dylib"),
		}},
	})

	lo := &local{err: errors.New("the reverse index will not build")}
	owed := Owed(current(t, st), clock)
	require.Len(t, owed, 1)
	require.NoError(t, Analyse(t.Context(), st, lo, owed[0], func() time.Time { return clock }))

	an := current(t, st).Attempts["att-1"].Analysis
	require.NotNil(t, an)
	assert.Equal(t, record.Uncertain, an.Phase, "still owed")
	assert.Equal(t, 2, an.Attempts)
	require.NotNil(t, an.NotBefore)
	assert.Equal(t, clock.Add(10*time.Minute), *an.NotBefore, "and it doubles")
	assert.Empty(t, Owed(current(t, st), clock), "not re-walked before the backoff passes")
}

// current is the store as it now stands, for the assertions above.
func current(t *testing.T, st *statestore.Store) statestore.State {
	t.Helper()
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	return s
}
