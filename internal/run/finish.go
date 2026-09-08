package run

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/darwin/abi"
	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// CohortCap is how many revision bumps one proposal may put forward. It
// is a cap on BUILDS and what is past it is a second cohort rather than
// a truncation — dependents.Propose says so and records the deferred
// members as examined — because a real tree collapses gdal's 82
// dependents into 39 portdirs and a proposal that put all of them in one
// guest would spend a day proving one library moved.
const CohortCap = 12

// SettleIn writes a judgment onto the attempt as a transaction step:
// Phase Finished, Runs (with each member's Manifest, Baseline and Probes
// off the evidence), Interrupt. It is this package's OWNED mutator for
// its kind, and Finish calls it in ONE Amend beside lease.RequestIn (the
// release claim), lease.RetainIn (the Keep deadline) and change.ProposeIn
// (the cohort finding) — four mutators, each exported by the package
// that owns its kind, one transaction. A draft exported a Store-level
// Settle that ran its own Amend and "discharged the environment outside
// the lock", naming no lease function that would: Release refuses over a
// claim already held, and run calling the provider itself is rule 4
// broken. lease.Fulfil is the function; this is the write; Finish is the
// order.
//
// IT STAMPS NO CLOCK OF ITS OWN. Every time on a settled attempt is
// already fixed by the moment the evidence was gathered — Judge writes
// Evidence.At onto each run and the Interrupt carries its own At — so a
// second reading here would put two answers to "when" on one record. The
// parameter stays because every other mutator in this design takes the
// caller's clock and a stamp added later belongs in one place.
//
// IT WRITES OVER THE STATE THE TRANSACTION WAS HANDED and not over the
// attempt the caller observed. The two can differ — a peer settled it,
// a cancel landed mid-poll — and the closure's own read is the one under
// the flock; a caller's stale copy written back would resurrect a verdict
// somebody else had already replaced. An attempt already Finished is
// left exactly as it stands, which is the no-op that makes a dispatcher
// taking the judge's chair mid-wait safe.
func SettleIn(tx *statestore.Txn, a record.Attempt, ev Evidence, j Judgment, _ time.Time) record.Attempt {
	cur, ok := tx.State().Attempts[a.ID]
	if !ok || cur.Settled() {
		return cur
	}
	cur.Phase = record.Finished
	cur.Interrupt = ev.Interrupt
	if cur.Runs == nil {
		cur.Runs = map[string]record.Run{}
	}
	for port, r := range j.Runs {
		cur.Runs[port] = r
	}
	// The backoff belongs to a submission that failed for its own
	// reasons, and this attempt is over: whatever it was waiting for, it
	// is not waiting any more.
	cur.NotBefore, cur.LastError = nil, ""
	tx.PutAttempt(cur)
	return cur
}

