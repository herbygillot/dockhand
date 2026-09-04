package verdict

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
)

// DuplicatePRError is promote's refusal when an open upstream PR
// already claims the same change: a duplicate spends reviewer
// attention on the purest kind of waste. Refusal with a remedy, not a
// failure — the other PR may be theirs to join, or --no-pr-check
// promotes past it deliberately.
//
// It exits in the refused band rather than the declined one: nothing
// is wrong with the change, and what will not take it is the
// destination.
type DuplicatePRError struct {
	Title string
	URL   string
}

func (e *DuplicatePRError) Error() string {
	return fmt.Sprintf("an open PR already proposes %q: %s — join it, retitle with --title, or --no-pr-check to promote anyway", e.Title, e.URL)
}

// DockhandExit: the destination already carries this change.
func (e *DuplicatePRError) DockhandExit() int { return exitcode.DuplicatePR }

// Code names the refusal for a machine.
func (e *DuplicatePRError) Code() string { return "duplicate-pr" }

// FailedVerificationError is promote's refusal over distinct negative
// evidence: the tip's verification ran to completion and the port did
// not build. It is the one thing an unverified branch's promotion
// cannot talk its way past, and --no-verify is the deliberate override.
//
// Typed, and in the verdict band with the failure it is refusing over,
// because a caller telling "the build says no" from "promote broke"
// was previously reading prose to do it. The words are unchanged from
// when it was a plain error; only its identity is new.
type FailedVerificationError struct {
	Branch string
	// Tip is the tip's shortened sha, abbreviated by the caller.
	Tip string
}

func (e *FailedVerificationError) Error() string {
	return fmt.Sprintf("%s: tip %s has a failed verification — fix it, `dockhand discard` it, or --no-verify to promote anyway",
		e.Branch, e.Tip)
}

// DockhandExit: the verdict band — the same band the failing run itself
// exits in, because this refusal is that verdict being enforced.
func (e *FailedVerificationError) DockhandExit() int { return exitcode.VerifyFailed }

// Code names the refusal for a machine.
func (e *FailedVerificationError) Code() string { return "verification-failed" }

// PRMergedError is a promotion whose own pull request already merged:
// there is nothing left to publish, and pushing to that branch would
// resurrect work the project has already taken.
//
// It exits in the refused band: the destination is a dead end rather
// than a failure, and `dockhand clean` is the remedy.
type PRMergedError struct {
	Number int
	Branch string
	URL    string
}

func (e *PRMergedError) Error() string {
	return fmt.Sprintf("PR #%d for %s already merged (%s) — `dockhand clean` retires the branch",
		e.Number, e.Branch, e.URL)
}

// DockhandExit: the destination will take nothing more.
func (e *PRMergedError) DockhandExit() int { return exitcode.PRMerged }

// Code names the refusal for a machine.
func (e *PRMergedError) Code() string { return "pr-merged" }

// PromotionPendingError is the machine's answer to a change whose
// verification has not finished: nothing is wrong, nothing is settled,
// and the next unattended pass will ask again.
//
// It exits in the pending band and never in a refusal one, which is the
// whole reason it is a separate type from the refusals below. An
// unattended reconciler runs on a timer; a change waiting for its own
// build is the ordinary state of half the namespace on any given pass,
// and a caller that read "waiting" as "refused" would page somebody
// about work that is proceeding exactly as it should.
type PromotionPendingError struct {
	Branch string
	// Platforms are the releases whose runs have not finished, in the
	// record's stable order. Named rather than counted: which build is
	// still going is something a reader can look up, and "1 run" is not.
	Platforms []string
}

func (e *PromotionPendingError) Error() string {
	return fmt.Sprintf("%s: verification has not finished (%s); the next pass will ask again",
		e.Branch, strings.Join(e.Platforms, ", "))
}

// DockhandExit: the pending band — nobody's problem yet.
func (e *PromotionPendingError) DockhandExit() int { return exitcode.PromotionPending }

// Code names the outcome for a machine.
func (e *PromotionPendingError) Code() string { return "promotion-pending" }

// UnprovenError is the machine's inversion of promote's most permissive
// rule.
//
// A person invoking promote has already made the publication choice, so
// an unverified branch goes out with a complaint: they are looking at
// the complaint. An unattended pass has nobody looking, and absence of
// evidence there is not a candour problem but the absence of any reason
// to spend a reviewer's attention at all. The machine road therefore
// requires POSITIVE evidence — a pass — and refuses on anything less,
// including the states a person is merely warned about.
//
// It is in the refused band and specifically at the machine gate: the
// change is fine, the road refused it, and a person asking for the same
// thing would be allowed it. That is the definition of the code.
type UnprovenError struct {
	Branch string
	// Tip is the tip's shortened sha, abbreviated by the caller.
	Tip string
}

func (e *UnprovenError) Error() string {
	return fmt.Sprintf("%s: tip %s carries no passing verification — an unattended publication needs positive evidence, not the absence of a failure; `dockhand promote %s` publishes it on a person's authority",
		e.Branch, e.Tip, e.Branch)
}

