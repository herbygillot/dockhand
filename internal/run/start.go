package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrNoChange is an attempt whose change record is gone: a compacted or
// hand-deleted change with a queued attempt still standing. Reported,
// never started.
var ErrNoChange = errors.New("run: the attempt's change has no record")

// ErrNotQueued is Start refusing an attempt somebody else already
// started. It exists because Start is called from four roads and two of
// them can meet the same attempt in one second — a person's `verify` and
// a resident dispatcher's drain — and the second to arrive must not boot
// a guest for work that already has one. The refusal is by IDENTITY and
// not by a re-read race: the phase is asked inside the lease Amend, so
// the loser sees the winner's write.
var ErrNotQueued = errors.New("run: the attempt is not queued")

// ErrSpecMismatch is Start refusing an attempt whose frozen roster does
// not hash to the SpecID beside it.
//
// It is a REFUSAL AND NOT A REPAIR. The id is what adoption, the queue
// and publication's verdict projection all join on, so an attempt whose
// two halves disagree cannot be started under either of them: building
// the roster would file evidence under a question nobody asked, and
// re-hashing the roster would rename work other records already point
// at. A person is the one who resolves it, by discarding the attempt and
// asking again.
//
// It should be unreachable in a tree that only ever writes both halves
// together at enqueue. It exists because the id used to be stored ALONE
// and re-derived from the change hours later, which silently produced
// exactly this disagreement and had nothing to notice it.
var ErrSpecMismatch = errors.New("run: the attempt's roster does not hash to its spec id")

