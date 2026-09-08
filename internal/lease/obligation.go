package lease

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Obligation is one piece of unfinished business a pass can act on, and
// its Standing says whether THIS pass may. Four kinds, found three ways:
// Owed and Due come from the state ref, Requested from the state ref
// joined to the provider, Untracked from the provider's own inventory.
type Obligation struct {
	Kind     ObligationKind
	Standing Standing
	Change   record.ChangeID
	Platform string
	Worker   string
	ID       record.LeaseID
	// Root is the checkout the obligation belongs to when it is not this
	// one — the foreign OwnerID.Root, or an untracked worker's
	// attribution string — so a report can NAME it rather than say
	// "somebody else's". Empty when the obligation is this checkout's.
	Root     string
	Since    time.Time
	Attempts int
	Why      string
	// Request is the token the lease was written under, and the string
	// the provider's inventory joins on. It is here because Discharge
	// needs it to ask LookupRequest about a Requested obligation, and
	// because it is the only name an Untracked worker and a lease have
	// in common. Empty for an untracked worker the backend could not
	// name a request for.
	Request string
	// Job is what an Untracked worker is released through, filled in by
	// the provider that knows how its jobs and its environments
	// correspond (verify.Worker.Job). It is the zero value for a
	// lease-backed obligation, whose job comes from the record instead,
	// and for a worker the backend named no job for — which is a worker
	// this pass says so about rather than guesses at.
	Job verify.Job
}

// ObligationKind is the POPULATION of the owed stage, stated once
// because an adversarial pass showed the spine's rule for it — "one
// owned by this PID under an earlier pass token is dead by construction"
// — was FALSE for a dispatcher's own detached builds: every Active lease
// its Start wrote in pass N is, at pass N+1, owned by this PID under an
// earlier token, and a build runs for hours. Built as written, a resident
// dispatcher destroyed its own running VMs on the tick after the grace
// cutoff. So the population is exactly these four, and a lease that is
// Held with a Handle the provider still lists and a Retain that has not
// passed is NEVER an obligation, whatever its owner's liveness or pass
// token: the record, not the process, owns a detached build.
type ObligationKind uint8

const (
	// UnknownObligation is the zero value: an obligation whose kind nobody
	// set is not a kind, and Discharge refuses it rather than guessing.
	UnknownObligation ObligationKind = iota
	// Owed is a lease with Release.Requested and no Done: a release this
	// checkout claimed and did not finish — a crash between Fulfil's
	// provider call and its Confirm.
	Owed
	// Requested is a lease in Phase Requested with no Handle: Acquire
	// crashed between writing the record and recording the provider's
	// answer. Resolved through verify.RequestLookup on the id the caller
	// assigned; Absent finishes it, Unknown leaves it standing.
	Requested
	// Untracked is an inventory worker no lease's Request joins: a crash
	// between the provider creating the guest and Acquire recording the
	// handle, or a guest something other than dockhand made. Its Standing
	// is decided by ATTRIBUTION (verify.Worker.Owner) and not by any
	// lease, since there is none.
	Untracked
	// Due is a HELD lease whose Retain has passed, or whose change is
	// Closed(): a kept debug guest nobody came back for, or a guest a
	// merged change left behind. It is the kind a draft had no source
	// for — record.Lease.Retain was "a deadline, not a verdict" that
	// nothing read — and without it a kept failure on a two-guest
	// machine stalled every drain until a person typed cancel.
	Due
)

