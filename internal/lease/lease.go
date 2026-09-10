// Package lease owns the environment lifecycle: what this checkout has
// been handed by a verification provider, and what it owes back. It is
// a separate package from run because holding an environment and
// judging a build are different obligations with different recovery,
// and today they share a single boolean.
//
// EVERY PROVIDER CALL IN THIS PACKAGE SITS OUTSIDE EVERY LOCK AND
// OUTSIDE EVERY AMEND CLOSURE, and that is the one rule to read the
// file by. statestore.Amend takes a repository-wide flock and retries
// its closure on a lost race, so an effect inside one would be
// performed twice on a retry and would stall every peer sharing the
// checkout for as long as the slowest provider takes. The shape that
// falls out of it is the same three steps everywhere: a transaction
// that takes responsibility, the call, and a transaction that records
// what the call said. The shipped tree's ReleaseJob wrote the claim and
// the completion as one bool, and two of its three call sites made the
// provider call while still holding the notes lock.
package lease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Claimant identifies the checkout, host and process taking
// responsibility, so that a second pass looking at the same state can
// tell an obligation it may retry from one somebody else is
// discharging — and, on its own host, can tell a live claimant from a
// dead one.
//
// A Phase alone cannot say that. "Requested" is what a live acquirer
// and a process killed one instruction later both look like, so the
// claim carries an owner and an expiry and same-host recovery checks
// whether the PID is still alive before taking over — comparing the
// process's START TIME as well as its number, because PIDs are reused
// and a number alone eventually matches a stranger.
type Claimant struct {
	Owner   record.OwnerID
	Expires time.Time
	// Pass is the pass token a cycle stamps onto record.Claim.Pass; empty
	// for a verb that is not a pass. A report field (see Standing).
	Pass string
}

// Slot is the document name a lease is claimed under. It is derived
// from the CHANGE IDENTITY and the platform rather than a fresh id or
// a sha — a sha would leave a pre-mint gate with no slot to claim, which
// is exactly the case the state ref exists for,
// because compare-and-set on a shared ref excludes concurrent WRITERS
// and not concurrent CLAIMANTS: two acquirers writing two different
// filenames both succeed and both get an environment. One deterministic
// name per (change, platform) is what makes them collide.
//
// Everything below is keyed the same way, and a draft of this design
// re-keyed only this function. Every operation that ADDRESSED a slot
// still took a sha, so the pre-mint case the re-key exists for could not
// claim, discharge or hand back the slot Slot() had named for it — and
// on the ordinary road a change's sha moves under Extend while its
// identity does not, so a lease claimed at one tip was addressed at
// another, the compare-and-set found nothing, and the obligation stood
// Owed forever: retried by every pass, never closed, never compacted.
//
// WHERE THE COLLISION ACTUALLY HAPPENS IS Acquire, NOT THE FILENAME, and
// that is this design's one departure from the sketch that wrote this
// paragraph. The store keys a lease document by its Request token —
// statestore.State says so and gives the reason: the token is the one
// name a lease is guaranteed to have, because it is what recovery joins
// a provider's inventory on and it exists before the provider is called.
// A slot-shaped filename cannot also be that, and "/" is not a legal
// entry name in the flat tree besides. So the property this function's
// name promises is enforced where it can be enforced with the same
// force: Acquire refuses inside its Amend when the state it was handed
// already holds a live lease for the slot (ErrSlotTaken), and the
// refusal lands BEFORE the provider call, under the store's own flock
// and compare-and-set. Two acquirers still collide; they collide on a
// predicate instead of on a filename. What Slot remains is the canonical
// spelling of the pair — one string a report, a log line or a record
// field can carry — and nothing splits it back apart (record.RunKey is
// the standing reminder of why).
func Slot(change record.ChangeID, platform string) string {
	return string(change) + "/" + platform
}

// ErrNotOurs is Fulfil refusing to perform a release over a lease whose
// claim this caller does not hold. It is the refusal Fulfil's doc
// describes: a caller holding a lease it did not claim is a caller
// performing somebody else's obligation, and the Confirm on the far side
// of the provider call would find no claim to write against — so the
// guest would be destroyed and the record would still say it was there.
//
// The same rule refused earlier and without an error is RequestIn's
// foreign-root guard: another checkout's lease is not this one's to
// claim in the first place.
var ErrNotOurs = errors.New("lease: claimed by another checkout")

