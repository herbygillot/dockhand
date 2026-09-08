package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// tools is the finder every fixture opens with. These tests drive a real
// repository through a real statestore for the reason statestore's own
// tests do: what is being proven here is an ordering of writes against a
// compare-and-set, and a fake store would prove something about the
// fake.
var tools = tool.NewFinder(nil)

// sequoia is a concrete platform, because a lease's slot is a (change,
// platform) pair and a zero release would make every test share one.
var sequoia = func() platform.Release {
	r, _ := platform.ByName("Sequoia")
	return r
}()

const platformName = "Sequoia"

// newStore is a ports-tree-shaped repository with no state ref yet.
func newStore(t *testing.T) *statestore.Store {
	t.Helper()
	return statestore.Open(gittest.PortsTree(t, tools))
}

// me is this checkout's identity, with a PID and a start time that the
// scripted process table below agrees is alive.
func me() record.OwnerID {
	return record.OwnerID{Root: "/w/ports", Host: "mac", PID: 100, Since: birth}
}

var birth = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// now is a fixed clock. Every deadline in this package is a comparison
// between two recorded instants, so a test that read the wall clock
// would be asserting about the moment it happened to run.
var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) func() time.Time { return func() time.Time { return now.Add(d) } }

func claimant(o record.OwnerID) Claimant {
	return Claimant{Owner: o, Expires: now.Add(time.Hour), Pass: "pass-1"}
}

// plantChange and plantAttempt write another lifecycle's records
// straight into the store.
//
// A fixture reaching for PutChange and PutAttempt is exactly what rule 4
// forbids in production code, and it is what a fixture must do here:
// this package needs a change to be closed and an attempt to be building
// in order to prove what it does about either, and the packages that own
// those mutators are being built beside this one. A test file is out of
// the census's scope for the same reason (statestore's own AST walk skips
// _test.go, and says why).
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

// leaseOn reads back the one live lease for a slot, which is what every
// assertion here is about.
func leaseOn(t *testing.T, st *statestore.Store, change record.ChangeID) record.Lease {
	t.Helper()
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	l, ok := liveOn(s, change, platformName)
	require.True(t, ok, "no live lease on %s", Slot(change, platformName))
	return l
}

func request(id string) verify.Request {
	return verify.Request{ID: id, Ports: []string{"jq"}, Platform: sequoia}
}

// The slot is the change identity and the platform, and never a sha. A
// change's sha moves under Extend while its identity does not, so a
// lease claimed at one tip and addressed at another would leave an
// obligation standing forever.
func TestASlotIsNamedForTheChangeAndThePlatform(t *testing.T) {
	assert.Equal(t, "chg-1/Sequoia", Slot("chg-1", "Sequoia"))
	assert.NotEqual(t, Slot("chg-1", "Sequoia"), Slot("chg-1", "Sonoma"),
		"one environment per platform, so the platform is part of the name")
}

// RULE 3, THE HEADLINE CASE. The lease exists before the provider is
// asked for anything, so a crash in the window leaves a record with a
// request token that recovery can resolve rather than an anonymous VM
// and nothing to look for.
//
// It is proven by a provider that READS THE STORE from inside Submit:
// nothing else can distinguish "written first" from "written in the
// same breath".
func TestAcquireWritesTheLeaseBeforeTheProviderIsCalled(t *testing.T) {
	st := newStore(t)
	var seen record.Lease
	var found bool
	prov := &spy{Fake: &verifytest.Fake{}, onSubmit: func(req verify.Request) {
		s, err := st.Read(t.Context())
		require.NoError(t, err)
		seen, found = liveOn(s, "chg-1", platformName)
		assert.Equal(t, req.ID, seen.Request, "the record names the id the provider was handed")
	}}

	l, err := Acquire(t.Context(), st, prov, "chg-1", request(""), claimant(me()), at(0))
	require.NoError(t, err)

	require.True(t, found, "the provider was called before anything was written down")
	assert.Equal(t, record.Requested, seen.Phase, "written Requested: asked and not yet answered")
	assert.Empty(t, seen.Handle, "there is no handle yet — that is the whole of the window")
	assert.Equal(t, me(), seen.Owner)
	assert.Equal(t, l.Request, seen.Request)
	assert.Equal(t, record.Active, l.Phase, "what Acquire hands back is the answered lease")
	assert.Equal(t, "fake", l.ID.Provider)
}