// Standing is whether THIS pass may act on an obligation, and it is the
// --reclaim-orphans ownership rule as a typed value. It is decided ONCE,
// in Outstanding, from three facts — OwnerID.Root against me.Root, the
// (PID, start-time) liveness pair against this host's process table,
// and the pass token — and Discharge's seize list is this table and
// nothing else, stated here and not twice.
//
//	Mine          this process's own residue, or an untracked worker
//	              attributed to this checkout's Root: SEIZED once older
//	              than the grace cutoff. exec/shell guests are NOT here,
//	              because they hold a lease (Acquire) and so are never
//	              untracked; a guest that is here with this Root and no
//	              lease is a crash's leavings.
//	EarlierPass   this PID under an earlier pass token: SEIZED after the
//	              grace cutoff. Kept as its own value for the report,
//	              though because discharge runs FIRST in a pass nothing of
//	              the current pass can exist when it runs, so Mine already
//	              means "from an earlier pass" — the token is a report
//	              field, not a seize condition.
//	LiveElsewhere same Root, another PID that is ALIVE (PID present with
//	              the matching start time): REPORTED, never seized,
//	              whatever its age. A person's bump mid-Submit in another
//	              shell, a dispatcher's predecessor still shutting down.
//	DeadElsewhere same Root, another PID that is dead (absent, or present
//	              with a different start time — a reused number): SEIZED
//	              after the grace cutoff. A crashed bump, a dispatcher
//	              that restarted and now has a new PID. A draft's enum had
//	              NO value for this, the commonest same-checkout case, so
//	              its Standing fell to the zero value and Discharge refused
//	              it: a restarted dispatcher could never discharge its
//	              predecessor's leases.
//	ForeignRoot   another checkout's, by OwnerID.Root or by the worker's
//	              attribution: REPORTED with the root named, never seized.
//	              A repository copied to another machine must not stop a
//	              VM it does not own.
//	Unattributed  an untracked worker with an EMPTY attribution — a guest
//	              made with no --tree, or not by dockhand: REPORTED by the
//	              machine, SEIZED only under Seizure.Unattributed, which is
//	              a person's `cycle --once` flag and never a loop's.
//
// The grace cutoff protects ONLY the window the liveness check cannot
// see — a live process's Requested lease whose Submit is still in flight
// (the record exists, the handle does not, the PID is alive but the
// obligation is real for the seconds before the provider answers) — and
// not everything; that is why it is a duration and not a policy.
//
// TWO CASES THE TABLE DOES NOT NAME, decided here in its spirit and both
// toward reporting rather than seizing. A lease owned on ANOTHER HOST is
// LiveElsewhere however old it is: this machine has no process table to
// ask, and "I could not find out" must not arrive as "it is dead" when
// the act on the other side destroys a VM (rule 7). A same-host pair
// this machine could not resolve at all — no ps, a refused exec — is
// LiveElsewhere for the same reason.
type Standing uint8

const (
	// StandingUnknown is the zero value: nobody decided, and Discharge
	// refuses it rather than seizing on a default.
	StandingUnknown Standing = iota
	Mine
	EarlierPass
	LiveElsewhere
	DeadElsewhere
	ForeignRoot
	Unattributed
)

// Seizable reports the standings Discharge may act on at all; the grace
// cutoff and Seizure.Unattributed then narrow it. A method so a new
// standing is a compile-time visit here rather than a missed case in
// the seize loop.
func (s Standing) Seizable() bool {
	return s == Mine || s == EarlierPass || s == DeadElsewhere || s == Unattributed
}

