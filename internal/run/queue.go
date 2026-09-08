package run

import (
	"sort"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Order decides which queued attempts are tried next, and in what
// order. It is a pure function — attempts in, an ordered list out — and
// it takes NO vacancy. A draft parameterised it on verify.Vacancy, and
// an adversarial pass showed what that cost: Survey, which is the bump
// road under a selector, is forbidden by R8 from asking whether the
// verifier is full, so it could only pass an unknown Vacancy — and an
// unknown Vacancy admits nothing, so the sweep would have started
// nothing and flow 3's "2 submitted now" could never happen. The count
// was never load-bearing here: the caller iterates this list calling
// Start until the FIRST verify.ErrNoVacancy and stops, on every road,
// and the sentinel is the authority. verify.Vacancy survives only as a
// number `status` reports.
//
// The ordering is by record.Attempt.NotBefore then by age, so a
// backed-off attempt waits its turn and the longest-waiting work goes
// first. That is the whole policy, and it fixes two of the three defects
// the drain has today — an alphabetical order that starves the tail of
// the namespace, and a port that fails for its own reasons retried at
// full cost every pass. The third ("how many may run" answered by a
// failed call) is not a defect under R8; it is the design. Writing the
// backoff is Defer's job.
//
// An attempt whose NotBefore has PASSED is not behind one whose
// NotBefore was never set: a backoff that has expired is over, and a
// queue that kept ranking a recovered port behind every fresh one would
// starve it exactly as the alphabetical order did. So the sort key is
// the later of NotBefore and now — every ready attempt shares one key —
// and the tie is broken by age. now is a parameter for that reason, and
// it is a parameter rather than a clock read because this is a decision
// and decisions here take their facts as values.
func Order(queued []record.Attempt, now time.Time) []record.Attempt {
	out := append([]record.Attempt(nil), queued...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := readyAt(out[i], now), readyAt(out[j], now)
		if !ri.Equal(rj) {
			return ri.Before(rj)
		}
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.Before(out[j].Started)
		}
		// Two attempts queued in one batch share an instant — a sweep
		// writes N in one transaction — so the id breaks the tie and two
		// passes over one store produce one order.
		return out[i].ID < out[j].ID
	})
	return out
}

// readyAt is when an attempt became eligible: now for one with no
// backoff or an expired one, and the deadline itself for one still
// waiting.
func readyAt(a record.Attempt, now time.Time) time.Time {
	if a.NotBefore == nil || a.NotBefore.Before(now) {
		return now
	}
	return *a.NotBefore
}

// NotStarted is one queued attempt this pass may not start, and why.
// The eligibility gates are POLICY that Order cannot see — a person's
// hold, a change a newer sibling superseded, one that is closed — so
// they are refused before the ordering and reported as values rather
// than dropped. "One whose ref a crash never created" is NOT a gate any
// more: under R23 the ref is created in the batch that writes the record
// and the attempt, so a queued attempt on a record with no ref is
// unrepresentable, and the Unbound reason that named it is gone. There
// is no Unbound. A change bound for nowhere is not one of them either:
// Destination is how far publication reaches, and an attempt's existence
// is the ask.
//
// It is a separate type from NotAdmitted because the two refusals happen
// at different stages to different populations for different reasons,
// and a reader who conflates them will look for a queue cap when the
// answer is a hold. A hold is also the one entry here that a person must
// act on, which the exit band has to be able to see.
type NotStarted struct {
	Attempt string
	Why     Ineligible
	Detail  string
}

type Ineligible uint8

const (
	IneligibleUnknown Ineligible = iota
	// Held is a PERSON's hold — change.Held(c, ActVerify, _) — and only
	// that: a crossing's born-hold withholds publication and deletion,
	// never a build, so a stable-to-prerelease bump drains like any other.
	Held
	// Superseded reads record.Change.SupersededBy != "" and NOT the
	// State, because a change with an open publication is superseded
	// while still open (change.SupersedeIn); its queued attempts must not
	// be drained either way.
	Superseded
	// Closed is a queued attempt on a change that is Closed(): a defence
	// for records written before run.WithdrawIn existed, since every
	// close now withdraws the change's queued attempts in the same Amend.
	// Without it an adversarial pass showed the drain building demolished
	// changes — the commit object outlives the branch for
	// statestore.PruneExpire's window — and the withheld ones counting
	// against MaxQueued for the life of the repository.
	Closed
)