// The request token is minted here and stamped over whatever the caller
// put in the request: it is the store's key for the document and the
// name the provider carries into the worker, and two spellings of one
// identity is a guest nothing can join.
func TestAcquireMintsTheRequestTokenItself(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{}
	_, err := Acquire(t.Context(), st, fake, "chg-1", request("caller-chose-this"), claimant(me()), at(0))
	require.NoError(t, err)

	require.Len(t, fake.Submitted, 1)
	assert.NotEqual(t, "caller-chose-this", fake.Submitted[0].ID)
	assert.Equal(t, leaseOn(t, st, "chg-1").Request, fake.Submitted[0].ID,
		"one token: the document's name and the provider's id are the same string")
}

// verify.Request.Owner had NO PRODUCER at all until Acquire stamped it:
// the field was documented, tart carried it into its attribution
// sidecar, and every submission left it empty — so no guest on any
// machine was attributable to anything, verify.Worker.Owner was
// permanently "", and every ownership question about a guest was
// unanswerable. A contract with no producer is not a policy anybody
// chose.
func TestAcquireStampsTheOwningCheckoutOnTheRequest(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{}
	_, err := Acquire(t.Context(), st, fake, "chg-1", request("r"), claimant(me()), at(0))
	require.NoError(t, err)

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, me().Root, fake.Submitted[0].Owner,
		"the ROOT and not the whole OwnerID: a guest outlives the process that made it, so the durable question is which checkout")
}

// TWO ACQUIRERS, ONE SLOT. The refusal is inside the Amend, so it is
// under the store's flock and compare-and-set and it lands BEFORE the
// provider call — which is the property Slot's name promises and the
// store's request-token key cannot give a filename.
func TestAcquireRefusesASecondEnvironmentForOneSlot(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{}
	_, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(0))
	require.NoError(t, err)

	_, err = Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(time.Minute))
	require.ErrorIs(t, err, ErrSlotTaken)
	assert.Len(t, fake.Submitted, 1, "the refusal came before the provider was asked")

	// The same change on another platform is another slot, and one guest
	// per platform is the point.
	_, err = Acquire(t.Context(), st, fake, "chg-1",
		verify.Request{Ports: []string{"jq"}, Platform: sonoma()}, claimant(me()), at(time.Minute))
	require.NoError(t, err)
	assert.Len(t, fake.Submitted, 2)
}

func sonoma() platform.Release {
	r, _ := platform.ByName("Sonoma")
	return r
}

// A SATURATED MACHINE MUST NOT LEAVE A PHANTOM PER REFUSAL. The
// no-vacancy refusal asserts that nothing was created, so the lease
// written moments earlier is retired Absent in the same pass rather than
// standing for a LookupRequest round trip on every queued attempt on
// every tick.
func TestACapacityRefusalClosesTheLeaseItJustWrote(t *testing.T) {
	st := newStore(t)
	full := &verify.NoVacancyError{Busy: 2, Limit: 2}
	fake := &verifytest.Fake{SubmitErr: full}

	_, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(0))
	require.ErrorIs(t, err, full)

	s, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	require.Len(t, s.Leases, 1)
	for _, l := range s.Leases {
		assert.True(t, l.Returned(), "the refusal is confirmed absence, and confirmed absence is done")
		assert.Equal(t, record.Finished, l.Phase)
	}
	_, live := liveOn(s, "chg-1", platformName)
	assert.False(t, live, "nothing is outstanding, so the slot is free again")
}

// EVERY OTHER SUBMIT FAILURE LEAVES THE LEASE STANDING, and that is the
// difference the capacity contract buys. A timeout does not prove a VM
// was not created, so the record stays Requested and only the provider
// can say what became of it.
func TestASubmitThatMightHaveCreatedSomethingLeavesTheLeaseStanding(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{SubmitErr: errors.New("i/o timeout")}

	_, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(0))
	require.Error(t, err)

	l := leaseOn(t, st, "chg-1")
	assert.Equal(t, record.Requested, l.Phase)
	assert.False(t, l.Returned(), "nothing was confirmed, so nothing is closed")
}