// Outstanding is every obligation this checkout can see, each with its
// Standing decided: the four kinds, found from the state ref and the
// provider's inventory, joined on verify.Worker.Request. pass is the
// current pass token (record.Claim.Pass on this pass's own claims), used
// only to label EarlierPass. It seizes nothing and filters nothing by
// age — Status reports the whole list, foreign roots named — and the
// grace cutoff is Discharge's, so that "what is owed" and "what this
// pass will take" are two answers and not one.
//
// It propagates statestore.ErrNoState rather than reporting an empty
// list. A missing state ref means this pass cannot account for anything
// on the machine, and the caller that acts on obligations DESTROYS
// provider resources — so "I could not find out" must not arrive as
// "there is nothing here".
//
// THE INVENTORY IS READ FIRST AND THE STATE REF SECOND, and the order is
// load-bearing rather than incidental. Acquire writes the lease BEFORE
// the provider is called, so a worker in the inventory snapshot was
// created after its lease was written; reading the state afterwards
// therefore reads a state that already contains every lease behind every
// worker in the snapshot. That is what makes "no lease joins this
// worker" a fact rather than a race — under the opposite order, a lease
// written between the two reads would leave its brand-new guest looking
// untracked, and untracked plus this checkout's own attribution is
// seizable. It is also why an untracked worker needs no age: the
// ordering closes the window a cutoff would otherwise have to guess at,
// and a provider's listing carries no creation time to measure one from.
//
// A provider that cannot be asked for an inventory — no provider at all,
// a backend that is not a WorkerLister, a listing that failed — yields
// no Untracked obligations and no error. That is the shipped audit's own
// rule and its reason: the pass has learned NOTHING about this machine's
// workers, which is a different thing from learning there are none, and
// the honest rendering of both is silence. The lease-backed kinds are
// still reported, because those are answered by the state ref alone.
func Outstanding(ctx context.Context, st *statestore.Store, prov verify.Verifier, me record.OwnerID, pass string, now time.Time) ([]Obligation, error) {
	workers := inventory(ctx, prov)
	s, err := st.Read(ctx)
	if err != nil {
		return nil, err
	}
	// One answer per process for the whole pass. Standing is decided ONCE,
	// and asking the host twice about one PID could give two answers — a
	// process that exited between two leases it owned would be alive in
	// the first obligation and dead in the second, which is one pass
	// holding two opinions about one fact.
	asked := map[int]liveness{}

	var obs []Obligation
	joined := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(s.Leases)) {
		l := s.Leases[key]
		// EVERY lease joins, returned ones included. A lease the provider
		// confirmed back should match no live worker at all; if one is
		// still listed, the safe reading is that our inventory snapshot
		// predates the handback, and calling it untracked would seize a
		// guest on the strength of a timing accident.
		joined[l.Request] = true
		if l.Handle != "" {
			joined[l.Handle] = true
		}
		kind, since, why := classifyLease(l, s, now)
		if kind == UnknownObligation {
			continue
		}
		ob := Obligation{
			Kind: kind, Change: l.Change, Platform: l.Platform,
			Worker: l.Handle, ID: l.ID, Since: since, Why: why, Request: l.Request,
		}
		if l.Release != nil {
			ob.Attempts = l.Release.Attempts
		}
		ob.Standing, ob.Root = standingOf(ctx, l.Owner, l.Claim, me, pass, asked)
		obs = append(obs, ob)
	}

	for _, w := range workers {
		if joined[w.Request] || joined[w.Name] {
			continue
		}
		ob := Obligation{
			Kind: Untracked, Worker: w.Name, Request: w.Request, Job: w.Job,
			Why: "the provider is running it and no lease here names it",
		}
		switch w.Owner {
		case "":
			ob.Standing = Unattributed
		case me.Root:
			ob.Standing = Mine
		default:
			ob.Standing, ob.Root = ForeignRoot, w.Owner
		}
		obs = append(obs, ob)
	}
	return obs, nil
}

// classifyLease is the population rule, in the order the kinds exclude
// one another: a lease is at most one of these, and everything else is
// not an obligation at all.
//
// The last clause is the one an adversarial pass paid for. A Held lease
// with a Retain that has not passed, under a change that is still open,
// is NOT an obligation however old it is and whoever owns it — that is a
// detached build, and the record owns it rather than the process that
// started it. A rule that seized by the owner's liveness or by its pass
// token would eat every VM the dispatcher itself started on the previous
// pass, because a build runs for hours and a pass is five minutes.
func classifyLease(l record.Lease, s statestore.State, now time.Time) (ObligationKind, time.Time, string) {
	taken := time.Time{}
	if l.Claim != nil {
		taken = l.Claim.At
	}
	switch {
	case l.Returned():
		return UnknownObligation, time.Time{}, ""
	case l.Owed():
		why := l.Release.LastError
		if why == "" {
			why = "a release was claimed here and never confirmed"
		}
		return Owed, l.Release.Requested, why
	case l.Phase == record.Requested && l.Handle == "":
		return Requested, taken, "the provider was asked for this environment and never answered"
	case l.Held() && l.Retain != nil && l.Retain.Before(now):
		return Due, taken, "the environment was kept until " + l.Retain.Format(time.RFC3339) + " and nobody came back for it"
	case l.Held() && closed(s, l.Change):
		return Due, taken, "the change it was taken for is closed"
	}
	return UnknownObligation, time.Time{}, ""
}

// closed reports a change whose life is over, which is one of the two
// sources of a Due obligation: a merged change's kept guest would
// otherwise be Held forever under a ChangeID the tree no longer holds.
//
// A change with NO record is not closed here. That is the safe reading
// and it is deliberate: a compacted or hand-deleted change record is a
// missing fact, and treating a missing fact as permission to destroy an
// environment is the shape of D22. Such a lease stands, and a person is
// the one who resolves it.
func closed(s statestore.State, id record.ChangeID) bool {
	c, ok := s.Changes[string(id)]
	return ok && c.State.Closed()
}