// Finish is the sequencer over one attempt, and there is exactly one
// order in which a verdict, its proposal and its release are written:
//
//	Observe                       the provider, while the lease is held
//	Judge                         pure; the one interpreter
//	propose                       local.Dependents + local.Instructions,
//	                              in-flight branches off the state, then
//	                              dependents.Propose (pure) — only when
//	                              Judge passed the headline and the abi
//	                              delta says the interface moved
//	ONE Amend                     SettleIn + lease.RequestIn (or
//	                              lease.RetainIn under Keep) +
//	                              change.ProposeIn
//	lease.Fulfil                  the provider call, outside the lock
//	Export                        the note onto the sha
//
// It is what every road that settles calls — status (no dispatcher),
// cycle's settle stage, the judge under --wait, Cancel and the stale
// stage — so they cannot disagree about the order. The propose step is a
// SEPARATE step and not part of Judge, because Judge as the cohort
// proposer would be two judgments in one function (rule 2) and would
// need portindex and Portfile I/O inside the one pure function this
// package has; it is here rather than in Accept's resolve because a
// proposal is made once, when the evidence is fresh, and read back by
// the person who answers it — Accept's resolve reads the stored finding
// and never re-derives.
//
// itr is the interrupt to read as evidence, or nil. With nil and a job
// still Running, Finish returns the attempt unchanged (Phase not
// Finished) and no error: a caller that loops on it is a judge waiting;
// a caller that meets it once is status reporting "still running". With
// an interrupt the job is stopped BY BEING RELEASED — under the ruled
// five-method Verifier there is no Cancel, and Release of a running
// guest is the stop — so Observe first gathers the log as evidence of
// where it got to, Judge reads the interrupt and returns every member
// Canceled or Superseded with ReleaseQuietly, the Amend writes verdict
// and claim together, and Fulfil performs the release. ONE JUDGE: a
// cancellation is an observation the judge reads, not a second
// verdict-writer, which is why no run.Cancel effect function exists.
//
// Under a Keep disposition — a failure's debug guest, a --keep-env pass
// — no claim is taken and lease.RetainIn writes now + lease.KeepFor
// instead, so the kept guest becomes a Due obligation on a deadline
// rather than a slot held until somebody remembers.
//
// Crash-safety is by rerun: a crash after the Amend leaves the lease
// Owed (lease.Discharge retries it) and the attempt Finished, so a rerun
// finds nothing to judge; a crash before it leaves the attempt Active
// and a rerun observes again. A Finished attempt is a no-op, which is
// what makes the residency handoff during a --wait safe.
//
// THE EXPORT'S FAILURE IS NOT THE SETTLEMENT'S. The note is a derived
// projection of a state that is already committed, so a re-export a
// later pass owes is a re-export and never a lost verdict; returning the
// error here would make a caller believe the judgment had not landed.
func Finish(ctx context.Context, st *statestore.Store, l *ledger.Ledger, prov verify.Verifier, local Local, a record.Attempt, spec Spec, itr *record.Interrupt, by lease.Claimant, now func() time.Time) (record.Attempt, error) {
	s0, err := st.Read(ctx)
	if err != nil {
		return a, err
	}
	cur, ok := s0.Attempts[a.ID]
	if !ok {
		return a, ErrNoChange
	}
	if cur.Settled() {
		// Already judged, by this process or by the dispatcher that took
		// the chair. The no-op is the point.
		return cur, nil
	}
	lse, held := s0.Leases[cur.Lease]
	if !held {
		return cur, ErrNoLease
	}

	ev, err := Observe(ctx, prov, lse, spec, cur.Runs)
	if err != nil {
		return cur, err
	}
	ev.Interrupt = itr
	// THE UNASKED QUESTIONS, carried from the attempt to the judge. The
	// preflight ran in the pass that STARTED this build, against a staged
	// tree that pass has since dropped; the attempt is the only thing
	// that still knows a member's Portfile would not evaluate.
	ev.Unchecked = cur.Unchecked
	if itr == nil && !ev.Vanished && !ev.Status.State.Terminal() {
		// Still building. Nothing is written — an unchanged attempt
		// rewritten is a state document per tick per attempt — and the
		// caller decides whether to loop.
		return cur, nil
	}
	j := Judge(ev)

	c := s0.Changes[string(cur.Change)]
	finding, propose, perr := proposeCohort(ctx, local, s0, c, cur, ev, j)
	if perr != nil {
		// A proposal that could not be made is not a finding of "no
		// dependents" (rule 7), and it is not a reason to withhold a
		// verdict either: the build happened and what it concluded is
		// owed to the record. The refusal travels back with the settled
		// attempt so the caller can say the check was unavailable.
		propose = false
	}

	settled := cur
	var claimed record.Lease
	var took bool
	at := now()
	if err := st.Amend(ctx, func(tx *statestore.Txn) error {
		settled = SettleIn(tx, cur, ev, j, at)
		// The provider's own name for the environment, once a Status has
		// reported one. It is written here and not at Acquire because the
		// two facts arrive from different calls — Submit answers with a
		// job and only a Poll answers with a handle — and it matters most
		// for a guest that is being KEPT, since the handle is what a
		// person types to go and look inside it.
		lease.HandleIn(tx, lse.Request, ev.Status.Handle)
		switch j.Disposition {
		case Keep:
			// The guest stands, on a deadline rather than until somebody
			// remembers: lease.Outstanding reads the Retain back as a Due
			// obligation once it passes.
			lease.RetainIn(tx, lse.Request, at.Add(lease.KeepFor))
			claimed, took = record.Lease{}, false
		case ReleaseAndReport, ReleaseQuietly:
			claimed, took = lease.RequestIn(tx, cur.Change, lse.Platform, by, at)
		}
		// WHAT BECAME OF THE ANALYSIS IS WRITTEN WITH THE VERDICT, in the
		// same transaction, so a failure has somewhere to be picked up
		// from. It used to travel back as an advisory after the attempt
		// was settled — and a later Finish returns immediately for a
		// settled attempt, so the only retry was a person noticing a line.
		settled.Analysis = analysisOf(perr, at)
		tx.PutAttempt(settled)
		if propose {
			return change.ProposeIn(tx, cur.Change, finding, at)
		}
		return nil
	}); err != nil {
		return cur, err
	}

	if took {
		// The provider call, outside the lock and over a claim this
		// transaction already took. Its failure is recorded as an
		// obligation by Fulfil's own Confirm and never raised here: a
		// guest that would not go back is not a verdict about the port,
		// and the verdict is already written.
		_ = lease.Fulfil(ctx, st, prov, claimed, now)
	}
	// The note is the readable copy on the commit; the store is the
	// authority, and it is already committed.
	_ = st.Export(ctx, l, settled.Sha)
	if perr != nil {
		return settled, perr
	}
	return settled, nil
}