// ErrSlotTaken is Acquire refusing to boot a second environment for a
// (change, platform) this checkout already holds one for. See Slot: it
// is the collision that a filename cannot make, made by a predicate
// inside the same compare-and-set instead.
var ErrSlotTaken = errors.New("lease: this change already holds an environment on this platform")

// Request takes responsibility for handing an environment back. It is a
// compare-and-set on the state ref and it performs no provider I/O,
// which is the whole point: today ReleaseJob writes the claim and the
// completion as one bool, and two of its three call sites make the
// provider call while still holding the repository-wide notes lock.
//
// It returns false when there is nothing to claim: the lease is gone,
// somebody else is releasing it, or a subject is still building.
func Request(ctx context.Context, st *statestore.Store, change record.ChangeID, platform string, by Claimant, now time.Time) (record.Lease, bool, error) {
	var l record.Lease
	var took bool
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		l, took = RequestIn(tx, change, platform, by, now)
		return nil
	})
	if err != nil {
		return record.Lease{}, false, err
	}
	return l, took, nil
}

// RequestIn is Request's body as a transaction step, so that an owner of
// another lifecycle can take the release obligation in the SAME state-ref
// update as its own write. run.Finish is the caller that needs it, and
// without it the design's flagship atomic write had to reach into these
// records directly — advancing a lifecycle it does not own, which rule 4
// forbids.
//
// The pattern is the rule: a cross-lifecycle write is two OWNED mutators
// in one transaction, each exported by the package that owns its kind.
//
// The one refusal that is not obvious from the doc above: an obligation
// ALREADY claimed by another owner is not re-claimable here, and that is
// what "somebody else is releasing it" means. Our own standing claim is
// re-taken rather than refused — a retry of our own obligation is not a
// second claimant — and a claim held by an owner this pass has decided
// is dead is Discharge's to take, through the seizure road below, where
// the decision has a name and a Standing behind it.
func RequestIn(tx *statestore.Txn, change record.ChangeID, platform string, by Claimant, now time.Time) (record.Lease, bool) {
	l, ok := liveOn(tx.State(), change, platform)
	if !ok {
		return record.Lease{}, false
	}
	if l.Owner.Root != by.Owner.Root {
		// Another checkout's environment, met on the ordinary road rather
		// than in the reclaim stage. It is refused for the reason a
		// ForeignRoot obligation is never seized: a repository copied to
		// another machine must not stop a VM it does not own, and the peer
		// whose record claims it as a kept failure cannot see us take it.
		// Without this guard the rule would hold in `cycle` and nowhere
		// else — `cancel` over a shared checkout would walk straight past
		// it.
		return record.Lease{}, false
	}
	if l.Owed() && !sameOwner(l.Owner, by.Owner) {
		return record.Lease{}, false
	}
	return claimIn(tx, l, by, now)
}

// claimIn is the write both roads share: RequestIn's, which asks whether
// the claim is free, and Discharge's, which has already decided that it
// is this pass's to take. Splitting them is rule 1 — the question of WHO
// MAY is the caller's, and this is only the effect — and it is what lets
// a restarted dispatcher discharge its predecessor's leases without
// RequestIn growing a liveness check it has no business making.
//
// The guard it does NOT delegate is the one about the build: a subject
// still running in the environment stops both roads, because a release
// that raced a live build would take the guest out from under it. That
// was the shipped ReleaseJob's `idle` check, and it is the reason a
// concurrent pass can no longer delete a running build's VM.
//
// Owner MOVES to the claimant. Whoever owes the handback is who is
// accountable for the lease's remaining life, which is the same rule
// record.Attempt states for its own Owner ("Owner moves when a
// dispatcher takes over a queued attempt"), and it is what makes
// Standing answerable at all: an Owed obligation's liveness question is
// about the process that took the release, not about the one that took
// the guest months of passes ago.
func claimIn(tx *statestore.Txn, l record.Lease, by Claimant, now time.Time) (record.Lease, bool) {
	if building(tx.State(), l.Change, l.Platform) {
		return record.Lease{}, false
	}
	if l.Release == nil {
		l.Release = &record.Release{Requested: now, By: spell(by.Owner)}
	} else {
		// Our own standing obligation, re-taken: the attempt count and the
		// last error are the backoff's history and survive, because a
		// retry that reset them would retry a refusing provider at full
		// speed forever.
		l.Release.By = spell(by.Owner)
	}
	l.Owner = by.Owner
	// A FRESH TOKEN ON EVERY CLAIM, including a re-take of our own
	// standing obligation. That is what makes it a fence: a pass whose
	// provider reply arrives after a peer re-claimed the same lease
	// presents a token the record no longer carries, and Confirm writes
	// nothing rather than completing somebody else's claim.
	l.Claim = &record.Claim{By: spell(by.Owner), At: now, Expires: by.Expires, Pass: by.Pass, Token: mintClaim()}
	tx.PutLease(l)
	return l, true
}