// Pending derives Order's input from one read: the attempts eligible to
// start, and the ones refused with the reason. Pure, like Count, and
// for the same import reason.
//
// The walk is over sorted ids rather than the map, so two passes over
// one store report the same refusals in the same order and a golden can
// pin them; the ORDERING of what may start is Order's and not this
// function's.
//
// It takes a clock it does not currently read, and that is deliberate
// rather than left over. Every gate here is a fact about the RECORD — a
// hold, a supersession, a close — and none of them expires; the one
// time-shaped fact in the queue is the backoff, which is Order's key and
// not an eligibility question. The parameter stays because every road
// that drains passes its own clock into this pair of calls already, and
// a gate that does turn on time (a hold with an expiry, a claim that
// went stale) lands here without moving every caller — and because a
// pure decision in this design takes its facts as values, clock
// included, rather than reading one.
func Pending(s statestore.State, _ time.Time) (queued []record.Attempt, ineligible []NotStarted) {
	for _, id := range sortedAttempts(s) {
		a := s.Attempts[id]
		if !a.Queued() {
			continue
		}
		c, ok := s.Changes[string(a.Change)]
		switch {
		case !ok || c.State.Closed():
			ineligible = append(ineligible, NotStarted{Attempt: a.ID, Why: Closed,
				Detail: "the change this attempt belongs to is closed"})
		case c.SupersededBy != "":
			ineligible = append(ineligible, NotStarted{Attempt: a.ID, Why: Superseded,
				Detail: "a newer change superseded this one: " + c.SupersededBy})
		case change.Held(c, change.ActVerify, record.Human) != nil:
			ineligible = append(ineligible, NotStarted{Attempt: a.ID, Why: Held,
				Detail: holdReason(c)})
		default:
			queued = append(queued, a)
		}
	}
	return queued, ineligible
}

// holdReason is the person's own words for the hold, when they gave
// any. It is the one Detail here a person must act on, so it quotes them
// rather than restating the state.
func holdReason(c record.Change) string {
	if c.Hold != nil && c.Hold.Reason != "" {
		return "held: " + c.Hold.Reason
	}
	return "held"
}