// ActiveIn is the lease's half of run.Start's one Amend. It is a
// compare-and-set on the phase: a discharge that decided the submit had
// been stranded and retired the lease must not be overwritten by an
// answer obtained before that decision was made.
func TestActiveInRefusesALeaseSomebodyElseAlreadyMoved(t *testing.T) {
	st := newStore(t)
	fake := &verifytest.Fake{}
	l, err := Acquire(t.Context(), st, fake, "chg-1", request(""), claimant(me()), at(0))
	require.NoError(t, err)

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		assert.True(t, ActiveIn(tx, l), "the lease is still the Requested one this process wrote")
		return nil
	}))
	assert.Equal(t, record.Active, leaseOn(t, st, "chg-1").Phase)

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		assert.False(t, ActiveIn(tx, l), "it is not Requested any more; somebody moved it")
		return nil
	}))
}

// The claim and the completion are two fields, which is the whole split
// this package exists for: Requested without Done is work owed, and it
// is exactly the state a crash between the two leaves behind.
func TestAClaimIsNotACompletion(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")

	l, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	require.True(t, took)
	assert.True(t, l.Owed(), "claimed and not finished")
	assert.False(t, l.Returned())
	assert.Equal(t, now, leaseOn(t, st, "chg-1").Release.Requested)

	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Released, "", now.Add(time.Second)))
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, s.Leases, 1)
	for _, done := range s.Leases {
		assert.True(t, done.Returned(), "only the provider's answer writes Done")
		assert.Equal(t, record.Finished, done.Phase)
	}
}

// A subject still building in the environment stops both roads into a
// claim. This is the shipped ReleaseJob's idle check, and it is why a
// concurrent pass cannot delete a running build's VM.
func TestAClaimIsRefusedWhileASubjectIsStillBuilding(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	plantAttempt(t, st, record.Attempt{ID: "att-1", Change: "chg-1", Platform: platformName,
		Sha: "deadbeef", Phase: record.Active, Lease: "lease-token"})

	_, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	assert.False(t, took, "the guest is in use; the release is not free to take")
}

// One claimant at a time. A second process meeting an obligation
// somebody else took is told no, which is what stops two dockhands each
// reading "finished" and each handing the same guest back.
func TestAClaimAnotherProcessHoldsIsRefusedAndOurOwnIsRetaken(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	peer := me()
	peer.PID = 200

	_, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(peer), now)
	require.NoError(t, err)
	require.True(t, took)

	_, took, err = Request(t.Context(), st, "chg-1", platformName, claimant(me()), now.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, took, "somebody else is releasing it")

	// Our own standing obligation is a retry and not a second claimant.
	_, took, err = Request(t.Context(), st, "chg-1", platformName, claimant(peer), now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, took)
}

// A repository copied to another machine must not stop a VM it does not
// own, and the rule holds on the ordinary road and not only in the
// reclaim stage: without this guard `cancel` over a shared checkout
// would walk straight past it.
func TestAForeignCheckoutsLeaseIsNotOursToClaim(t *testing.T) {
	st := newStore(t)
	other := me()
	other.Root = "/elsewhere/ports"
	heldBy(t, st, "chg-1", other)

	_, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	assert.False(t, took)
}

// THE ADVERSARIAL FINDING, AS A TEST. run.Finish takes the claim inside
// its own Amend, so Release's first act finds it held and calls no
// provider — one release claimed and zero performed. Fulfil is the
// second half on its own, and it is what performs it.
func TestReleaseOverAClaimAlreadyTakenPerformsNothingAndFulfilPerformsIt(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	fake := &verifytest.Fake{}

	// The settle Amend: the claim is taken beside a verdict, through the
	// transaction step that exists for it.
	var claimed record.Lease
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		var took bool
		claimed, took = RequestIn(tx, "chg-1", platformName, claimant(me()), now)
		require.True(t, took)
		return nil
	}))

	require.NoError(t, Release(t.Context(), st, fake, "chg-1", platformName, claimant(peerOf(me())), at(time.Minute)))
	assert.Empty(t, fake.Released, "Release found the claim held and asked the provider nothing")
	assert.True(t, leaseOn(t, st, "chg-1").Owed(), "and the obligation is still standing")

	require.NoError(t, Fulfil(t.Context(), st, fake, claimed, at(time.Minute)))
	assert.Equal(t, []string{"fake-1"}, fake.Released, "exactly once")
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	for _, l := range s.Leases {
		assert.True(t, l.Returned())
	}
}

