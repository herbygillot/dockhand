package run

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Enqueue is what EnqueueIn writes. Sha is REQUIRED — the pre-mint gate
// that had no sha is deleted — and EnqueueIn refuses an empty one, since
// an attempt no drain could materialize is a row that waits forever.
// EnqueuedBy is who asked (Q7: yes, one field), which Owner cannot say
// once a dispatcher is usually the process that starts what a person
// queued. Ask is carried on the attempt so the start honours the
// enqueuer's --test and --keep-env and not the dispatcher's flags.
type Enqueue struct {
	Change     record.ChangeID
	Sha        string
	Content    record.ContentID
	Spec       Spec
	Platform   platform.Release
	Ask        record.Ask
	EnqueuedBy record.OwnerID
}

// ErrNoSha is EnqueueIn's refusal of an attempt with nothing to build.
var ErrNoSha = errors.New("run: an attempt must name the commit it builds")

// EnqueueIn writes a queued attempt — Phase Requested, no lease — and it
// is a PLAIN WRITER. It decides nothing about attempts already standing:
// a draft of the synthesis folded "is an open attempt for this content,
// spec and platform already standing?" into this function, returning the
// match and announcing it, which put a decision and an announcement
// inside an Amend closure that is pure by contract and may run more than
// once (rule 1, and statestore.Amend's own doc). The decision is
// Adoptable's, pure, taken by the operation INSIDE the same closure over
// tx.State() with the match returned as DATA — not over a State read
// before the Amend, which a first fix allowed and the reconciliation
// deleted, since a read outside the flock can miss an attempt the
// concurrent enqueue it is racing just wrote. EnqueueIn is simply not
// called for an adopted attempt, and the announcement is app's, after
// the Amend returns, from the attempt the closure handed back.
//
// THE ADOPTION KEY is (Content, Spec.ID, Platform), and Spec.ID is
// computed at enqueue from RECORD-DERIVABLE inputs — Content, Platform,
// the roster by identity and the Ask (Test, FromSource, Requires; never
// KeepEnv or Trace) — so that a drain re-deriving the Spec from the
// attempt hours later computes the same id, and the shipped Spec's
// staged Portdirs (a filesystem fact) are outside it. A key that
// included a path would match nothing across processes.
//
// It takes a Txn rather than a Store because every road that enqueues
// writes the attempt beside something else — change.MintIn on Change and
// Survey, change.ExtendIn on Accept, change.AdoptIn on Verify — and a
// sweep writes N of each in one transaction. Two hundred admitted
// targets through two hundred Amends is the write storm the admission
// cap exists to prevent, performed by the operation that obeys it.
//
// THE ID IS MINTED IN THE CLOSURE, and that is safe for the reason a ref
// line queued in one is: a closure that runs twice mints twice, and only
// the run that commits writes anything — Amend returns after the batch
// lands, so the attempt handed back is the one that stands. Deriving the
// id from the state instead would make two concurrent enqueues on one
// content collide on a name rather than on the compare-and-set.
func EnqueueIn(tx *statestore.Txn, e Enqueue, now time.Time) (record.Attempt, error) {
	if e.Sha == "" {
		return record.Attempt{}, ErrNoSha
	}
	a := record.Attempt{
		Schema:     record.DocSchema,
		ID:         mintAttempt(),
		Change:     e.Change,
		Spec:       e.Spec.ID(),
		Content:    e.Content,
		Sha:        e.Sha,
		Platform:   e.Platform.Name,
		Owner:      e.EnqueuedBy,
		EnqueuedBy: e.EnqueuedBy,
		Ask:        e.Ask,
		Started:    now.UTC(),
		Phase:      record.Requested,
		Members:    memberPorts(e.Spec.Roster),
	}
	tx.PutAttempt(a)
	return a, nil
}

// mintAttempt makes an attempt's durable id. Random rather than derived
// from the change and the tip, because a change is verified, fails, and
// is verified again at the same tip — a derived id would address the
// previous attempt's record and overwrite the evidence a person is
// reading. Hex and sixteen bytes, on lease's precedent: the id is a
// document name in a flat tree, and a collision would silently join two
// verifications.
func mintAttempt() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "att-" + hex.EncodeToString(b[:])
}

// memberPorts is the roster as the record spells it: one port per
// member, in build order, headline first.
func memberPorts(members []Member) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Port)
	}
	return out
}