// Outcome is what the provider said about a release, and it has three
// values rather than two because the recoveries differ. An earlier
// draft of this design said a failed attempt is simply Owed with a
// higher count, "because the recovery is identical". It is not:
// an environment that is merely busy should be retried, and one the
// provider confirms is already gone is DONE — leaving it Owed means it
// is retried forever and every pass pays for the failure again.
type Outcome uint8

const (
	// Unconfirmed is the zero value, and it exists because a mechanical
	// rule-14 check found that this enum did not have one. Released was
	// the zero, so an Outcome nobody filled in — a struct built and not
	// populated, a classify that fell through — read as "the provider let
	// it go" and would write Done on the lease. That is D-for-D the defect
	// this entire package exists to remove: today ledger.ReleaseJob writes
	// Released = true BEFORE the provider is asked for anything. Rewriting
	// it with the same zero-value hazard would have been the same bug in
	// better clothes.
	//
	// Unconfirmed never writes Done and never closes an obligation.
	Unconfirmed Outcome = iota
	Released            // the provider let it go
	Absent              // the provider confirms it does not exist: also done
	Failed              // transport or busy: still owed, retry
)

// Confirm records the outcome of the provider call. Released and Absent
// both write Done; Failed leaves the obligation standing and records
// the attempt, so the next reconciliation sees work owed rather than a
// lease that claims to have been returned.
func Confirm(ctx context.Context, st *statestore.Store, c Claimed, out Outcome, detail string, now time.Time) error {
	return st.Amend(ctx, func(tx *statestore.Txn) error {
		ConfirmIn(tx, c, out, detail, now)
		return nil
	})
}

// Claimed is a claim this caller holds: WHICH lease, and WHICH claim on
// it. Every completion presents one, and the only way to obtain one is
// claimOf over a record.Lease that a claim road handed back.
//
// It is a type rather than two string parameters because the two are a
// pair and Go will silently accept them transposed. That is not a
// hypothetical: the first cut of this fence took (request, token) as
// bare strings, and the existing tests — written against the older
// slot-addressed spelling, (change, platform) — kept COMPILING and
// silently confirmed nothing. A value that can only be built from a
// lease cannot be assembled out of whatever strings are in scope.
type Claimed struct {
	Request string
	Token   string
}

// claimOf is the only constructor: the lease's own key and the token of
// the claim currently on it. A lease with no claim yields a zero Token,
// which ConfirmIn refuses — Confirm is the second half of a claim, and a
// caller holding an unclaimed lease is performing an obligation nobody
// took.
func claimOf(l record.Lease) Claimed {
	c := Claimed{Request: l.Request}
	if l.Claim != nil {
		c.Token = l.Claim.Token
	}
	return c
}

// ConfirmIn is Confirm as a transaction step, for the same reason
// RequestIn exists.
//
// It writes nothing at all in three cases, and each is deliberate. An
// Outcome nobody filled in (Unconfirmed) changes nothing, which is what
// a refusing zero has to mean here — the alternative is a value that
// closes an obligation by default. A lease that is gone or already
// returned has nothing to record against. And a lease with NO release
// claimed is not this call's to finish: Confirm is the second half of a
// claim, and writing Done over an unclaimed lease would retire an
// environment nobody had taken responsibility for.
func ConfirmIn(tx *statestore.Txn, c Claimed, out Outcome, detail string, now time.Time) bool {
	l, ok := tx.State().Leases[c.Request]
	if !ok || l.Returned() || l.Release == nil {
		return false
	}
	if l.Claim == nil || c.Token == "" || l.Claim.Token != c.Token {
		// THE FENCE. The claim this completion belongs to is not the claim
		// the record carries: a peer seized the lease in the window
		// between our claim and the provider's reply, and completing it
		// here would write Done over a claim we do not hold — retiring an
		// environment on the strength of somebody else's responsibility.
		// Nothing is written and the caller is told, so a pass reports the
		// obligation as standing rather than as discharged.
		return false
	}
	switch out {
	case Unconfirmed:
		return false
	case Released, Absent:
		done := now
		l.Release.Done = &done
		// Nothing is owed any more, so nothing waits and nothing is
		// outstanding: the backoff and the last failure go with the
		// obligation they belonged to. Attempts stays — how many tries a
		// handback took is history a person may want, not a live fault.
		l.Release.NotBefore = nil
		l.Release.LastError = ""
		l.Phase = record.Finished
	case Failed:
		l.Release.Attempts++
		l.Release.LastError = detail
		until := now.Add(retryAfter(l.Release.Attempts))
		l.Release.NotBefore = &until
	}
	tx.PutLease(l)
	return true
}