// ErrNoLease is Finish meeting an Active attempt whose lease document is
// gone — compacted, or a state ref recreated under a running build. It
// is reported and never guessed past: without the lease there is no job
// to poll, and inventing one would poll a job no provider has.
var ErrNoLease = errors.New("run: the attempt names a lease the store does not hold")

// proposeCohort is Finish's cohort step: the guest half off the evidence
// (darwin/abi over the headline's Manifests), the local half through
// Local, in-flight branches from the state, and dependents.Propose over
// all of it. It returns the finding to write and whether there is one;
// it writes nothing and it is not exported, because the only caller is
// Finish and the only pure function tests need is dependents.Propose.
//
// IT RUNS ONLY WHERE SOMEBODY WOULD READ THE ANSWER. A port nothing
// depends on is never measured — the measurement's one consumer is the
// cohort decision, and a measurement on every bump in the tree would be
// a finding nobody reads — so the dependents are both the gate and the
// input, and they are read once. A headline that did not PASS is not
// measured either: a comparison against an installation a failed build
// left behind measures the change against a half-finished tree, and the
// proposal that rests on it would ask a person to revbump 39 ports over
// a build that never worked.
//
// A PROPOSAL ALREADY ANSWERED IS NOT MADE AGAIN. The cohort's own
// verification settles against the same content, so this would otherwise
// run a second time over the same dependents and ask a person a question
// they have already answered — accepted by the commit that seated the
// members, or dismissed by name.
func proposeCohort(ctx context.Context, local Local, s statestore.State, c record.Change, a record.Attempt, ev Evidence, j Judgment) (record.Finding, bool, error) {
	if local == nil || len(ev.Spec.Roster) == 0 {
		return record.Finding{}, false, nil
	}
	head := ev.Spec.Roster[0]
	if j.Runs[head.Port].State != record.Passed || answered(c) {
		return record.Finding{}, false, nil
	}
	rows, unread, err := local.Dependents(ctx, head.Port)
	if err != nil {
		// No index, or one that would not walk. Rule 7: no finding is not
		// a finding of "no dependents", so nothing is recorded and the
		// caller is told the check was unavailable.
		return record.Finding{}, false, err
	}
	if len(rows) == 0 {
		return record.Finding{}, false, nil
	}
	m := ev.Manifests[head.Port]
	delta := abi.Delta(abi.Input{
		Port: head.Port,
		// The change's own tree-relative portdir, which is what a reader
		// of the finding needs; head.Portdir is the stager's host path and
		// is empty on every settlement that did not just stage.
		Portdir:   subjectDir(c, head.Port),
		Described: m.Candidate != nil || m.Baseline != nil,
		// The headline's own run says whether the installed side ignored
		// the archive, because "measured against what was published" and
		// "measured against what this branch built" are different claims
		// about the after side.
		FromSource: j.Runs[head.Port].Ask.FromSource,
		Baseline:   m.Baseline,
		Candidate:  m.Candidate,
		Source:     abi.Source(m.Source),
		Reason:     m.Reason,
	})
	// THE CUES COME FROM THE COMMIT AND NOT FROM THE WORKING DIRECTORY.
	// The portdir is the change's own — the subject's tree-relative path,
	// which the record keeps — and the sha is the attempt's, so what is
	// read is the Portfile as this change left it. The old call passed
	// the roster member's Portdir, which is a host path the STAGER fills
	// in and which is empty on every settlement that did not just stage:
	// os.ReadFile then read whatever Portfile was under the process's
	// working directory, and the error was discarded.
	quotes, ierr := local.Instructions(ctx, a.Sha, subjectDir(c, head.Port))
	missed := ev.Unavailable
	if ierr != nil {
		// The maintainer's cues are an input and not a gate: a Portfile
		// that could not be read leaves the measurement to speak alone,
		// which is what it did before the cues existed. It is NOT silent
		// though — a cue names ports the index cannot, so a proposal made
		// without them may be missing members, and the person deciding on
		// it is the one who has to know that.
		quotes = nil
		missed = append(missed, "the maintainer's cues in "+subjectDir(c, head.Port)+
			" could not be read ("+ierr.Error()+"), so any port they name is unaccounted for here")
	}
	deps, short := dependents.From(rows, unread, inFlight(s, c.ID), carried(c))
	f, ok := dependents.Propose(delta, quotes, deps, short, CohortCap).Finding()
	if ok {
		f.Criterion = withUnavailable(f.Criterion, missed)
	}
	return f, ok, nil
}