// standingOf decides one lease-backed obligation's Standing, from the
// three facts Standing's table names and nothing else.
func standingOf(ctx context.Context, owner record.OwnerID, claim *record.Claim, me record.OwnerID, pass string, asked map[int]liveness) (Standing, string) {
	if owner.Root != me.Root {
		return ForeignRoot, owner.Root
	}
	if sameOwner(owner, me) {
		// This very process. The pass token only labels it: discharge runs
		// first in a pass, so nothing of the current pass can exist when it
		// does, and Mine already means "from an earlier pass".
		if pass != "" && claim != nil && claim.Pass != "" && claim.Pass != pass {
			return EarlierPass, ""
		}
		return Mine, ""
	}
	if owner.Host != me.Host {
		// Another machine's process, over a root that spells the same. No
		// process table here can answer for it, and an unanswerable
		// question is not a dead process.
		return LiveElsewhere, ""
	}
	live, seen := asked[owner.PID]
	if !seen {
		live = processIs(ctx, owner.PID, owner.Since)
		asked[owner.PID] = live
	}
	switch live {
	case gone:
		return DeadElsewhere, ""
	case running, unknownLiveness:
		return LiveElsewhere, ""
	}
	return LiveElsewhere, ""
}

// inventory is every environment the provider is running, or nothing at
// all when it cannot be asked. See Outstanding for why a refusal is
// silence here and not an error.
func inventory(ctx context.Context, prov verify.Verifier) []verify.Worker {
	if prov == nil {
		return nil
	}
	lister, ok := prov.(verify.WorkerLister)
	if !ok {
		return nil
	}
	workers, err := lister.Workers(ctx)
	if err != nil {
		return nil
	}
	return workers
}

// Seizure is what Discharge is permitted to take, and it is a policy
// value with a Set (rule 7): a zero Grace would seize seconds-old work
// and a zero Seizure{} would look chosen. Grace is the cutoff (F2: a
// reconciler at the start of a five-minute pass meets work that is
// seconds old); Unattributed is the person's `cycle --once` flag, refused
// on a loop by cli, that lets a pass take guests nobody attributed.
type Seizure struct {
	Set   bool
	Grace time.Duration
	// Unattributed permits seizing an inventory worker with EMPTY
	// attribution — not made by dockhand, or made with no --tree — and
	// nothing else. It is NOT the switch for a person's own exec or shell
	// guest: those record a lease (ruled 2026-09-07), so a live one is
	// LiveElsewhere and never seized, and a crashed one is DeadElsewhere
	// and seized past Grace with no flag able to save or doom it. A draft
	// of this design had this flag guarding exec's guests, which meant the
	// resident loop seized a person's live shell fifteen minutes in while a
	// crashed submit's guest leaked for a month unless somebody typed the
	// flag by hand. Refused on a loop by cli, as --superseded is.
	Unattributed bool
}

// ErrSeizureUnset is Discharge refusing a policy nobody chose.
var ErrSeizureUnset = errors.New("lease: no seizure policy was chosen; nothing discharged")

// Discharge retries the obligations it is given that the policy permits,
// and returns the ones that still stand. It is what `cycle` runs; a
// failed release is not an error the pass aborts on, it is an obligation
// that stands. The seize list is Standing's table: Seizable standings
// older than pol.Grace, and Unattributed only under pol.Unattributed.
// Everything else is returned untouched, with its Standing, for the
// report.
//
// Owed and Due go through Release (the claim is free to take). A lease
// still in Requested has no handle — that is the whole point of writing
// it before the call — so it goes through verify.LookupRequest on the id
// the caller assigned: Absent finishes it; Found hands the job to
// Fulfil; Unknown means it stands. Without that lookup a Requested lease
// could never be discharged at all, which is the failure an adversarial
// pass found in the first version of this. An Untracked worker is
// released through the provider directly, since no lease exists to claim
// — the one provider effect here that no record precedes, which is why
// its standing is decided by attribution and its seizure by a cutoff.
//
// A release the provider refuses stands as Owed with Release.NotBefore
// backoff, so a resident pass does not retry a refusing provider every
// tick.
//
// "The claim is free to take" is true of an obligation this pass has
// decided is its own, and that decision is Standing's — so the claim
// these roads take is claimIn's and not RequestIn's. The difference is
// exactly one guard: RequestIn refuses a claim another owner holds,
// because a caller with no Standing in its hand has no grounds to
// overrule one, and a restarted dispatcher discharging its predecessor's
// leases is precisely the case that must not be refused. Rule 1 is why
// the two are separate functions rather than one function with a flag:
// Outstanding observes, Discharge decides, claimIn effects.
//
// It reads the state ref once, for the backoffs. Release.NotBefore is a
// decision the LAST pass wrote, so it is read before anything is
// attempted rather than inside each takeover's closure — an Amend that
// only discovered it must do nothing would still have committed a
// commit and moved the ref for every backing-off obligation on every
// tick.
func Discharge(ctx context.Context, st *statestore.Store, prov verify.Verifier, obs []Obligation, pol Seizure, by Claimant, now func() time.Time) ([]Obligation, error) {
	if !pol.Set {
		return obs, ErrSeizureUnset
	}
	s, err := st.Read(ctx)
	if err != nil {
		return obs, err
	}
	var stands []Obligation
	for _, ob := range obs {
		if !mayTake(ob, pol, now()) || waiting(s, ob, now()) {
			stands = append(stands, ob)
			continue
		}
		if err := take(ctx, st, prov, ob, by, now); err != nil {
			stands = append(stands, ob)
		}
	}
	return stands, nil
}