// retryAfter is how long a refused release waits before the next pass
// tries it again: five minutes, doubling, capped at an hour.
//
// The floor is the resident dispatcher's own cadence — record.Release
// states the problem as "a resident pass at five-minute cadence does not
// retry a refusing provider every tick" — so the first retry is the next
// pass and no sooner. The ceiling is there because the commonest reason
// a provider refuses is that a person is using the machine, and an hour
// is short enough that the slot comes back on its own and long enough
// that a broken provider is not being paid for every tick until someone
// notices.
func retryAfter(attempts int) time.Duration {
	const base, ceiling = 5 * time.Minute, time.Hour
	wait := base
	for i := 1; i < attempts && wait < ceiling; i++ {
		wait *= 2
	}
	return min(wait, ceiling)
}

// Release is the whole sequence for a caller that has NOT pre-claimed:
// claim, call the provider outside the lock, record the outcome. The
// provider call sits BETWEEN the two transactions and not inside either,
// because Amend's closure is retried on a lost race and an effect inside
// it would run twice.
//
// WHO CALLS IT: the release stage on Verify, Accept, Cancel and Discard,
// over a FINISHED attempt's KEPT environment — a failure's debug guest,
// or a pass that asked --keep-env — where the verdict stands and only the
// slot comes back; Cycle's close stage over the kept leases of a change
// the forge has finished with, BEFORE demolish, so a merged change never
// leaves a Held lease behind under a ChangeID the tree no longer holds;
// and lease.Discharge's retry of an Owed or Due obligation. It is NOT
// what run.Finish calls, and a draft of this design said it was. Finish
// claims the release INSIDE its own Amend, through RequestIn, beside the
// verdict — that one atomicity is the whole reason RequestIn exists —
// and Release's first act is Request, which finds the release already
// claimed and returns false, so the provider was never called and the
// environment stayed on the machine with an obligation that read as
// somebody else's. Finish calls Fulfil, below, which is this function's
// second half on its own.
func Release(ctx context.Context, st *statestore.Store, prov verify.Verifier, change record.ChangeID, platform string, by Claimant, now func() time.Time) error {
	lease, took, err := Request(ctx, st, change, platform, by, now())
	if err != nil || !took {
		return err
	}
	return Fulfil(ctx, st, prov, lease, now)
}

// Fulfil is the second half of a release for a caller that ALREADY holds
// the claim: the provider call outside the lock, then Confirm. It exists
// because run.Finish takes the claim inside the same Amend as the
// verdict (lease.RequestIn), and there was then no exported lease
// function that would perform the provider effect over a claim already
// taken — Release refuses (Request finds the claim held), and run
// calling prov.Release itself would be the environment lifecycle's one
// provider effect performed by the package that does not own it, rule 4
// broken by the design's flagship write. An adversarial pass found the
// sequence "Settle writes RequestIn; lease.Release hands back; Confirm
// records" claimed one release three times and performed it zero.
//
// It is called Fulfil and not Discharge, which a first fix proposed:
// Discharge already exists and is Cycle's obligations road, and one
// verb for "perform a claim I hold" and "retry what a pass owes" would
// be two questions under one name. A claim is REQUESTED, then FULFILLED.
//
// It takes the record.Lease Request/RequestIn returned rather than
// re-reading it, so the handle it releases is the one the claim was
// taken over. It never claims: a caller holding a lease it did not claim
// is a caller performing somebody else's obligation, and Confirm will
// write against a claim that is not this process's.
//
// WHO CALLS IT: run.Finish, once, after its settle Amend returns and
// before it exports the note; and lease.Discharge for a Requested lease
// it has just resolved through LookupRequest.
func Fulfil(ctx context.Context, st *statestore.Store, prov verify.Verifier, l record.Lease, now func() time.Time) error {
	if l.Release == nil {
		// The lease handed in carries no claim, so this caller did not take
		// one: it is about to perform a release nobody is accountable for,
		// and the Confirm afterwards would find nothing to write against
		// and silently succeed over a guest that had just been destroyed.
		return fmt.Errorf("%w: no release is claimed on %s", ErrNotOurs, Slot(l.Change, l.Platform))
	}
	_, err := fulfil(ctx, st, prov, l, now)
	return err
}