// withUnavailable appends what this settlement asked for and did not get
// to the criterion the proposal was reached on.
//
// It is the criterion because that is the durable sentence a person
// meets when they are asked to revbump thirty-nine ports: the finding is
// what the record keeps and what `status` and the pull request body
// render, and a caveat anywhere else would not travel with the question
// it qualifies.
//
// THE STACK IS THE DEFECT, not any one absorption. Three reads on this
// path swallow their errors with a correct rule-7 rationale each — an
// absence is not a finding — and together they made a completely broken
// analysis indistinguishable from a clean one. Each is still absorbed;
// none is still silent.
func withUnavailable(criterion string, missed []string) string {
	if len(missed) == 0 {
		return criterion
	}
	out := criterion
	for _, m := range missed {
		if out == "" {
			out = m
			continue
		}
		out += "; " + m
	}
	return out
}

// answered reports whether a person has already given this change's
// revbump proposal an answer. The measurement is not repeated for one
// that has: asking again is asking somebody twice.
func answered(c record.Change) bool {
	for _, f := range c.Findings {
		if f.Kind == record.KindABIDependents && f.Disposition != record.Proposed {
			return true
		}
	}
	return false
}

// inFlight names, per folded port, the branch already carrying a change
// to it — read off the STATE and not off git, because a branch is a
// binding on a change and the record is what says which change binds it.
// Two branches revbumping one port is two revisions and a conflict at
// merge, so such a port is examined and left out with the reason.
//
// The change being settled is excluded from its own answer: its members
// are already carried, which is a different exclusion with a different
// sentence, and a change that read itself as in flight would exclude
// every member it is about to propose.
func inFlight(s statestore.State, self record.ChangeID) map[string]string {
	out := map[string]string{}
	for _, c := range s.Changes {
		if c.ID == self || !c.Bound() || c.Branch == "" {
			continue
		}
		for _, sub := range c.Subjects {
			out[strings.ToLower(sub.Port)] = c.Branch
		}
	}
	return out
}

// carried is the set of ports this change already has as subjects.
//
// It is a different fact from inFlight and needs its own exclusion,
// because the settlement that measures a cohort is the cohort's own
// verification: the members are subjects of the change being settled, so
// a second pass over the same content re-measures, re-reads the same
// dependents, and would propose revbumping ports this very commit has
// already revbumped.
func carried(c record.Change) map[string]bool {
	out := map[string]bool{}
	for _, s := range c.Subjects {
		out[strings.ToLower(s.Port)] = true
	}
	return out
}

// subjectDir is a member's TREE-RELATIVE portdir, off the change's own
// subjects. It is the durable answer to "where does this port live",
// and the one every reader of a settled attempt needs: run.Member's
// Portdir is a host path the stager fills in at start, so it is empty
// for the settle, status and cancel roads that replay a frozen roster.
func subjectDir(c record.Change, port string) string {
	for _, sub := range c.Subjects {
		if sub.Port == port {
			return sub.Portdir
		}
	}
	return ""
}