// Start is the sequencer that turns a queued attempt into a running one,
// and it is the ONLY function that submits to a provider. Every road
// calls it — Change and Survey opportunistically, Cycle's drain in
// Order's order, Verify and Accept for what they enqueued — and it
// performs, in this order: Roster over the record; Stage (the re-plan);
// Plan over the preflight, which declines known_fail before any VM
// boots; lease.Acquire, which writes the Requested lease BEFORE the
// provider is called and closes it Absent itself on a capacity refusal;
// then ONE Amend advancing the attempt to Phase Active with its lease.
//
// ON verify.ErrNoVacancy NOTHING IS WRITTEN ON THE ATTEMPT — it stays
// Queued, the sentinel is returned unwrapped, and the caller stops
// submitting. A draft said "nothing written", which was untrue of the
// lease: Acquire writes a Requested lease and retires it Absent on the
// refusal in the same pass, and that is Acquire's own business and not
// this attempt's state. On ErrNoProvider or ErrNoEnvironment it stays
// Queued likewise and the caller reports a typed deferral. On a failure
// that IS the attempt's fault Start calls Defer, which writes the
// backoff Order reads.
//
// It NEVER asks whether the provider has room. R8: tart.Admit holds a
// machine-wide lock through clone and start, so check-then-act is racy;
// the sentinel is the authority and verify.Vacancy is an economy for
// status.
//
// IT PERFORMS NO HOLD CHECK. A draft began this body with one, and an
// adversarial pass showed what it cost: the crossing's born-hold is a
// record.Hold, so `bump amber-lang --to 0.3.2-alpha` enqueued and then
// refused its own opportunistic start, and no drain started it until
// `unhold` — which also lifted the publication hold. cli_spec flow 10
// (ruled) shows the attempt SUBMITTED with "the hold is on publication,
// not on the build". The person's own bump is the road that asked, and a
// person's `hold` is refused where it belongs: at Verify's and Accept's
// resolve (exit 23), and in Pending for the drain, both through
// change.Held with ActVerify, which a crossing never withholds. When the
// headline port has dependents, Stage also materializes Base.Sha's
// portdirs as the ABI baseline (verify.Request.Baseline), so the guest
// can build both sides and Observe can gather both manifests.
//
// TWO STARTERS MEETING ONE ATTEMPT COLLIDE AT lease.Acquire AND NOT
// HERE. A person's `verify` and a resident dispatcher's drain can reach
// the same queued attempt in one second, and Acquire refuses the second
// with ErrSlotTaken from inside its own Amend — under the store's flock
// and compare-and-set, before any provider is called — because a change
// already holding an environment on this platform may not boot a second.
// The phase check in the closure below is the belt to that braces: it
// costs nothing and it is the only thing standing between a peer that
// started the attempt through some road Acquire could not see and a
// record carrying two starts.
//
// EVERY MEMBER ANSWERED WITHOUT A BUILD ENDS IT HERE. Plan comes back
// ErrNothingToBuild when the whole roster declined or was withheld, and
// the answer is written — the attempt is Finished with those verdicts on
// it, no lease is taken and no guest is asked for — because a platform a
// port refuses is a verdict about that platform and an attempt left
// Queued would be retried at full cost on every pass forever.
func Start(ctx context.Context, st *statestore.Store, prov verify.Verifier, stage Stager, a record.Attempt, by lease.Claimant, now time.Time) (record.Attempt, error) {
	st0, err := st.Read(ctx)
	if err != nil {
		return a, err
	}
	c, ok := st0.Changes[string(a.Change)]
	if !ok {
		return a, ErrNoChange
	}
	if !a.Queued() {
		return a, ErrNotQueued
	}

	// THE QUESTION IS THE FROZEN ONE, replayed from the attempt, and it
	// used to be rebuilt from the change's CURRENT subjects and findings.
	// That made a queued attempt mean whatever the change meant by the
	// time a drain reached it: a probe queued one member, added a second
	// to the change, started the old attempt, and the provider received
	// both while the recorded Spec id sat unchanged. Two of the hashed
	// inputs were not on the record at all, so the id could not even be
	// recomputed honestly.
	spec := Frozen(a)
	if got := spec.ID(); got != a.Spec {
		// THE RECORD DISAGREES WITH ITSELF and this attempt is not
		// startable. It is a refusal and not a repair: the id is what
		// adoption, the queue and publication's projection all join on, so
		// a build started under a spec that hashes to something else would
		// file its evidence under a question nobody asked. Deferred like
		// any other fault of this attempt's own, so it backs off instead of
		// being retried at full cost every pass.
		return a, deferred(ctx, st, a, fmt.Errorf("%w: the record hashes to %s and carries %s",
			ErrSpecMismatch, got, a.Spec), now)
	}
	// THE ATTEMPT'S OWN PLATFORM FRAMES ITS PREFLIGHT. spec is Frozen
	// from the record, so the release is the one this build is bound for
	// — not the host's, and not whichever release a matrix listed first.
	staged, pre, err := stage.Stage(ctx, a.Sha, c.Subjects, spec.Platform)
	if err != nil {
		// A re-plan that could not materialize the commit is this
		// attempt's own fault in the only sense Defer cares about: nothing
		// about the machine's occupancy will make it work, and retrying it
		// at full cost every pass is the defect the backoff exists to end.
		return a, deferred(ctx, st, a, err, now)
	}
	spec = seat(spec, staged)
	// THE BASE'S OWN PORTDIRS, so the provider has a before to compare
	// the change against. A change with no recorded base has none, and a
	// staging that could not produce one is not a fault of this attempt:
	// the comparison degrades to "undescribed", which is exactly what
	// abi.Delta's Described flag exists to say.
	baseline, berr := stage.Baseline(ctx, c.Base.Sha, c.Subjects)
	var baselineNote string
	if berr != nil {
		// STILL NOT A FAULT OF THIS ATTEMPT — the comparison degrades to
		// "undescribed" and the build is worth running — but the reason
		// travels now instead of being dropped. It was dropped, and the
		// record then held baseline_source "none" with nothing to explain
		// it while a cohort proposal declined three layers downstream on
		// the strength of that nothing.
		baseline, baselineNote = nil, berr.Error()
	}
	req, declined, err := PlanWith(spec, pre, baseline, baselineNote)
	if errors.Is(err, ErrNothingToBuild) {
		return declineOnly(ctx, st, a, declined, now)
	}
	if err != nil {
		return a, deferred(ctx, st, a, err, now)
	}

	l, err := lease.Acquire(ctx, st, prov, a.Change, req, by, func() time.Time { return now })
	switch {
	case errors.Is(err, verify.ErrNoVacancy):
		// Nothing is written on the attempt: it stays Queued and the
		// sentinel travels unwrapped, so the caller stops submitting
		// without having to read a sentence. The lease Acquire wrote a
		// moment ago is already retired Absent — that is Acquire's own
		// business and not this attempt's state.
		return a, err
	case errors.Is(err, verify.ErrNoProvider), errors.Is(err, verify.ErrNoEnvironment):
		// The machine cannot verify, or cannot verify HERE. Both are facts
		// about the machine and never about the port, so the attempt stays
		// Queued for the pass that meets a provisioned machine, and the
		// caller reports the deferral.
		return a, err
	case err != nil:
		return a, deferred(ctx, st, a, err, now)
	}

	// ONE Amend advancing the attempt to Active with its lease and the
	// runs the submission started, beside the verdicts Plan reached
	// without a build. It is one write because a record that named a lease
	// and no runs, or runs and no lease, is a shape no reader can act on:
	// the settle road joins them.
	started := a
	if err := st.Amend(ctx, func(tx *statestore.Txn) error {
		cur, ok := tx.State().Attempts[a.ID]
		if !ok {
			return ErrNoChange
		}
		if !cur.Queued() {
			// A peer started it between the read above and this closure.
			// The lease is ours and the guest is running, so this is not a
			// clean refusal — but the record must not carry two starts, and
			// the caller learns which by the error.
			return ErrNotQueued
		}
		if !lease.ActiveIn(tx, l) {
			// The lease this call wrote is gone or was retired while the
			// provider was being asked — a peer's discharge decided the
			// submit had been stranded. The guest may be running, and the
			// attempt must NOT claim it: the record would name an
			// environment whose own document says nobody holds it, and the
			// reclaim road is what frees such a guest.
			return ErrNoLease
		}
		cur.Phase = record.Active
		cur.Lease = l.Request
		cur.Owner = by.Owner
		cur.Started = now.UTC()
		cur.Runs = startedRuns(spec, req, declined, now)
		cur.Unchecked = unchecked(pre)
		tx.PutAttempt(cur)
		started = cur
		return nil
	}); err != nil {
		return a, err
	}
	return started, nil
}