// fulfil is Fulfil's body with the OUTCOME still in hand. The exported
// verb throws it away on purpose — run.Finish must not abort its own
// road because a guest would not go back, and a refused release is
// recorded as an obligation rather than raised as an error — but
// Discharge has to know: an obligation this pass did not close is an
// obligation that still stands, and dropping it from the report would
// have the pass claim it had discharged an environment it had not.
func fulfil(ctx context.Context, st *statestore.Store, prov verify.Verifier, l record.Lease, now func() time.Time) (Outcome, error) {
	// AN EMPTY JOB CANNOT PRODUCE EVIDENCE OF ABSENCE, and this refusal
	// is the whole of a defect that turned a transport failure into a
	// false handback. A Requested lease whose lookup found a real job,
	// released, and failed used to leave the record with its provider and
	// id still EMPTY; the next pass saw an ordinary Owed obligation, put
	// the empty job to the provider, and tart answered ErrUnknownJob —
	// "that is not a tart job" — which classify reads as CONFIRMED
	// ABSENCE. The lease was marked returned while the worker was still
	// running, and Outstanding then joined it away from the untracked
	// audit that would have found the guest.
	//
	// So a lease with no identity is Failed and stays owed. The provider
	// was not asked, because there is nothing to ask it about; the
	// recovery is a request lookup, which is the Requested road.
	if l.ID.ID == "" {
		const detail = "this lease names no job yet, so a release cannot be attempted or confirmed"
		return Failed, Confirm(ctx, st, claimOf(l), Failed, detail, now())
	}
	out, detail := classify(prov.Release(ctx, jobOf(l)))
	return out, Confirm(ctx, st, claimOf(l), out, detail, now())
}

// Closes reports an outcome that ends an obligation: the provider let
// the environment go, or confirmed it does not exist. A method so that a
// fourth outcome is a compile-time visit here rather than a missed case
// wherever "did this finish" is asked.
func (o Outcome) Closes() bool { return o == Released || o == Absent }

// classify turns a provider's release error into an outcome. Confirmed
// absence is the case that matters: without it an environment somebody
// else already deleted is owed forever.
//
// ErrUnknownJob is the whole vocabulary of confirmed absence, and it is
// the word every other verb on the Verifier already uses for a job the
// provider does not have. Everything else is Failed, deliberately
// including the machine facts — a host with no provider and one with no
// environment for this platform have not confirmed anything, and reading
// either as absence would retire a lease for a VM that is still running.
func classify(err error) (Outcome, string) {
	switch {
	case err == nil:
		return Released, ""
	case errors.Is(err, verify.ErrUnknownJob):
		return Absent, err.Error()
	default:
		return Failed, err.Error()
	}
}

// jobOf is the adapter record.LeaseID exists for: three fields copied,
// so the durable shape is not hostage to the provider interface.
func jobOf(l record.Lease) verify.Job {
	return verify.Job{Provider: l.ID.Provider, ID: l.ID.ID, Started: l.ID.Started, Request: l.Request}
}

// KeepFor is how long a kept environment stays before it becomes a Due
// obligation: the Retain deadline run.Finish writes through RetainIn on
// every Keep disposition — a failure's debug guest, a --keep-env pass.
// A constant in phase one (--keep-for later), and a constant rather than
// "until a person types cancel" because an adversarial pass priced the
// alternative: on a two-guest machine two kept failures stall every
// drain until somebody notices, under a dispatcher whose purpose is to
// keep things moving, and a merged change's kept failure guest — closed,
// demolished, compacted — was Held forever under a ChangeID nothing in
// the tree could name. A deadline is a verdict on nothing; it is when
// the slot comes back.
const KeepFor = 24 * time.Hour