// mayTake is Standing's table and the two narrowings, in one place so
// the seize list is stated once. An obligation whose kind nobody set is
// refused here rather than switched on later: Discharge does not guess a
// kind, and a value that reached this far without one is a caller's bug
// and not an environment to destroy.
func mayTake(ob Obligation, pol Seizure, now time.Time) bool {
	if ob.Kind == UnknownObligation || !ob.Standing.Seizable() {
		return false
	}
	if ob.Standing == Unattributed && !pol.Unattributed {
		return false
	}
	if ob.Kind == Untracked {
		// No age to measure and none needed: see Outstanding on the read
		// order, which closes the window a cutoff would otherwise guess
		// at. A provider's listing carries no creation time, and inventing
		// one would be a fact about nothing.
		return true
	}
	// A zero Since is an obligation whose age this pass could not
	// establish — a lease with no claim on it. It is reported and never
	// seized: rule 7 again, since an unknown age must not read as an old
	// one when the act on the other side is destructive.
	return !ob.Since.IsZero() && !now.Before(ob.Since.Add(pol.Grace))
}

// waiting reports a lease-backed obligation still inside the backoff a
// refused release wrote. Read off the record rather than off the
// Obligation, because the backoff is the state ref's fact and a caller
// may have been holding its list of obligations since before the last
// pass wrote one.
func waiting(s statestore.State, ob Obligation, now time.Time) bool {
	if ob.Request == "" {
		return false
	}
	l, ok := s.Leases[ob.Request]
	if !ok || l.Release == nil || l.Release.NotBefore == nil {
		return false
	}
	return now.Before(*l.Release.NotBefore)
}

// take performs one obligation, by kind. Every provider call in it is
// outside the store's lock and outside every closure, which is what the
// three-step shape is for.
func take(ctx context.Context, st *statestore.Store, prov verify.Verifier, ob Obligation, by Claimant, now func() time.Time) error {
	switch ob.Kind {
	case UnknownObligation:
		return errors.New("lease: an obligation with no kind is not dischargeable")
	case Untracked:
		return reclaim(ctx, prov, ob)
	case Requested:
		return resolve(ctx, st, prov, ob, by, now)
	case Owed, Due:
		l, took, err := seize(ctx, st, ob, by, now())
		if err != nil {
			return err
		}
		if !took {
			return errStands
		}
		out, ferr := fulfil(ctx, st, prov, l, now)
		if ferr != nil {
			return ferr
		}
		if !out.Closes() {
			return errStands
		}
		return nil
	}
	return nil
}

// errStands is this pass not closing an obligation, for any of the three
// reasons that are not a fault of its own: the state ref would not give
// it the claim (the lease is gone, or a subject is still building), the
// provider refused the release, or the provider could not say what
// became of a request.
//
// It is an error rather than a bool because the obligation must come
// back as STANDING, and a road that returned nil for "I did nothing"
// would drop it from the report — the pass would then say it had
// discharged an environment it had not touched. It stays unexported:
// Discharge's answer is the standing obligation itself, with its own
// Kind and Standing on it, and nothing outside decides on a sentinel
// that would only say the same thing twice.
var errStands = errors.New("lease: the obligation was not closed and still stands")

