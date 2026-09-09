package statestore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// ErrRetentionUnset is Compact refusing a Retention nobody configured.
// It is the refusal Retention.Set exists for, and it is a sentinel
// because the caller that has to hear it is `cycle --compact`, one
// package away, and a road that could not tell "you did not choose a
// retention" from "git would not answer" would report a composition bug
// as a broken repository.
var ErrRetentionUnset = errors.New("statestore: no retention was chosen; nothing compacted")

// Compact drops records that are closed — settled attempts, returned
// leases, publications with a settled outcome, and changes in a closed
// state — from the TREE. It loses nothing: the ref is a commit chain, so
// every record it ever held is still reachable through its own history.
// Only the working set stays in the tree.
//
// The change had to be in that list and a draft's was not, which made
// the whole mechanism decorative: changes are the kind every other kind
// is keyed on and the kind that accumulates fastest, so a store that can
// drop the other three still grows without bound. That draft's
// record.Change had no state field for "closed" to land in at all, which
// is why the omission was invisible.
//
// WHAT IT ACTUALLY BUYS, since a draft of this design claimed the wrong
// thing and measurement settled it. The draft justified Compact on the
// READ path — "every read walks all of it". With the batch session Read is
// required to use, N is nearly free to read: 44 ms at five thousand records,
// 444 ms at fifty thousand. The read is not what Compact saves.
//
// What it saves is the WRITE critical section, and the flock ruling put
// that claim on BETTER footing rather than worse. Every Amend rewrites
// the whole flat tree — 28+len(name) bytes per entry, about 56 for a
// 16-hex id and 81 for a 40-hex one — so N sets how long one Amend takes.
//
// Under the compare-and-set alone that mattered because it set how wide
// the window was for a peer to lose a race, and the evidence was a
// lost-race measurement: 7.71/s at one writer against 7.02/s at eight
// over 100 records, and 4.09/s against 1.92/s over 5,000 — about 2x.
//
// Under a lock there are no lost races, so that measurement no longer
// applies and a weaker claim would be easy to leave standing. It is not
// weaker. With writers serialized, total throughput IS one over the
// critical section, so the single-writer figures above are the answer
// directly: 130 ms per Amend at 100 records, 244 ms at 5,000. Compact is
// worth 244/130 = 1.9x, and it is now a MEASURED claim about serialized
// time rather than an inference from races. Same magnitude, better
// evidence, and close to nothing on the read path either way.
//
// WHAT IT DOES NOT DO, all three measured:
//
//   - It reclaims NO disk. Every record it drops stays reachable through
//     the ref's own history, which is the same fact as "git log on the ref
//     is the archive". The archive property and the unreclaimability are
//     one property, not two.
//   - It does not reduce housekeeping pressure. An Amend writes k+2 objects
//     (k record blobs, one tree, one commit) — verified at k=1,5,20 giving
//     3.0, 7.0, 22.0 — and that count is independent of N. git's auto
//     maintenance triggers on loose-object count, so Compact cannot delay it.
//     BATCHING can, and by about 3x per unit of work: one amend touching
//     twenty records costs 22 objects where twenty amends cost 60.
//   - It does not bound N. N is Retention.ClosedFor times the closure rate,
//     PLUS every open record — and a selector sweep over a twenty-thousand
//     Portfile tree can enqueue tens of thousands of OPEN attempts, which
//     Compact cannot touch by definition. What bounds the store is a cap on
//     what a sweep may enqueue; Compact only bounds the closed tail.
//
// It is a maintenance operation with an explicit trigger, never automatic:
// dropping a record is the one thing here that cannot be undone by
// re-reading.
//
// Two floors that are not Retention's to waive: a machine publication
// row younger than keep.MachineWindow (see Retention), and a publication
// whose record.DeleteFork step is Requested or Uncertain — the retry
// population publish.ForkOwed walks, which "a settled outcome" would
// otherwise drop mid-obligation. There is no longer a pin exemption,
// because there is no longer a closed change with a pin: change.CloseIn
// deletes refs/dockhand/verify/<id> in the same batch that closes the
// record, so by the time Compact reads a closed snapshot its pin is
// already gone. COMPACT TOUCHES NO REF. A draft had it drop "orphan
// pins" — a pin whose change had closed, or whose record a crash never
// wrote. The first is unrepresentable now, because the close deletes the
// pin. The second is unrepresentable BY A CRASH, because the pin is
// created in the batch that writes the record — but not by a RECREATED
// state ref, which is this design's own remedy for a document of a
// foreign shape (Amend's doc) and for a bad ref (`update-ref -d`): that
// deletion leaves every refs/dockhand/verify/<id> standing with no record
// and no reflog pointing back at one (measured). That population has an
// observer and a person, never a sweep: `doctor` lists every ref
// git.Repo.RefsUnder(change.PinRef("")) returns whose id no record
// carries, with the by-hand remedy `git update-ref -d
// refs/dockhand/verify/<id>` (Q59). A closed change whose Branch is
// still set is a branch a policy KEPT (ReportOnly, WithholdDeletion) or
// a hand moved; it is the ref's fact whether it still stands, and
// Compact reads neither.
//
// What a person gets afterwards, since the tree no longer holds it:
// `git log` on the ref, which is the archive. dockhand itself does NOT
// read compacted records — Read is the tip only, there is no historical
// query, and adding one would be the tripwire above. So Retention has to
// be generous enough that `status` never needs history.
//
// THE AGE IS THE RECORD'S OWN AND AN UNKNOWN AGE KEEPS. Each kind
// carries its closing instant in a different field — a change's Closed,
// a lease's Release.Done, a publication's last step, an attempt's last
// verdict — and a record that is closed but says nothing about WHEN is
// kept rather than dropped, because a zero time read as "long ago" would
// make an unstamped record the first thing to go (rule 7).
func (s *Store) Compact(ctx context.Context, keep Retention) (int, error) {
	if !keep.Set {
		return 0, ErrRetentionUnset
	}
	if keep.MachineWindow <= 0 {
		return 0, fmt.Errorf("%w: no machine publication floor", ErrRetentionUnset)
	}
	now := time.Now()
	dropped := 0
	err := s.Amend(ctx, func(tx *Txn) error {
		// The closure may run twice, so the count is rebuilt from the
		// state this run was handed rather than added to across runs.
		dropped = 0
		st := tx.State()

		// THE ORDER IS THE DEPENDENCY ORDER, and that is the whole of what
		// this function was missing. It used to be four independent sweeps
		// over four local timestamps: each kind asked only its own age, so
		// a record could be dropped while something the pass KEPT still
		// needed it. A probe built an old closed change with an outstanding
		// fork deletion and compacted; the retention rule correctly kept
		// the publication and dropped the change — and publish.DeleteFork
		// then failed, because it reads the branch off the change. The
		// preserved obligation had been made unfulfillable by the same
		// pass that preserved it.
		//
		// So each kind is decided against what survives above it:
		// publications root changes, changes root attempts, attempts root
		// leases. Four passes, one direction, and nothing is dropped that
		// something still standing can still ask for.

		// 1 · PUBLICATIONS decide for themselves (mayDrop already refuses
		// to drop an unsettled row, an owed fork deletion, or a machine
		// row inside the allowance window).
		drop := map[string]bool{}
		for _, id := range slices.Sorted(maps.Keys(st.Publications)) {
			if keep.mayDrop(now, st.Publications[id]) {
				drop[publicationPrefix+id+docSuffix] = true
			}
		}

		// 2 · CHANGES, rooted by every publication that survives. A row
		// this pass is keeping names a change, and every question left to
		// ask of that row — which branch the fork copy is under, what was
		// published and at what content — is answered from the change.
		rooted := map[record.ChangeID]bool{}
		for _, id := range slices.Sorted(maps.Keys(st.Publications)) {
			if !drop[publicationPrefix+id+docSuffix] {
				rooted[st.Publications[id].Change] = true
			}
		}
		for _, id := range slices.Sorted(maps.Keys(st.Changes)) {
			c := st.Changes[id]
			if c.State.Closed() && keep.past(now, c.Closed) && !rooted[c.ID] {
				drop[changePrefix+id+docSuffix] = true
			}
		}

		// 3 · ATTEMPTS. A settled attempt is dropped when it is old enough
		// AND its change is over; an attempt whose change this pass is
		// dropping goes with it, whatever its own age, because it names a
		// change nothing can look up.
		//
		// AN OPEN CHANGE KEEPS ITS EVIDENCE, whatever the tail says. A
		// settled attempt is the only place a verdict lives, publication
		// candidacy is "a passed attempt at this tip", and adoption reuses
		// one across changes — so aging out an attempt under a change that
		// is still standing removes the evidence a live decision rests on.
		// The old rule asked only the attempt's own timestamp and did
		// exactly that.
		//
		// IT ASKED THE WRONG CHANGE, which is the half this comment
		// already argued for and the code did not do. "Its change" was the
		// one that ENQUEUED it, and adoption is precisely the case where
		// that is not the change relying on it: `--replace` supersedes the
		// enqueuer and mints a live change over the identical tree, so the
		// evidence became droppable at the moment it started being load-
		// bearing. Evidence is gathered by content everywhere else now
		// (publish.attemptsFor), so retention asks the same question: is
		// there ANY open change carrying these bytes.
		// THE RULE ONLY EVER KEEPS MORE. Retention is widened by the
		// content clause and nothing is narrowed by it: an attempt kept
		// before because its own change is open is kept still. Compaction
		// deletes records that no longer answer a question, and a pass
		// that started dropping rows it used to keep — a contentless
		// attempt under a live change, say — would be doing that on a
		// guess about which malformed records nobody wants.
		live := map[record.ContentID]bool{}
		for _, key := range slices.Sorted(maps.Keys(st.Changes)) {
			c := st.Changes[key]
			if c.Content != "" && !c.State.Closed() && !drop[changePrefix+string(c.ID)+docSuffix] {
				live[c.Content] = true
			}
		}
		for _, id := range slices.Sorted(maps.Keys(st.Attempts)) {
			a := st.Attempts[id]
			c, known := st.Changes[string(a.Change)]
			rested := known && !c.State.Closed() || live[a.Content]
			switch {
			case drop[changePrefix+string(a.Change)+docSuffix] && !live[a.Content]:
				// the enqueuer is going and nothing else carries these bytes
				drop[attemptPrefix+id+docSuffix] = true
			case !a.Settled() || !keep.past(now, settledAt(a)):
			case rested:
				// a live change rests on this verdict — its own, or one
				// carrying the same bytes: kept
			default:
				drop[attemptPrefix+id+docSuffix] = true
			}
		}

		// 4 · LEASES, rooted by the attempts that survive. A returned
		// lease is history; a returned lease a surviving attempt still
		// NAMES is the only account of which environment that verdict was
		// earned in, and a reader following record.Attempt.Lease to
		// nothing cannot tell "handed back long ago" from "never existed".
		named := map[string]bool{}
		for _, id := range slices.Sorted(maps.Keys(st.Attempts)) {
			a := st.Attempts[id]
			if a.Lease != "" && !drop[attemptPrefix+id+docSuffix] {
				named[a.Lease] = true
			}
		}
		for _, id := range slices.Sorted(maps.Keys(st.Leases)) {
			l := st.Leases[id]
			if l.Returned() && keep.past(now, l.Release.Done) && !named[id] {
				drop[leasePrefix+id+docSuffix] = true
			}
		}

		for _, name := range slices.Sorted(maps.Keys(drop)) {
			tx.Drop(name)
			dropped++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return dropped, nil
}

// past reports a closing instant old enough for the tail to have run
// out under it. A nil or zero instant is never past: an unknown age is
// not an old one, and the record stays.
func (r Retention) past(now time.Time, at *time.Time) bool {
	if at == nil || at.IsZero() {
		return false
	}
	return now.Sub(*at) > time.Duration(r.ClosedFor)*24*time.Hour
}

// mayDrop is the publication predicate, which carries two floors that
// are not this Retention's to waive: an unfinished fork deletion, which
// is an obligation publish.ForkOwed still walks, and a MACHINE row
// younger than the window — the rows publish.Facts.Spent is derived over
// in another process, where a row dropped early is an undercount and a
// machine that publishes past its allowance.
func (r Retention) mayDrop(now time.Time, p record.Publication) bool {
	if !p.Outcome.Settled() {
		return false
	}
	for _, step := range p.Steps {
		if step.Kind == record.DeleteFork && (step.Phase == record.Requested || step.Phase == record.Uncertain) {
			return false
		}
	}
	if p.By == record.Machine {
		// A machine row with no steps at all has no first step to be
		// younger than the window and nothing to date it by either, so
		// both rules agree that it stays.
		if len(p.Steps) == 0 || now.Sub(p.Steps[0].At) < r.MachineWindow {
			return false
		}
	}
	var last time.Time
	for _, step := range p.Steps {
		if step.At.After(last) {
			last = step.At
		}
	}
	if last.IsZero() {
		return false
	}
	return r.past(now, &last)
}

// settledAt is when a settled attempt actually finished: the latest
// instant any of its members' verdicts carries, falling back to the
// interruption that stopped it. An attempt whose runs are all unstamped
// answers with a zero time, which past() keeps.
func settledAt(a record.Attempt) *time.Time {
	var last time.Time
	for _, run := range a.Runs {
		if run.At.After(last) {
			last = run.At
		}
	}
	if a.Interrupt != nil && a.Interrupt.At.After(last) {
		last = a.Interrupt.At
	}
	if last.IsZero() {
		return nil
	}
	return &last
}

// Retention is what Compact keeps beyond the live set — a tail, so that
// `status` can still say something about work that finished recently
// without reading history.
type Retention struct {
	// Set is rule 7, and this type is the SIXTH to break it — caught by
	// the roll-call rather than by a reader, which is the point of having
	// one. ClosedFor == 0 meant both "keep no tail" and "nobody
	// configured this", on the one operation in this design that a
	// re-read cannot undo: an unconfigured Retention{} would have dropped
	// the entire closed tail while looking deliberate. That is
	// run.Admission's confession word for word, one package over, and the
	// two are the same shape passed the same way — Compact(ctx, keep
	// Retention) beside Admit(open, proposed, cap Admission).
	//
	// Compact refuses an unset Retention. It CAN refuse, where Admit
	// could not, because it already returns an error; what it cannot do
	// is tell an unset zero from a chosen one, and "keep no tail" is a
	// legitimate choice — it is the setting that maximises the measured
	// 1.9x on the write path.
	Set       bool
	ClosedFor int // days a closed record stays in the tree
	// MachineWindow is the floor under machine publication rows: one is
	// never dropped while its first step's At is younger than this,
	// WHATEVER ClosedFor says. It is stamped by app from publish.MaxWindow
	// — the one constant, passed in as a value (rule 5) because this
	// package may not import publish — and Compact refuses a zero here
	// the same way it refuses Set false: the floor is the one safety
	// property default-on publication has. publish.Facts.Spent is derived
	// over the rows Compact leaves, so a floor shorter than the pace
	// window is an undercount and a machine that publishes past its
	// allowance; a draft made that a cross-process check between
	// ClosedFor and --publish-every, which an adversarial pass showed
	// could only ever run in the process that did not carry the window.
	MachineWindow time.Duration
}
