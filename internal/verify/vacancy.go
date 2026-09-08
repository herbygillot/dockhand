package verify

import (
	"context"
	"fmt"
	"io"
	"time"
)

// NoVacancyError is a submission refused for want of a slot, counted at
// the provider's own admission gate. Its identity is ErrNoVacancy, so
// errors.Is is the DECISION and errors.As is the OBSERVATION — the same
// rule-1 split the rest of this design makes with two values, made here
// with two idioms over one.
//
// IT CARRIES A CONTRACT, AND THE CONTRACT IS THE POINT: a provider
// returning this asserts that NOTHING WAS CREATED for the request. That
// is what lets lease.Acquire close the Requested lease it wrote moments
// earlier with lease.Absent — "confirmed not to exist: also done" — in
// the same pass, instead of leaving a phantom obligation for a
// LookupRequest round trip on every refusal of every queued attempt on
// every pass. On a saturated machine that is the difference between a
// refusal costing nothing and a refusal costing state-tree churn at
// exactly the moment N is worst.
//
// It is true of the only implementation — tart.Admit refuses under the
// machine-wide lock, before clone and before `tart run` — but it is a
// contract on an interface anyone may implement, so it wants a
// conformance test in the provider suite rather than this paragraph.
//
// Busy and Limit are the provider's own observation. WHETHER ANYBODY IS
// STANDING THERE WAITING IS NOT: the shipped tree's CapacityError had a
// Synchronous field whose own doc conceded "the provider cannot fill
// this in", and three call sites stamped it by MUTATING the error after
// the fact — one value carrying a provider's observation and a caller's
// decision, with the decision written in by mutation. That is rules 1
// and 2 broken together in eight lines, and it does not come back:
// app.Delivery already knows which road it is on.
//
// There is no retry-after. A provider genuinely cannot know when a slot
// frees — a build runs for minutes to hours — and a fabricated duration
// is worse than none. How long to wait is the run lifecycle's decision
// and record.Attempt.NotBefore is where it is written.
type NoVacancyError struct {
	Busy, Limit int
	AsOf        time.Time
}

func (e *NoVacancyError) Error() string {
	return fmt.Sprintf("verify: all %d slots busy (%d running as of %s)", e.Limit, e.Busy, e.AsOf.Format(time.RFC3339))
}

func (e *NoVacancyError) Is(target error) bool { return target == ErrNoVacancy }

// Vacancy is what a provider observed about its own free room at one
// moment.
//
// IT IS NOT CALLED Capacity, and the rename is worth its own paragraph
// because the first draft of this ruling made the mistake. `Capacity`
// sits one letter from `Capabilities` in this same package, and both are
// provider self-description — a reader skimming verify would have had to
// look twice at every use, forever, to tell a static declaration from a
// live observation. They are also opposite in kind: Capabilities is what
// this backend IS, answered without I/O and without failure; this is
// what it has free RIGHT NOW, which is I/O and can fail. Two questions
// (rule 2) deserve two words, not one word and a plural.
//
// Vacancy also names the FREE side rather than the total, which is what
// a scheduler asks about, and it gives the refusal its natural opposite:
// no vacancy. The policy axis keeps the word "cap" to itself —
// run.Admission.MaxQueued and MaxPerPass are caps an operator chooses,
// and run.Withholding's constants are named for them — so "cap" now
// means policy and "vacancy" means machine, with no word doing both.
//
// It moved here FROM run, which is the whole of the audit finding:
// run.Capacity's doc said the number "is asked of the PROVIDER", and it
// sat in a package the provider cannot reach — verify may not import run
// — so nothing could ever produce one. Moving it across the seam gives
// the orphan a producer and removes a duplicate in the same edit. run
// already imports verify, so no edge moves.
//
// IT IS AN OBSERVATION AND NEVER A PERMISSION, and that is not a
// nicety. tart's admission lock is held only from the count through the
// guest's boot, and its own comment concedes a person's `tart run` takes
// no lock at all — so a right-sized batch can still meet ErrNoVacancy
// mid-pass. The error is the authority; this is an economy. What it buys
// is not a saved RPC: every refused submit is a state-ref write, and N
// is what sets an Amend's critical section (91% of single-writer
// throughput at 100 records, 47% at 5,000). Starting forty to seat two
// costs thirty-eight writes that slow every other writer down.
//
// Known and AsOf are rule 7. Free == 0 alone means both "the machine is
// full" and "the provider could not be asked", so a provider that is
// DOWN would read as a busy one. The producer must derive Known from
// the listing succeeding AND parsing, not from the count being
// non-negative: tart's countRunning returns a bare int and yields 0 for
// output it could not parse, and under-counting means OVER-admitting.
//
// WHAT Known == false MUST MEAN is "order the queue and let the backend
// enforce", NOT "start nothing". The conservative reading is the defect:
// an unreachable provider would silently idle a healthy machine while
// the pass reported itself clean. Both defects run.Order exists to fix
// are ordering defects and they survive the loss of the count — Order
// takes no Vacancy at all; the sentinel is the gate.
type Vacancy struct {
	Known bool
	Free  int
	// Limit is the machine's ceiling, beside the live count and under the
	// same AsOf. It does NOT go back onto Capabilities as the shipped
	// tree's Concurrent int: that method takes no context and returns no
	// error, so it cannot carry a fact that is I/O and can fail, and a
	// ceiling read at one moment beside an occupancy read at another is
	// two values by rule 2. In the shipped tree Concurrent is a hardcoded
	// `concurrent = 2` and its only readers hand it straight back to
	// tart's own Admit, so nothing outside the provider ever read it.
	//
	// Free sits right beside it, so the subtraction run.Capacity's doc
	// recorded as this design's own mistake — a free count computed from
	// a cap minus THIS CHECKOUT's leases, two different scopes — is absurd
	// on its face here rather than merely forbidden in a comment.
	Limit int
	AsOf  time.Time
}