// RetainIn writes the Retain deadline on a Held lease, as a transaction
// step, and it is exported by THIS package because Retain is a lease
// field and run.Finish is the caller — the same rule-4 shape as
// RequestIn. Finish calls it in the settle Amend for every Keep
// disposition, so the deadline lands in the same write as the verdict
// that kept the guest; Outstanding reads it back as a Due obligation
// once it has passed.
//
// Held is the precondition and it is checked: a lease with a release
// already claimed is on its way back, and a Retain on it would be a
// deadline for keeping something nobody is keeping.
func RetainIn(tx *statestore.Txn, request string, until time.Time) {
	l, ok := tx.State().Leases[request]
	if !ok || l.Returned() || !l.Held() {
		return
	}
	l.Retain = &until
	tx.PutLease(l)
}

// Acquire is the OTHER end, and the one an earlier draft of this design
// left out. It writes the lease with Phase Requested BEFORE the
// provider is called, so a crash in the window leaves a record with a
// request token and no handle — which recovery can resolve by asking
// the provider — rather than an anonymous VM and nothing to look for.
//
// It is also what makes every guest dockhand boots visible. A draft
// exempted the synchronous gate from it, and the gate is gone; what
// remains exempt is NOTHING: `exec` and `shell` inside a repository
// write a lease through this function too, with a Claim.Expires the
// shell renews, so a person's interactive guest on a dispatched
// checkout is Standing LiveElsewhere to the pass and never seized. That
// is the answer to the adversarial finding that default-on seizure of
// untracked guests at tick rate destroys a live `shell` session twenty
// minutes in — the guest is tracked, so it is not untracked. D22 closes
// on this road too.
//
// A CAPACITY REFUSAL CLOSES THE LEASE IT JUST WROTE, in the same pass,
// without asking the provider anything. verify.ErrNoVacancy carries the
// assertion that nothing was created, so the Requested lease is retired
// with Outcome Absent — "confirmed not to exist: also done" — rather
// than left standing for a LookupRequest round trip. That matters at
// exactly the wrong moment: on a saturated machine every queued attempt
// is refused, and a phantom obligation per refusal is state-tree churn
// when N is already worst. It is the one place this design closes an
// obligation on a provider's WORD instead of a provider's answer, and
// that word is a contract on an interface anyone may implement — so it
// wants a conformance test rather than trust.
//
// THE REQUEST TOKEN IS MINTED HERE and stamped into req.ID, whatever the
// caller put there. It is the store's key for the lease document and the
// name the provider carries into the worker, so the two cannot be
// allowed to differ: a caller-chosen id that collided with a live lease
// would address somebody else's environment, and one that differed from
// what was submitted would leave a guest nothing could join. There is
// exactly one minter because there is exactly one writer of the record
// the token names.
//
// A SECOND ENVIRONMENT FOR ONE SLOT IS REFUSED, inside the Amend and so
// under the store's flock and compare-and-set, before the provider is
// called. See Slot for why the refusal lives in a predicate rather than
// in a filename.
func Acquire(ctx context.Context, st *statestore.Store, prov verify.Verifier, change record.ChangeID, req verify.Request, by Claimant, now func() time.Time) (record.Lease, error) {
	// The slot's platform is the release the request names, and a request
	// that names none takes the provider's default — so its lease reads
	// as the default slot for this change, which is one environment and
	// not none. It is not refused here: a single-platform provider has no
	// other answer to give, and refusing would make the honest zero
	// verify.Request documents unusable.
	platform := req.Platform.Name
	req.ID = mint()
	// THE OWNER IS STAMPED HERE, beside the id, and for the same reason
	// the id is: this is the one place a request is completed before a
	// provider ever sees it, and both fields are facts about WHOSE work
	// this is that no layer below can supply.
	//
	// verify.Request.Owner had no producer at all until now — the field
	// was documented, tart carried it into its attribution sidecar, and
	// every submission left it empty, so writeAttribution returned
	// immediately and no guest on any machine was attributable to
	// anything. That made verify.Worker.Owner permanently "" and left
	// every ownership question about a guest unanswerable, which is a
	// contract with no producer rather than a policy anybody chose.
	//
	// The root and not the whole OwnerID: a guest outlives the process
	// and the pass that made it, so the durable question a reader asks
	// of it is WHICH CHECKOUT, which is exactly Root and exactly what
	// lease.standingOf compares for ForeignRoot. It is already canonical
	// — record.OwnerID.Root is spelled once, by the composition root —
	// so two dockhands on one machine compare the same string.
	req.Owner = by.Owner.Root
	at := now()
	var l record.Lease
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		if held, ok := liveOn(tx.State(), change, platform); ok {
			return fmt.Errorf("%w: %s is held under request %s", ErrSlotTaken, Slot(change, platform), held.Request)
		}
		l = record.Lease{
			Request:  req.ID,
			Change:   change,
			Owner:    by.Owner,
			Platform: platform,
			Phase:    record.Requested,
			Test:     req.Test,
			Claim:    &record.Claim{By: spell(by.Owner), At: at, Expires: by.Expires, Pass: by.Pass},
		}
		tx.PutLease(l)
		return nil
	})
	if err != nil {
		return record.Lease{}, err
	}

	job, serr := prov.Submit(ctx, req)
	if serr != nil {
		if errors.Is(serr, verify.ErrNoVacancy) {
			// The refusal asserts that nothing was created, so the record
			// this call wrote a moment ago names nothing and is retired
			// here rather than left for a round trip. Its own failure is
			// swallowed on purpose: the caller is being handed the refusal
			// that matters, and a state-ref write that lost a race leaves
			// an obligation the next pass discharges anyway.
			_ = retire(ctx, st, change, platform, by, serr.Error(), now())
			return record.Lease{}, serr
		}
		// Everything else leaves the lease standing in Requested with no
		// handle, which is precisely the obligation kind Discharge
		// resolves through LookupRequest. A submit that failed after
		// creating a guest and a submit that failed before creating one
		// look identical from here, and the provider is the only thing
		// that can tell them apart.
		return l, serr
	}

	// The handle is not known yet: a job's id and the environment's own
	// name are the same string for one backend and need not be for
	// another, so Handle stays empty until a Status reports one. What is
	// known is the identity, and that is what ActiveIn writes.
	l.ID = record.LeaseID{Provider: job.Provider, ID: job.ID, Started: job.Started}
	l.Image, l.OS, l.Xcode = job.Base, job.OS, job.Xcode
	l.Phase = record.Active
	return l, nil
}