// seize takes the release claim over an obligation this pass has already
// decided is its own. It is Request with claimIn in place of RequestIn:
// the same transaction, the same write, one guard fewer, and the guard
// that is gone is the one Standing already answered.
//
// THE CLOSURE ASSIGNS BOTH OF ITS CAPTURES ON EVERY PATH, and that is
// the whole of what seizeIn exists for. statestore.Amend runs its
// closure AGAIN over a fresh read when the state ref loses its
// compare-and-set to a writer that never took the flock — a stray
// `git update-ref`, an older build, the ref recreated after ErrDocShape
// — so a closure that wrote its answer into a captured variable on one
// path and left it alone on another would carry the FIRST, discarded
// run's claim out of the last one. Written that way, a lost race
// returned a lease the committed transaction never claimed: take() went
// on to call the provider's Release on it, destroying an environment on
// the strength of a claim that is not in the store, and Discharge then
// reported the obligation discharged with no claim, no Release.Done and
// no lease behind it. A function whose returns ARE the assignment
// cannot leak across a retry, because there is nothing to leak into.
func seize(ctx context.Context, st *statestore.Store, ob Obligation, by Claimant, now time.Time) (record.Lease, bool, error) {
	var l record.Lease
	var took bool
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		l, took = seizeIn(tx, ob, by, now)
		return nil
	})
	if err != nil {
		return record.Lease{}, false, err
	}
	return l, took, nil
}

// seizeIn is seize's whole transaction body, as a pure function of the
// state it is handed: no live lease on the slot is the zero lease and
// false, every run, whatever the run before it found.
func seizeIn(tx *statestore.Txn, ob Obligation, by Claimant, now time.Time) (record.Lease, bool) {
	cur, ok := liveOn(tx.State(), ob.Change, ob.Platform)
	if !ok {
		return record.Lease{}, false
	}
	return claimIn(tx, cur, by, now)
}

// resolve is the Requested kind's road: a lease with a request token and
// no handle, which only the provider can answer for.
//
// The claim is taken FIRST and the lookup happens after it, so that two
// passes meeting the same stranded lease do not both ask and both act.
// Absent and Found are then recorded in the one shape everything else
// here uses — the answer is a provider's, the write is the store's, and
// nothing crosses the two.
func resolve(ctx context.Context, st *statestore.Store, prov verify.Verifier, ob Obligation, by Claimant, now func() time.Time) error {
	lookup, ok := prov.(verify.RequestLookup)
	if !ok {
		// A backend that cannot be asked about a request id cannot offer
		// unattended verification, which verify.RequestLookup says in
		// full. The obligation stands, and it stands for a reason a person
		// can act on rather than being silently retried forever.
		return errors.New("lease: the provider cannot be asked what became of a request")
	}
	l, took, err := seize(ctx, st, ob, by, now())
	if err != nil {
		return err
	}
	if !took {
		return errStands
	}
	obs, err := lookup.LookupRequest(ctx, l.Request)
	if err != nil {
		return stillOwed(ctx, st, l, err.Error(), now)
	}
	switch obs.State {
	case verify.Absent:
		return Confirm(ctx, st, l.Change, l.Platform, Absent,
			"the provider confirms nothing was created for this request", now())
	case verify.Found:
		l.ID = record.LeaseID{Provider: obs.Job.Provider, ID: obs.Job.ID, Started: obs.Job.Started}
		out, ferr := fulfil(ctx, st, prov, l, now)
		if ferr != nil {
			return ferr
		}
		if !out.Closes() {
			return errStands
		}
		return nil
	case verify.Unknown:
		return stillOwed(ctx, st, l, "the provider could not say what became of this request", now)
	}
	return errStands
}

// stillOwed records a lookup this pass could not act on and reports the
// obligation as standing. The claim stays taken and the backoff is
// written, so the next pass finds an Owed obligation rather than a
// Requested one and does not ask a silent provider again on every tick.
func stillOwed(ctx context.Context, st *statestore.Store, l record.Lease, detail string, now func() time.Time) error {
	if err := Confirm(ctx, st, l.Change, l.Platform, Failed, detail, now()); err != nil {
		return err
	}
	return errStands
}

// reclaim hands back an untracked worker through the provider's own
// Release, which is the one provider effect here that no record
// precedes. It goes through verify.Worker.Job and never through a name:
// that a job's id is the environment's name is one backend's fact, and
// the kernel does not learn it.
func reclaim(ctx context.Context, prov verify.Verifier, ob Obligation) error {
	if ob.Job.ID == "" {
		return errors.New("lease: the backend named no job for " + ob.Worker)
	}
	return prov.Release(ctx, ob.Job)
}