// Adoptable is the answer to "is an attempt for exactly this question
// already standing?" — an open (Queued or Active) or Passed attempt with
// this content, spec and platform, which a re-queue should take over
// rather than pay for twice. Ruled 2026-09-06: adoption is AUTOMATIC.
// Its scenario moved with the gate's deletion — there is no interrupted
// `bump --verify` to resume any more (R12) — and what survives is the
// re-queued attempt: `verify` run twice on one tip, or a dispatcher
// meeting a queued attempt a person also just asked for.
//
// IT IS PURE AND IT IS CALLED INSIDE THE ENQUEUE AMEND'S CLOSURE, over
// tx.State(), with the match returned as data through a variable the
// closure captures — never over a State read before the Amend, which a
// first fix permitted and the reconciliation deleted: a read outside
// the flock can miss the attempt a concurrent enqueue just wrote, and
// two `verify`s of one tip a second apart would both pay. It is never
// called from inside EnqueueIn: an adversarial pass caught the synthesis
// folding this decision into that writer, inside a closure
// statestore.Amend may run more than once, and returning-and-announcing
// from it — rule 1 broken and the Amend contract with it. EnqueueIn is
// simply not called when this returns true, and app announces the
// adoption, naming the attempt and when it started, after the Amend.
//
// Both identities must match and both must be non-empty on both sides,
// so a caller holding a zero SpecID cannot adopt anything by accident.
// A FINISHED Passed attempt is the best case: its verdict is already
// earned. A running one is adopted by watching (AwaitRecord). What
// adoption cannot give back is a TRACE of a build that already ended:
// Trace is outside the SpecID so a --trace rerun still matches, and a
// finished adoptee refuses the trace in favour of `dockhand log`.
//
// WHICH ONE, when several match: the best answer rather than the first
// the map hands over. A Passed attempt beats an open one — its verdict
// is earned and costs nothing — and among equals the most recently
// started wins, because that is the one whose evidence describes the
// state of the world the caller is asking about. A map walk with no
// order would adopt a different attempt on two identical calls.
//
// It takes a clock it does not read, for Pending's reason: adoption is
// decided entirely by identity — content, spec, platform — and nothing
// here expires. A draft that aged attempts out of the population would
// be choosing a staleness policy inside a predicate, where the caller
// asking "has this already been answered" cannot see it; if one is ever
// wanted it lands here rather than at every call site.
func Adoptable(attempts []record.Attempt, content record.ContentID, spec record.SpecID, plat platform.Release, _ time.Time) (record.Attempt, bool) {
	if content == "" || spec == "" {
		return record.Attempt{}, false
	}
	var best record.Attempt
	var found bool
	for _, a := range attempts {
		if a.Content != content || a.Spec != spec || a.Platform != plat.Name {
			continue
		}
		if !adoptableState(a) {
			continue
		}
		if !found || better(a, best) {
			best, found = a, true
		}
	}
	return best, found
}

// adoptableState is the population Adoptable draws from: an attempt
// still in flight, or one that already passed.
//
// A FAILED attempt is not adoptable, and that is the whole point of
// naming the states rather than testing "not settled". A person
// re-asking for a verification of a tip that failed is asking for it
// again — the archive changed, a dependency was fixed, the machine was
// sick — and handing them the failure they already have would make the
// verb unable to retry anything.
func adoptableState(a record.Attempt) bool {
	if a.Queued() || a.Active() {
		return true
	}
	return a.Settled() && passed(a)
}

// passed reports an attempt every member of which reached a pass. A
// cohort with one failed member is not a proof of the change, so it is
// not a verdict a re-queue may inherit.
func passed(a record.Attempt) bool {
	if len(a.Runs) == 0 {
		return false
	}
	for _, r := range a.Runs {
		if r.State != record.Passed {
			return false
		}
	}
	return true
}

// better ranks two adoptable attempts: a passed one over an unfinished
// one, then the more recently started.
func better(a, b record.Attempt) bool {
	if ap, bp := a.Settled() && passed(a), b.Settled() && passed(b); ap != bp {
		return ap
	}
	if !a.Started.Equal(b.Started) {
		return a.Started.After(b.Started)
	}
	return a.ID > b.ID
}