// retire closes a lease this process wrote and the provider then said
// nothing was created for: the claim and the confirmation in ONE
// transaction.
//
// One transaction because there is nothing to put between them. The
// three-step shape everywhere else exists to keep a provider call out of
// a closure; here the provider has already spoken, and splitting the
// write in two would open a window in which the lease is claimed by a
// process that is about to return an error to its caller and go away.
//
// It is the one place this design closes an obligation on a provider's
// WORD rather than on a provider's answer to a question about a specific
// resource — see Acquire on why the capacity contract earns that, and on
// why the contract wants a conformance test rather than trust.
func retire(ctx context.Context, st *statestore.Store, change record.ChangeID, platform string, by Claimant, detail string, now time.Time) error {
	return st.Amend(ctx, func(tx *statestore.Txn) error {
		l, ok := liveOn(tx.State(), change, platform)
		if !ok {
			return nil
		}
		claimed, took := claimIn(tx, l, by, now)
		if !took {
			return nil
		}
		// The claim this transaction just took, by its own token: the two
		// writes are one transaction, so the fence can never be stale here
		// — but presenting it keeps ConfirmIn's contract single, with no
		// caller exempt from the rule that a completion names its claim.
		ConfirmIn(tx, claimOf(claimed), Absent, detail, now)
		return nil
	})
}

// ActiveIn records that the provider answered: the lease advances to
// Active carrying the job's identity, as a transaction step.
//
// It is a *Txn mutator and not a second Amend inside Acquire, and the
// difference is a crash window. run.Start advances its attempt to Active
// in ONE Amend; if the lease's own advance were a separate write, a
// crash between them would leave a Queued attempt beside a live lease
// for its slot — and the next Start would meet ErrSlotTaken forever,
// because the attempt it would have to finish is the one that never
// started. Composed into Start's transaction, the two facts land
// together or not at all, which is rule 4's shape (two owned mutators,
// one transaction) applied to the write it was invented for.
//
// It is a compare-and-set on the record's own phase: it writes only if
// the lease under this request token is still the Requested one this
// process wrote. False means somebody else moved it — a discharge that
// decided the submit had been stranded and retired the lease — and the
// caller must not overwrite that decision with an answer it obtained
// before it was made.
func ActiveIn(tx *statestore.Txn, l record.Lease) bool {
	cur, ok := tx.State().Leases[l.Request]
	if !ok || cur.Phase != record.Requested || cur.Returned() {
		return false
	}
	cur.ID = l.ID
	cur.Image = l.Image
	cur.Phase = record.Active
	tx.PutLease(cur)
	return true
}