// sortedAttempts is the store's attempt ids in a stable order, so every
// pure walk over the attempts answers the same way twice.
func sortedAttempts(s statestore.State) []string {
	out := make([]string, 0, len(s.Attempts))
	for id := range s.Attempts {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Admission is how much queue the operator will carry. It is a policy
// value and not a computed one — unlike verify.Vacancy, which is the
// backend's number, this is a choice about how much outstanding work is
// useful.
//
// Set is rule 7, and this type needed it for an embarrassing reason:
// the draft that introduced it, in the same pass that added the rule's
// roll-call table, had {MaxQueued, MaxPerPass} alone — so a zero value
// meant BOTH "the operator chose to admit nothing" and "nobody
// configured this", and an unconfigured build would have silently
// admitted no work at all while looking deliberate.
type Admission struct {
	Set       bool
	MaxQueued int // total OPEN attempts the tree may carry; see Count
	// MaxPerPass keeps its name and its documented unit — how many one
	// SWEEP may add. Q6 (rename to MaxPerWindow against --every) is
	// answered NO: Survey is the ONLY caller of Admit, and a sweep is a
	// person's invocation, so "per pass" means per sweep and is not a
	// rate at all. The unit error the rename answered was R8's — a pass
	// that admitted its own producer's work — and R8 removed the producer
	// from the pass. Cycle does not admit; it has nothing to admit.
	MaxPerPass int
}

// Withholding is why a pass did not enqueue a target. Only POLICY
// reasons appear here: Admit sees Admission and never sees capacity, so
// a busy machine is not one of these — that refusal happens later, at
// submit, and arrives as verify.ErrNoVacancy.
type Withholding uint8

const (
	// WithheldUnknown is the zero value and names no reason, so a caller
	// that forgets to set one cannot tell an operator to raise a cap that
	// was never the cause.
	WithheldUnknown Withholding = iota
	OverQueueCap                // Admission.MaxQueued: the store is full
	OverPassCap                 // Admission.MaxPerPass: come back next pass
	Unconfigured                // Admission.Set is false; nobody chose
)

// NotAdmitted is one target a pass did not enqueue, and why. Its own
// type and NOT run.Withheld, which keeps its one durable meaning — a
// member kept out of a guest's roster, record.Withheld. A draft had the
// two share a type and a doc comment saying so.
type NotAdmitted struct {
	Target string
	// Why is TYPED because the two caps are two different operator
	// actions: MaxQueued means the store is full and MaxPerPass means
	// come back next pass, and telling them apart from a sentence is rule
	// 6. Detail is the sentence a person reads and nobody decides from —
	// the same split app.Deferral already makes, and the same one the
	// shipped tree proves out, where VerifyDeferredError carries Reason
	// for stderr and Cause for every consumer that must decide.
	Why    Withholding
	Detail string
}

// Open is what the store already carries. It counts BOTH kinds a sweep
// grows, because a draft of Admit took []Spec and a Spec has no identity
// at all — so it could gate the queue and not the thing the design names
// as the fastest-accumulating kind. Ordering is the caller's and it must
// not be the sweep's own: sweep.sortTargets is deterministic by portdir,
// so admitting the first N of a stable order starves the tail of the
// namespace forever, which is verbatim the first defect Order is for.
type Open struct {
	changes  int
	attempts int
	at       string
}

// Changes and Attempts read the counts. THE FIELDS ARE UNEXPORTED AND
// Count IS THE ONLY CONSTRUCTOR, which is this design's answer to a
// rule-7 hazard that a Known flag would not have fixed.
//
// Every other fact in the roll call fails CLOSED when nobody filled it
// in: an unknown verify.Vacancy admits nothing, an unset Admission
// admits nothing. Open is the one that fails OPEN — Open{} says the
// store is empty, so Admit would admit the maximum, and the forged zero
// disables the only bound the design has on N. A Known bool would let a
// caller set Known and leave the counts at zero, which is the same
// forgery with a ceremony in front of it.
//
// So the zero is made UNCONSTRUCTIBLE outside this package instead. That
// is publish.Permit's mechanism — "not by convention, by the compiler" —
// applied to the value whose zero is most expensive.
func (o Open) Changes() int  { return o.changes }
func (o Open) Attempts() int { return o.attempts }

// At is the state commit the counts were taken from, so a report can say
// as-of. It is a stamp and never a gate: nothing decides from it.
func (o Open) At() string { return o.at }

// Count is Open's only constructor: a pure derivation from one
// consistent read, beside statestore.State's own Owed and Live.
//
// It lives HERE and not in statestore for a reason the import graph
// makes final rather than tasteful: statestore may not import run, so
// the package that holds the documents cannot name the type that
// bounds them. It takes a State VALUE rather than a *Store so it is
// callable inside an Amend closure over Txn.State(), which is what lets
// a pass count and enqueue in ONE transaction.
//
// Attempts counts every attempt Compact may not drop — !Settled(), not
// merely un-started — because that is the population that sets N, which
// is what an Amend's critical section is charged for. Changes counts the
// ones still Bound(), for the same reason and by the same rule: a closed
// change is on its way out of the tree.
func Count(s statestore.State) Open {
	o := Open{at: s.At}
	for _, c := range s.Changes {
		if c.Bound() {
			o.changes++
		}
	}
	for _, a := range s.Attempts {
		if !a.Settled() {
			o.attempts++
		}
	}
	return o
}

// Admit is the sweep's admission decision for ONE target, pure, and it
// is called INSIDE that target's Amend closure over run.Count(tx.State())
// so that the queue cap is read under the flock that guards the write —
// two hundred sweeps on two hundred checkouts of one repository cannot
// each admit against a count that was true before the others wrote. A
// draft admitted the whole selector at once, after every target had
// planned and prepared, and an adversarial pass priced it: a 400-port
// bump sweep fetches one target at a time, up to twenty minutes each, so
// nothing was committed for hours and a Ctrl-C lost all of it — where
// the shipped sweep emits a row and mints per target as it arrives and
// resumes by rerun. added is this sweep's own count so far, kept in the
// operation and not in the store, because MaxPerPass is the SWEEP's
// unit; MaxQueued is read from the count every time. ok false comes with
// a typed Withholding; ok true comes with WithheldUnknown, which nothing
// reads.
//
// ITS CALLER IS THE SWEEP ROAD AND NOT THE PASS. Under always-enqueue a
// pass has no minted-and-unattempted population to admit, and how much
// work may be QUEUED is decided where work is created: Survey is the one
// caller, and Cycle has nothing to admit.
func Admit(open Open, added int, cap Admission) (ok bool, why Withholding) {
	if !cap.Set {
		return false, Unconfigured
	}
	if cap.MaxPerPass > 0 && added >= cap.MaxPerPass {
		return false, OverPassCap
	}
	if cap.MaxQueued > 0 && open.Attempts() >= cap.MaxQueued {
		return false, OverQueueCap
	}
	return true, WithheldUnknown
}