func peerOf(o record.OwnerID) record.OwnerID { o.PID = 999; return o }

// Fulfil never claims. A caller holding a lease it did not claim is a
// caller performing somebody else's obligation, and the Confirm on the
// far side would find nothing to write against — so the guest would be
// destroyed and the record would still say it was there.
func TestFulfilRefusesALeaseNobodyClaimed(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	fake := &verifytest.Fake{}

	err := Fulfil(t.Context(), st, fake, leaseOn(t, st, "chg-1"), at(0))
	require.ErrorIs(t, err, ErrNotOurs)
	assert.Empty(t, fake.Released, "nothing was destroyed")
}

// Release is the whole sequence for a caller that has not pre-claimed,
// and the provider call sits BETWEEN the two transactions: a store the
// provider can read from inside Release is a store whose lock is not
// held across the call.
func TestReleaseCallsTheProviderOutsideTheLock(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	read := false
	prov := &spy{Fake: &verifytest.Fake{}, onRelease: func(verify.Job) {
		_, err := st.Read(t.Context())
		require.NoError(t, err, "the store is readable, so no lock is held across the provider call")
		read = true
	}}

	require.NoError(t, Release(t.Context(), st, prov, "chg-1", platformName, claimant(me()), at(0)))
	assert.True(t, read)
	assert.True(t, leaseOn2(t, st, "chg-1").Returned())
}

// leaseOn2 reads a lease that may already be returned, which leaseOn
// refuses to find.
func leaseOn2(t *testing.T, st *statestore.Store, change record.ChangeID) record.Lease {
	t.Helper()
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	for _, l := range s.Leases {
		if l.Change == change {
			return l
		}
	}
	t.Fatalf("no lease for %s", change)
	return record.Lease{}
}

// THE THREE OUTCOMES HAVE THREE RECOVERIES, which is why there are three
// and not two. Absence is the one that matters: without it an
// environment somebody else already deleted is owed forever.
func TestClassifyTellsAbsenceFromFailure(t *testing.T) {
	out, detail := classify(nil)
	assert.Equal(t, Released, out)
	assert.Empty(t, detail)

	out, detail = classify(errors.New("verify: unknown job: x"))
	assert.Equal(t, Failed, out, "an unrecognized sentence is not an answer")
	assert.NotEmpty(t, detail)

	out, _ = classify(verify.ErrUnknownJob)
	assert.Equal(t, Absent, out)

	// The machine facts have confirmed nothing, and reading either as
	// absence would retire a lease for a VM that is still running.
	for _, err := range []error{verify.ErrNoProvider, verify.ErrNoEnvironment, errors.New("boom")} {
		out, _ = classify(err)
		assert.Equal(t, Failed, out, "%v", err)
	}
}

// An Outcome nobody filled in changes nothing. Released was the zero
// once, so a struct built and not populated read as "the provider let it
// go" and wrote Done — the same defect this package exists to remove,
// in better clothes.
func TestTheZeroOutcomeWritesNothing(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	_, took, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	require.True(t, took)

	before := leaseOn(t, st, "chg-1")
	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Unconfirmed, "", now.Add(time.Hour)))
	after := leaseOn(t, st, "chg-1")
	assert.Equal(t, before.Release, after.Release, "nothing was recorded")
	assert.False(t, after.Returned())
}

// A refused release writes a backoff, so a resident pass at five-minute
// cadence does not retry a refusing provider every tick.
func TestAFailedReleaseBacksOffRatherThanRetryingEveryTick(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	_, _, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)

	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Failed, "tart is busy", now))
	l := leaseOn(t, st, "chg-1")
	assert.True(t, l.Owed(), "still owed: a failure closes nothing")
	assert.Equal(t, 1, l.Release.Attempts)
	assert.Equal(t, "tart is busy", l.Release.LastError)
	require.NotNil(t, l.Release.NotBefore)
	assert.Equal(t, now.Add(5*time.Minute), *l.Release.NotBefore, "the first retry is the next pass and no sooner")

	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Failed, "tart is busy", now))
	l = leaseOn(t, st, "chg-1")
	assert.Equal(t, 2, l.Release.Attempts)
	assert.Equal(t, now.Add(10*time.Minute), *l.Release.NotBefore, "and it doubles")

	assert.Equal(t, time.Hour, retryAfter(99), "capped, so a broken provider is not paid for forever")
	assert.Equal(t, 5*time.Minute, retryAfter(0), "an attempt count nobody set still waits a pass")
}