// DockhandExit: the refused band's machine gate.
func (e *UnprovenError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *UnprovenError) Code() string { return "no-positive-evidence" }

// Phase is how far a change has already travelled toward review. It is
// what makes an unattended pass idempotent: the machine acts on a change
// that has not gone out, and a change that already has is work this pass
// has nothing to do about — not an error, and not a refusal.
//
// It is read from the pull request and not from the note. The record
// says where the change was BOUND when it was minted, which a stale note
// or a hand-opened PR can both contradict; the forge says where it has
// actually GOT to, and that is the fact an idempotent pass must key on.
type Phase string

const (
	// PhaseInFlight is a change with no pull request: the only phase the
	// machine may act on.
	PhaseInFlight Phase = "in-flight"
	// PhasePublished is a change whose pull request is open. Reviewers
	// are already looking at it; there is nothing for an unattended pass
	// to do and nothing wrong.
	PhasePublished Phase = "published"
	// PhaseRetired is a change whose pull request merged or closed. An
	// end state, and the sweep's business rather than the publisher's.
	PhaseRetired Phase = "retired"
)

// PhaseOf reads the phase off the pull request the caller looked up. A
// lookup that found nothing is in flight, which is what "no pull
// request" means — and it is why a FAILED lookup must never be handed in
// as the zero fact on the machine road: absent and unknown would become
// the same answer, and the unattended pass would open a second pull
// request for a branch that already has one.
func PhaseOf(pr PRFact) Phase {
	switch {
	case !pr.Found:
		return PhaseInFlight
	case pr.Merged, !pr.Open:
		return PhaseRetired
	default:
		return PhasePublished
	}
}

// PublishAsk is one publication put to the gate: the change, the
// evidence reading the caller took, and — the two facts the machine road
// turns on — who is asking and how far the change has already got.
//
// A struct rather than seven positional arguments, and every field named
// at the call site, because two of them are the difference between the
// road a person walks and the road nobody walks: a By silently defaulted
// at a new call site is the one way the inversions below stop meaning
// anything.
type PublishAsk struct {
	Record record.Record
	// Promotable is the caller's reading of the verdict set: some run
	// passed, none failed, and every subject was proven.
	Promotable bool
	Branch     string
	// Tip is the tip's shortened sha, abbreviated by the caller.
	Tip string
	// NoVerify publishes past a failed verification. It is a person's
	// override and is NOT honoured on the machine road — there is nobody
	// on that road to have decided it.
	NoVerify bool
	// By is who is asking. The zero value is record.Human, which is every
	// verb a person types.
	By record.Driver
	// Phase is how far the change has already got, read from the forge by
	// the caller. It is consulted on the machine road only: a person
	// re-promoting an open pull request is refreshing it on purpose.
	Phase Phase
}

// by is who this publication is attributed to, with the zero value read
// as the caller every verb has today.
func (a PublishAsk) by() record.Driver {
	if a.By == "" {
		return record.Human
	}
	return a.By
}

// PublishDecision is what promote's verification gate concluded.
type PublishDecision struct {
	// Refusal stops the promotion. On the human road the one thing that
	// refuses is a completed failed build, so it is a
	// FailedVerificationError and it exits in the verdict band — with the
	// run whose answer it is enforcing, rather than among the ways
	// promote itself can break. The machine road adds two: a pending
	// verification and an unproven tip.
	Refusal error
	// Blocked is one advisory per blocked run, in the record's stable
	// order, for stderr.
	Blocked []string
	// SayUnverified asks the caller to say so before it publishes. Never
	// set on the machine road, which refuses instead of complaining.
	SayUnverified bool
	// NoOp says there is nothing to do and nothing wrong: the change has
	// already gone out. It is separate from a nil Refusal because those
	// two mean opposite things to the caller — one says publish, the
	// other says move on — and it is separate from a refusal because an
	// unattended pass that exited non-zero over work it had already
	// finished would page somebody every time it succeeded.
	NoOp bool
}

// DecidePublish is promote's gate.
//
// Invoking promote is already the publication choice, so an unverified
// branch promotes with a complaint and never a demanded flag. The one
// refusal that remains is distinct NEGATIVE evidence: a completed failed
// build, which --no-verify alone overrides. Everything else — nothing
// verified, a run still queued, a platform that declined — passes, and
// the PR body says what the evidence was.
//
// A blocked run is the one unverified shape with a story worth telling:
// the change is untested because a neighbour is broken, and the
// maintainer deciding to promote anyway deserves the name of the
// neighbour in front of them.
//
// THE MACHINE ROAD INVERTS IT, and decideForMachine below states each
// inversion beside the human rule it inverts. Everything above is an
// argument about a person who is standing there: the choice was already
// made by typing the verb, the complaint reaches somebody, the blocked
// neighbour's name is put in front of a reader. Take the reader away and
// every one of those arguments fails, so the unattended road requires a
// pass, refuses what it cannot prove, and reports a run still going as
// pending rather than as anything's fault.
func DecidePublish(a PublishAsk) PublishDecision {
	if a.by() == record.Machine {
		return decideForMachine(a)
	}
	if a.Promotable {
		return PublishDecision{}
	}
	if a.Record.AnyState(record.Failed) && !a.NoVerify {
		return PublishDecision{Refusal: &FailedVerificationError{Branch: a.Branch, Tip: a.Tip}}
	}
	d := PublishDecision{SayUnverified: true}
	named := Names(a.Record)
	for _, ref := range Runs(a.Record) {
		if ref.Run.State != record.Blocked {
			continue
		}
		// The subject is named only for a cohort. One member's block is
		// the whole change's, and prefixing the single port a branch is
		// named for would tell the maintainer something the branch name
		// on the line above already said.
		what := "verification"
		if named {
			what = ref.Port + "'s verification"
		}
		d.Blocked = append(d.Blocked, fmt.Sprintf("%s blocked on %s: %s", what, ref.Platform, ref.Run.Detail))
	}
	return d
}