// HandleIn records the provider's own name for the environment, once a
// Status has reported one, as a transaction step.
//
// It is separate from ActiveIn because the two facts arrive at different
// moments from different calls: Submit answers with a job, and only a
// Poll answers with a handle. record.Lease.Handle says as much in three
// words ("the provider's own name, once known"), and the alternative —
// having Acquire assume the job id is the environment's name — is one
// backend's fact written into the kernel, which is the defect the debug
// verbs reaching into tart were about.
func HandleIn(tx *statestore.Txn, request, handle string) {
	l, ok := tx.State().Leases[request]
	if !ok || l.Returned() || handle == "" || l.Handle == handle {
		return
	}
	l.Handle = handle
	tx.PutLease(l)
}

// liveOn is how every operation here addresses a slot: the one lease for
// this (change, platform) that the provider has not confirmed back.
//
// "Live" is statestore.State.Live's own reading and for its reason — a
// lease whose release is owed still names an environment that exists,
// and one written Requested before the provider was ever called names
// one that MAY. Only a confirmed handback takes a lease out of this
// list, which is what makes a Returned lease invisible here and a
// re-acquire of the same slot legal.
//
// It iterates because the store's key is the request token, not the
// slot. That is a scan of a map the store is sized for (statestore.State
// on N records), and it is deterministic: the keys are walked in sorted
// order, so two readers of one state agree about which lease they are
// looking at even in the state Acquire's refusal exists to prevent.
func liveOn(s statestore.State, change record.ChangeID, platform string) (record.Lease, bool) {
	for _, key := range slices.Sorted(maps.Keys(s.Leases)) {
		l := s.Leases[key]
		if l.Change == change && l.Platform == platform && !l.Returned() {
			return l, true
		}
	}
	return record.Lease{}, false
}

// building reports a subject still using this slot's environment: an
// attempt on the same change and platform that has a lease and no
// verdict.
//
// This is the shipped ReleaseJob's `idle` check, moved to where the
// question can be asked of a record instead of of a note's key
// suffixes. It stops both roads into claimIn, including a seizure, and
// it is why a concurrent pass cannot delete a running build's VM: the
// obligation is refused while the build is live, and the build's own
// settlement is what makes the slot claimable.
func building(s statestore.State, change record.ChangeID, platform string) bool {
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		a := s.Attempts[key]
		if a.Change == change && a.Platform == platform && a.Active() {
			return true
		}
	}
	return false
}

// sameOwner is OwnerID equality with the instant compared as an instant,
// which is statestore.State.Owed's rule and for its reason: a value
// decoded from JSON and one built in this process differ in their
// monotonic reading while naming the same instant, and == would report
// this checkout's own obligations as a stranger's.
func sameOwner(a, b record.OwnerID) bool {
	return a.Root == b.Root && a.Host == b.Host && a.PID == b.PID && a.Since.Equal(b.Since)
}

// spell is what record.Claim.By and record.Release.By hold: a person's
// reading of who took something, host and process.
//
// IT IS A REPORT FIELD AND NOTHING DECIDES FROM IT (rule 6). Every
// question about ownership is asked of record.Lease.Owner, which is a
// typed OwnerID with a canonical root and a liveness pair; this string
// exists so a `status` line and a stale record months later can say who
// without a reader having to assemble one.
func spell(o record.OwnerID) string {
	return o.Host + "/" + strconv.Itoa(o.PID)
}

// mintClaim makes a claim token: the fence a completion presents, fresh
// on every claim. Random and not derived from the claimant or the
// instant, because two re-claims by one owner in one second must not
// produce one token — that is exactly the ABA the fence exists to
// catch. Eight bytes: it is compared, never joined on, and it only has
// to be unrepeatable rather than globally unique.
func mintClaim() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// mint makes a request token: the name the lease document is stored
// under, the id the provider carries into the environment, and the
// string recovery joins the two on.
//
// Random rather than derived from the slot, because a slot is reused —
// a change verified, failed, re-verified on the same platform — and a
// token that repeated would address a previous environment's record.
// Hex and sixteen bytes: hex because the token becomes a resource name
// at a provider whose alphabet is not ours to assume, and sixteen bytes
// because a collision here would silently join two leases.
func mint() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