// Roster derives the members a guest seats from the RECORD — the
// change's subjects, minus the members an Accepted cohort finding
// withheld, with a forced member LAST and its deactivated sibling named
// on Member.Forced — so the cohort is seated identically by the bump
// that queued it, by cycle's drain and by a later verify. A draft had
// Accept compute this in-process and hand it to Submit, which meant the
// drain, meeting the same queued attempt an hour later, would have had
// to seat it from a different function. Pure, over two records.
//
// THE ATTEMPT IS READ TOO, and not only the change. A member the last
// submission recorded Withheld is one this attempt must not seat either
// — the guest will not activate it beside the sibling it conflicts with,
// whatever the change's findings have since been answered to — and a
// member whose run carries a Forced sibling was a person's override and
// goes last with that sibling named. Where the attempt has no runs at
// all — a cohort accepted with --no-verify, or one queued before any
// release resolved — the accepted proposal's candidates say the same
// thing from the other side (Solo, Over, Forced), and a road that read
// only runs would seat the withheld member beside its sibling and drop
// the person's override on the floor.
//
// Portdir is left EMPTY here. This function reads records and the staged
// path is a filesystem fact the Stager mints at the moment of starting;
// Start joins the two by port. That is also why Spec.ID can be computed
// at enqueue and re-computed at drain and agree.
func Roster(c record.Change, a record.Attempt) (members []Member, withheld []Withheld) {
	cands := map[string]record.Candidate{}
	for _, f := range c.Findings {
		if f.Kind != record.KindABIDependents || f.Disposition != record.Accepted {
			continue
		}
		for _, cand := range f.Candidates {
			cands[strings.ToLower(cand.Port)] = cand
		}
	}
	var seated, tail []Member
	for _, s := range c.Subjects {
		lp := strings.ToLower(s.Port)
		run, ran := a.Runs[s.Port]
		cand, proposed := cands[lp]
		m := Member{Port: s.Port, Names: append([]string(nil), s.Names...)}
		switch {
		case ran && run.State == record.Withheld:
			// Bumped, never built: the guest must not hold it beside its
			// sibling, whatever the record's subjects say.
			withheld = append(withheld, Withheld{Port: s.Port, Why: run.Detail})
		case ran && run.Ask.Forced != "":
			m.Forced = run.Ask.Forced
			tail = append(tail, m)
		case !ran && proposed && cand.Proposed && cand.Solo && cand.Forced && cand.Over != "":
			m.Forced = cand.Over
			tail = append(tail, m)
		case !ran && proposed && cand.Proposed && cand.Solo:
			withheld = append(withheld, Withheld{Port: s.Port, Why: cand.Reason})
		default:
			seated = append(seated, m)
		}
	}
	// A forced member is built LAST, after every member that might need
	// the sibling it deactivates, and the tail is ordered by name so two
	// processes seating the same cohort seat it identically — which is
	// what makes the SpecID of a re-derived spec equal to the one the
	// enqueue computed.
	sort.Slice(tail, func(i, j int) bool { return tail[i].Port < tail[j].Port })
	return append(seated, tail...), withheld
}

// WithdrawIn marks every QUEUED attempt of a change Finished, with an
// Interrupt carrying why (Canceled for a discard, Superseded for a
// supersede or a retire) and every member's run state to match, as a
// transaction step. It is this package's owned mutator for the write a
// close needs, and it is called in the SAME Amend as change.CloseIn or
// change.SupersedeIn — Discard's close, Cycle's retire, Survey's and
// --replace's supersede — which is rule 4's two owned mutators. It
// exists because Finish has no job to observe on a queued attempt, so
// the roads that stop live work through Finish left queued work
// standing: a queued attempt on a change retire had just closed was
// neither Held nor Superseded, Pending returned it, Start staged the tip
// by sha and booted a guest for a demolished change; and the ones
// Pending did refuse stayed Phase Requested forever, counted by
// run.Count against MaxQueued for the life of the repository. It
// returns what it withdrew, for the row.
func WithdrawIn(tx *statestore.Txn, id record.ChangeID, why record.InterruptWhy, by record.OwnerID, now time.Time) []record.Attempt {
	s := tx.State()
	var out []record.Attempt
	for _, key := range sortedAttempts(s) {
		a := s.Attempts[key]
		if a.Change != id || !a.Queued() {
			continue
		}
		at := now.UTC()
		a.Phase = record.Finished
		a.Interrupt = &record.Interrupt{Why: why, By: by, At: at, Detail: withdrawnDetail(why)}
		if a.Runs == nil {
			a.Runs = map[string]record.Run{}
		}
		for _, port := range a.Members {
			r := a.Runs[port]
			r.State, r.Detail, r.At = withdrawnState(why), withdrawnDetail(why), at
			a.Runs[port] = r
		}
		tx.PutAttempt(a)
		out = append(out, a)
	}
	return out
}

// withdrawnState is the member state a withdrawal writes, from the same
// typed cause the interrupt carries — so a person reading the record
// months later sees one word for what stopped it and never two accounts.
func withdrawnState(why record.InterruptWhy) record.RunState {
	if why == record.InterruptSuperseded {
		return record.Superseded
	}
	return record.Canceled
}

// withdrawnDetail is the sentence beside that word. Nothing decides from
// it — the cause is typed on the Interrupt — and it says what a reader
// needs: the build never started, so there is no verdict to look for.
func withdrawnDetail(why record.InterruptWhy) string {
	if why == record.InterruptSuperseded {
		return "withdrawn before it started: the change was superseded"
	}
	return "withdrawn before it started: the change was closed"
}