// A done release has no error outstanding and nothing waiting: the
// backoff belongs to the obligation, and the obligation is over. How
// many tries it took survives, because that is history rather than a
// live fault.
func TestASucceedingReleaseClearsWhatTheFailuresLeft(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	_, _, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Failed, "tart is busy", now))
	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Released, "", now.Add(time.Hour)))

	l := leaseOn2(t, st, "chg-1")
	require.True(t, l.Returned())
	assert.Nil(t, l.Release.NotBefore)
	assert.Empty(t, l.Release.LastError)
	assert.Equal(t, 1, l.Release.Attempts, "how many tries it took is worth keeping")
}

// A Confirm over a lease nobody claimed writes nothing. Confirm is the
// second half of a claim, and writing Done over an unclaimed lease would
// retire an environment nobody had taken responsibility for.
func TestConfirmOverAnUnclaimedLeaseWritesNothing(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	require.NoError(t, Confirm(t.Context(), st, "chg-1", platformName, Released, "", now))
	assert.True(t, leaseOn(t, st, "chg-1").Held(), "still held, and still nobody's obligation")
}

// RetainIn is the deadline run.Finish writes on a Keep, in the same
// Amend as the verdict that kept the guest. It is refused on a lease
// that is already on its way back: a deadline for keeping something
// nobody is keeping is not a fact.
func TestRetainInWritesTheDeadlineOnlyOnAHeldLease(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		RetainIn(tx, "chg-1", platformName, now.Add(KeepFor))
		return nil
	}))
	l := leaseOn(t, st, "chg-1")
	require.NotNil(t, l.Retain)
	assert.Equal(t, now.Add(24*time.Hour), *l.Retain)

	_, _, err := Request(t.Context(), st, "chg-1", platformName, claimant(me()), now)
	require.NoError(t, err)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		RetainIn(tx, "chg-1", platformName, now.Add(72*time.Hour))
		return nil
	}))
	assert.Equal(t, now.Add(24*time.Hour), *leaseOn(t, st, "chg-1").Retain, "unchanged")
}

// HandleIn records the provider's own name for the environment once a
// Status reports one — a different fact from the job, arriving from a
// different call, which is why it is a step of its own.
func TestHandleInRecordsTheProvidersOwnName(t *testing.T) {
	st := newStore(t)
	held(t, st, "chg-1")
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		HandleIn(tx, "chg-1", platformName, "dockhand-worker-abc")
		HandleIn(tx, "chg-1", platformName, "")
		return nil
	}))
	assert.Equal(t, "dockhand-worker-abc", leaseOn(t, st, "chg-1").Handle)
}

// held plants a lease in the ordinary way — through Acquire — so a
// fixture cannot invent a shape the real road never produces.
func held(t *testing.T, st *statestore.Store, change record.ChangeID) record.Lease {
	t.Helper()
	return heldBy(t, st, change, me())
}

func heldBy(t *testing.T, st *statestore.Store, change record.ChangeID, owner record.OwnerID) record.Lease {
	t.Helper()
	fake := &verifytest.Fake{}
	// An hour ago, so a fixture is past any cutoff a test chooses without
	// every test having to say so. The moment a lease was taken is what
	// its obligations are aged from.
	l, err := Acquire(t.Context(), st, fake, change, request(""), claimant(owner), at(-time.Hour))
	require.NoError(t, err)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		require.True(t, ActiveIn(tx, l))
		return nil
	}))
	return leaseOn(t, st, change)
}

// spy is a Fake with a hook on the calls whose ORDERING is what a test
// is proving. It embeds rather than reimplements, so every other part of
// the contract is the fake's and stays in one place.
type spy struct {
	*verifytest.Fake
	onSubmit  func(verify.Request)
	onRelease func(verify.Job)
}

func (s *spy) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	if s.onSubmit != nil {
		s.onSubmit(req)
	}
	return s.Fake.Submit(ctx, req)
}

func (s *spy) Release(ctx context.Context, job verify.Job) error {
	if s.onRelease != nil {
		s.onRelease(job)
	}
	return s.Fake.Release(ctx, job)
}

var _ verify.Verifier = (*spy)(nil)