// Admits reports whether n more ENVIRONMENTS could have been seated as
// of AsOf — environments, not ports and not attempts, since a cohort is
// one guest for N members.
//
// THIS IS THE RULING'S HasCapacityFor(n), sited on the observed value
// rather than on the interface. Two reasons it belongs here. "Has
// capacity" is what a SCHEDULER concludes; a provider reports and does
// not decide, which is rule 1. And a bool coming back across the seam
// would mean both "the machine is full" and "I could not find out",
// which is rule 7 in a shape check_rule14.py cannot see — while here the
// unknown case has exactly one answer, and it is total.
//
// An unknown Vacancy admits nothing, which is the safe direction for a
// caller that asked. It is not the safe direction for a caller that
// never asked, which is why Known == false is a fact the report shows
// rather than a reason to stop: no starter reads it.
func (v Vacancy) Admits(n int) bool { return v.Known && v.Free >= n }

// VacancyReporter answers how much room the backend has right now. It
// is the accessor half of the 2026-09-06 capacity ruling, and it is an
// EIGHTH OPTIONAL INTERFACE rather than a sixth method on Verifier.
//
// Three reasons the optional form is the one written here.
//
// First, it does not reverse the ruling that the core Verifier stays
// five methods, and optionality is this seam's normal mode rather than
// its exception: verify already uses the pattern six times over, and
// RequestLookup's doc states the general reason — a backend that cannot
// answer is still a perfectly good interactive tool, and the type
// assertion is where that is decided rather than a sentence nothing
// enforces.
//
// Second, and this is the argument that actually settles it: making the
// method CORE does not remove the unknown case, it DUPLICATES it. A
// provider that cannot forecast — a shared-pool CI backend, where "can
// you take four more" has no yes/no answer because the honest reply is
// "yes, and you will queue" — must still write a body, and the only body
// it can write returns a zero that reads as "full". That is verbatim the
// failure Vacancy.Known exists to prevent, reproduced in every
// implementation instead of solved once. Optional gives "unknown"
// exactly one producer, at one assertion site, feeding a field already
// shaped to receive it.
//
// Third, the name. "Has capacity" is a scheduler's conclusion; a
// provider reports. The n-form the ruling asks for survives intact as
// Vacancy.Admits(n), which is where a caller wanted it anyway.
//
// It does NOT obsolete or overlap WorkerLister, and it is not the
// rejected draft returning. What run.Capacity's doc rejected was
// DOCKHAND computing Cap - Busy over a scope narrower than the cap;
// asking the provider is what that comment demands. The trap is live one
// layer down and belongs in the implementation's tests: tart's Workers()
// filters `tart list --quiet` by the dockhand-worker- prefix and does
// not filter by state, so Cap - len(Workers()) is wrong in both
// directions at once — it misses foreign running guests and counts
// dockhand's own stopped ones. The implementation must reuse the
// machine-wide running count, not the worker list.
//
// Vacancy MUST NOT return ErrNoVacancy. A full machine is a legitimate
// observation — Known true, Free zero — and letting the observer refuse
// would make the error mean both "full" and "could not ask", which is
// the defect this whole pass closes.
type VacancyReporter interface {
	Vacancy(ctx context.Context) (Vacancy, error)
}

// Streamer is the provider's live log, and it is a NINTH OPTIONAL
// INTERFACE for the same reason VacancyReporter is an eighth: the core
// Verifier stays five methods, and Log returns a FINISHED string, which
// is the right contract for evidence and the wrong one for watching. A
// backend that cannot stream is still a perfectly good verifier —
// `verify --trace` on it falls back to the finished log when the record
// settles, and says so.
//
// IT JUDGES NOTHING AND IT IS NOT A POLL. Stream copies bytes to w until
// the job ends or ctx does, and returns; the VERDICT still arrives
// through the record, written by whichever process is the judge by
// residency (R10). That is how R10 and R11 are both true at once: a
// --trace client streams the provider's bytes and watches the store for
// the verdict, and never calls Poll. A draft had run.Follow poll Status
// between chunks "to know when to stop", which made every tracing client
// a second observer of the job; the stream's own EOF is when to stop.
type Streamer interface {
	Stream(ctx context.Context, job Job, w io.Writer) error
}