// analysisOf is the post-build work's durable state after one try. A
// success — including the success that concluded there was nothing to
// propose — is Finished; a refusal is Uncertain with the cause and a
// backoff, which is the same shape Release and Step carry for the same
// problem.
func analysisOf(err error, at time.Time) *record.Analysis {
	if err == nil {
		return &record.Analysis{Phase: record.Finished, At: at.UTC()}
	}
	until := at.UTC().Add(analysisBackoff(1))
	return &record.Analysis{Phase: record.Uncertain, At: at.UTC(),
		Detail: err.Error(), Attempts: 1, NotBefore: &until}
}

// analysisBackoff is how long a refused analysis waits: five minutes,
// doubling, capped at an hour. It is lease.retryAfter's schedule and its
// reasoning — the floor is the resident dispatcher's own cadence, and
// the ceiling is short enough that a fixed index comes back on its own
// and long enough that a permanently broken one is not re-walked on
// every tick.
func analysisBackoff(attempts int) time.Duration {
	const base, ceiling = 5 * time.Minute, time.Hour
	wait := base
	for i := 1; i < attempts && wait < ceiling; i++ {
		wait *= 2
	}
	return min(wait, ceiling)
}

// Analyse is the post-build work as a RETRY: the ABI comparison and the
// cohort proposal, over the evidence a settled attempt already holds.
//
// IT READS THE RECORD AND NOT LIVE EVIDENCE, which is what makes it
// replayable at all. record.Run keeps each member's Manifest, Baseline
// and BaselineSource, so everything abi.Delta needs survives settlement;
// what was missing was a state saying the analysis was owed and a caller
// that acted on it.
//
// It writes the finding and the analysis state in ONE transaction, the
// same pairing Finish makes, so an attempt never reads as analysed with
// no finding or the reverse.
func Analyse(ctx context.Context, st *statestore.Store, local Local, a record.Attempt, now func() time.Time) error {
	s0, err := st.Read(ctx)
	if err != nil {
		return err
	}
	c, ok := s0.Changes[string(a.Change)]
	if !ok {
		return ErrNoChange
	}
	ev, j := replay(a)
	finding, propose, perr := proposeCohort(ctx, local, s0, c, a, ev, j)
	at := now()
	return st.Amend(ctx, func(tx *statestore.Txn) error {
		cur, held := tx.State().Attempts[a.ID]
		if !held || !cur.AnalysisOwed() {
			return nil // somebody else finished it
		}
		tries := 1
		if cur.Analysis != nil {
			tries = cur.Analysis.Attempts + 1
		}
		if perr != nil {
			until := at.UTC().Add(analysisBackoff(tries))
			cur.Analysis = &record.Analysis{Phase: record.Uncertain, At: at.UTC(),
				Detail: perr.Error(), Attempts: tries, NotBefore: &until}
			tx.PutAttempt(cur)
			return nil
		}
		cur.Analysis = &record.Analysis{Phase: record.Finished, At: at.UTC(), Attempts: tries}
		tx.PutAttempt(cur)
		if propose {
			return change.ProposeIn(tx, cur.Change, finding, at)
		}
		return nil
	})
}

// replay rebuilds the two values proposeCohort reads out of a settled
// attempt's own record: the manifests it measured, and the verdicts it
// reached. It is the durable half of Evidence and Judgment, and nothing
// here asks a provider anything — the build is long over.
func replay(a record.Attempt) (Evidence, Judgment) {
	ev := Evidence{Spec: Frozen(a), Manifests: map[string]Manifests{}}
	j := Judgment{Runs: map[string]record.Run{}}
	for port, r := range a.Runs {
		j.Runs[port] = r
		ev.Manifests[port] = Manifests{
			Baseline:  r.Baseline,
			Candidate: r.Manifest,
			Source:    r.BaselineSource,
		}
	}
	return ev, j
}

// Owed is every settled attempt whose post-build analysis is still
// owed and past its backoff, in a stable order.
func Owed(s statestore.State, now time.Time) []record.Attempt {
	var out []record.Attempt
	for _, id := range sortedAttempts(s) {
		a := s.Attempts[id]
		if !a.Settled() || !a.AnalysisOwed() {
			continue
		}
		if a.Analysis.NotBefore != nil && now.Before(*a.Analysis.NotBefore) {
			continue
		}
		out = append(out, a)
	}
	return out
}