// decideForMachine is the unattended road, one inversion at a time.
//
// The order is not cosmetic. Phase comes first because a change already
// in front of reviewers is not a candidate at all, and every sentence
// after this point would be about work that does not need doing. Then a
// pass, which is the only thing that lets an unattended publication
// through. Then the failure, unconditionally — --no-verify is a person's
// override and there is no person. Then a run still going, which is
// pending and not a refusal. What is left is a tip with no evidence
// either way, and that is where the human road's whole permissiveness
// gets inverted: a person publishes it with a complaint, the machine
// does not publish it at all.
func decideForMachine(a PublishAsk) PublishDecision {
	if a.Phase != PhaseInFlight {
		return PublishDecision{NoOp: true}
	}
	if a.Promotable {
		return PublishDecision{}
	}
	if a.Record.AnyState(record.Failed) {
		return PublishDecision{Refusal: &FailedVerificationError{Branch: a.Branch, Tip: a.Tip}}
	}
	if unfinished := Unfinished(a.Record); len(unfinished) > 0 {
		return PublishDecision{Refusal: &PromotionPendingError{Branch: a.Branch, Platforms: unfinished}}
	}
	return PublishDecision{Refusal: &UnprovenError{Branch: a.Branch, Tip: a.Tip}}
}

// Unfinished names the platforms whose runs will still change on their
// own, in the record's stable order and without repeats.
//
// It asks the state's own Terminal, so a state this build cannot read
// counts as unfinished: the reading that waits is the reading that
// cannot publish something on a verdict it did not understand.
func Unfinished(r record.Record) []string {
	var out []string
	seen := map[string]bool{}
	for _, ref := range Runs(r) {
		if ref.Run.State.Terminal() || seen[ref.Platform] {
			continue
		}
		seen[ref.Platform] = true
		out = append(out, ref.Platform)
	}
	return out
}

// MergedDeadEnd refuses a promotion whose own PR already merged. Nil
// when the branch's PR is anything else, including absent — and nil
// as the untyped error value, never a typed nil, since the callers
// test it against nil directly.
func MergedDeadEnd(pr PRFact, branch string) error {
	if !pr.Found || !pr.Merged {
		return nil
	}
	return &PRMergedError{Number: pr.Number, Branch: branch, URL: pr.URL}
}

// PortName is the port to search open pull requests by: the one the
// note names, or — for a branch with no note at all — the prefix of the
// title, leaning on the project convention that a title is
// `<port>: <description>`. Empty when neither answers, which is the
// caller's cue to skip the duplicate search rather than run an
// unbounded one.
func PortName(recorded, title string) string {
	port := recorded
	if before, _, found := strings.Cut(title, ":"); port == "" && found {
		port = strings.TrimSpace(before)
	}
	return port
}

// DuplicateCheck is what the same-port search concluded.
type DuplicateCheck struct {
	// Refusal is the duplicate, when one was found.
	Refusal error
	// Notes are the same-port-different-change advisories, for stderr.
	// They are returned even alongside a refusal, and the caller prints
	// them before returning it: the search reports each PR as it walks
	// past, so the advisories for everything ahead of the duplicate are
	// already said by the time it is found.
	Notes []string
}

// CheckDuplicates walks the open pull requests that claim the same port.
// A title matching this promotion's, case- and space-insensitively, is
// the duplicate; anything else on the same port is not a duplicate but
// is worth knowing now rather than at review, because a maintainer
// coordinating both changes will want to.
//
// own is this branch's own pull request, skipped by number: re-promoting
// updates that PR in place, and matching against it would refuse the
// branch for duplicating itself.
func CheckDuplicates(open []PRFact, own PRFact, title string) DuplicateCheck {
	var d DuplicateCheck
	for _, pr := range open {
		if own.Found && pr.Number == own.Number {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(pr.Title), strings.TrimSpace(title)) {
			d.Refusal = &DuplicatePRError{Title: pr.Title, URL: pr.URL}
			return d
		}
		d.Notes = append(d.Notes, fmt.Sprintf("note: an open PR already touches this port: #%d %q (%s)",
			pr.Number, pr.Title, pr.URL))
	}
	return d
}