// unchecked is the preflight's failures, kept because they are the only
// part of a preflight that outlives Plan.
//
// Everything a preflight ANSWERED has already had its whole effect by
// the time this is called: a member declaring known_fail is not in the
// request and carries record.Unsupported, and use_xcode is in
// verify.Request.NeedsXcode. What it could not answer has had no effect
// at all, which is precisely why it has to be written down — run.Plan
// schedules an unread member as an ordinary build, and the person who
// reads the verdict hours later is the one who needs to know the
// question was never asked.
//
// Nil for a clean preflight, so the record carries the field only when
// it has something to say.
func unchecked(pre map[string]Preflight) map[string]string {
	var out map[string]string
	for port, pf := range pre {
		if pf.Read || pf.Err == nil {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[port] = pf.Err.Error()
	}
	return out
}

// startedRuns is what the attempt carries the moment the guest is
// asked: one Running run per member in the request, and the verdicts
// Plan reached without a build beside them.
//
// Every member the guest builds is Running and every one of them will
// have been linted, because that is what the runner was told to do —
// record.Run.Lint stays nil until a log is read, which is the field's own
// distinction between "no lint ran" and "linted, and it said nothing".
// Whether the archive was ignored is the MEMBER's own: the argv says it
// of one port, and a record that said it of the others would vouch for a
// build from source that never happened. Forced is the member's own for
// the same reason — the sibling this one deactivated is not a fact about
// any other member's build.
func startedRuns(spec Spec, req verify.Request, declined map[string]record.Run, now time.Time) map[string]record.Run {
	runs := make(map[string]record.Run, len(req.Ports)+len(declined))
	for port, r := range declined {
		r.At = now.UTC()
		runs[port] = r
	}
	forced := map[string]string{}
	for _, m := range spec.Roster {
		forced[m.Port] = m.Forced
	}
	fromSource := map[string]bool{}
	for _, p := range req.FromSource {
		fromSource[p] = true
	}
	for _, port := range req.Ports {
		runs[port] = record.Run{
			Ask: record.Ask{
				Test:       spec.Test,
				KeepEnv:    spec.KeepEnv,
				FromSource: fromSource[port],
				Forced:     forced[port],
			},
			State:   record.Running,
			Content: spec.Content,
			At:      now.UTC(),
		}
	}
	return runs
}

// declineOnly finishes an attempt every member of which was answered
// without a build — withheld, or declining this platform — in one Amend
// and with no lease taken.
//
// It is Finished and not left Queued, and that is the whole of why the
// case is written down: a queued attempt is retried by every pass, so an
// attempt nothing will ever build would boot a preflight, decline, and
// be tried again five minutes later for the life of the repository. The
// verdicts ARE the answer.
func declineOnly(ctx context.Context, st *statestore.Store, a record.Attempt, declined map[string]record.Run, now time.Time) (record.Attempt, error) {
	out := a
	err := st.Amend(ctx, func(tx *statestore.Txn) error {
		cur, ok := tx.State().Attempts[a.ID]
		if !ok {
			return ErrNoChange
		}
		if !cur.Queued() {
			return ErrNotQueued
		}
		cur.Phase = record.Finished
		cur.Runs = make(map[string]record.Run, len(declined))
		for port, r := range declined {
			r.At = now.UTC()
			cur.Runs[port] = r
		}
		tx.PutAttempt(cur)
		out = cur
		return nil
	})
	return out, err
}

// seat joins the frozen roster to the directories the stager just
// materialized, by port.
//
// IT ADDS A LOCATION AND CHANGES NO IDENTITY, which is what keeps
// Spec.ID stable across it: the id covers the roster by port, name and
// forced-sibling and never by path, so a spec seated on this host hashes
// to what the enqueue computed on another. A member the stager did not
// produce keeps an empty Portdir and is refused downstream rather than
// silently built from somewhere else.
func seat(spec Spec, staged []Member) Spec {
	dirs := make(map[string]string, len(staged))
	for _, m := range staged {
		dirs[m.Port] = m.Portdir
	}
	out := make([]Member, 0, len(spec.Roster))
	for _, m := range spec.Roster {
		m.Portdir = dirs[m.Port]
		out = append(out, m)
	}
	spec.Roster = out
	return spec
}

// deferred is Start's own fault road: record the backoff and hand back
// the cause, or hand back the cause alone where Defer refuses it.
//
// Defer's refusal is ignored on purpose — ErrNotAFault means the attempt
// is unchanged and the pass simply stops submitting — and the CAUSE is
// what travels, because a caller that reported the bookkeeping instead
// of the failure would tell a person their port was fine.
func deferred(ctx context.Context, st *statestore.Store, a record.Attempt, cause error, now time.Time) error {
	_ = Defer(ctx, st, a.ID, cause, now)
	return cause
}

// ErrNotAFault is what Defer returns when it is handed a cause that is
// not the attempt's fault. The caller does nothing with it: the attempt
// stays queued and the pass stops submitting. It exists so the refusal
// is a branch a reader can find rather than a paragraph a writer can
// forget.
var ErrNotAFault = errors.New("run: the cause is not this attempt's fault; nothing was deferred")

// Defer records that a submission failed for its own reasons and must
// not be retried until a later time. It is the write half of the
// backoff that Order's ordering READS, and a draft of this design
// had the ordering without it — so Order claimed to fix a defect it
// had no durable place to record.
//
// It is separate from Order because Order is a decision and this
// is an effect, which is rule 1: a permanently broken port must stop
// costing a VM on every nightly pass, and the fact that stops it has to
// survive the process that learned it.
//
// IT REFUSES A CAPACITY CAUSE, and that refusal is load-bearing rather
// than tidy. "Its own reasons" is the whole scope of this function, and
// a full machine is not the attempt's fault: the attempt is unchanged by
// the refusal and will submit fine when a slot frees. Fed one, Defer
// would advance Tries and push NotBefore out on a healthy port because
// the machine was busy — and on a two-slot machine a forty-attempt pass
// would back the entire queue off hours ahead while the machine sat
// idle. So at capacity nothing is written, the attempt stays queued, and
// the pass stops submitting.
//
// The refusal is mechanical rather than a sentence, because the failure
// it prevents is silent and the correct handling of ErrNotAFault is to
// ignore it. That is unusual enough to be worth the one branch: a caller
// that aborts a pass on it turns a benign full machine into a failed
// run.
//
// THE OTHER TWO MACHINE FACTS ARE REFUSED WITH IT, for the same reason
// spelled with different words: a host with no provider and a host with
// no environment for this platform are facts about the machine, they are
// fixed by provisioning rather than by the port, and an attempt backed
// off for one would still be waiting an hour after the person installed
// the thing that was missing.
func Defer(ctx context.Context, st *statestore.Store, attempt string, cause error, now time.Time) error {
	switch {
	case errors.Is(cause, verify.ErrNoVacancy),
		errors.Is(cause, verify.ErrNoProvider),
		errors.Is(cause, verify.ErrNoEnvironment):
		return ErrNotAFault
	}
	return st.Amend(ctx, func(tx *statestore.Txn) error {
		a, ok := tx.State().Attempts[attempt]
		if !ok {
			return ErrNoChange
		}
		if !a.Queued() {
			// Something started or finished it since the failure. A backoff
			// written now would delay an attempt that is no longer waiting.
			return ErrNotQueued
		}
		a.Tries++
		a.LastError = cause.Error()
		until := now.UTC().Add(backoff(a.Tries))
		a.NotBefore = &until
		tx.PutAttempt(a)
		return nil
	})
}

// backoff is how long a deferred attempt waits before the next pass
// tries it again: five minutes, doubling, capped at an hour — the same
// shape and the same two reasons lease.retryAfter has.
//
// The floor is the resident dispatcher's own cadence, so the first retry
// is the next pass and no sooner. The ceiling is there because the
// commonest reason a start fails for its own reasons is something a
// person is in the middle of fixing, and an hour is short enough that a
// fixed port comes back on its own and long enough that a permanently
// broken one is not costing a VM every tick until somebody notices.
func backoff(tries int) time.Duration {
	const base, ceiling = 5 * time.Minute, time.Hour
	d := base
	for i := 1; i < tries && d < ceiling; i++ {
		d *= 2
	}
	if d > ceiling {
		return ceiling
	}
	return d
}
