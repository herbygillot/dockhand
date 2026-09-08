package publish

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Authorize is the whole gate ladder for both roads, in one ordered
// place, as a pure function. It performs no I/O and causes no effect.
// The invoker is an input, not a caller identity, so the human and
// machine precedence orders are visible side by side rather than
// implied by which of two functions you happened to call. The ladder:
// hold (change.Held with ActPublish and f.Invoker — a person's hold
// refuses both roads, a crossing's refuses the machine and advises the
// person); evidence; duplicate PR unless --no-pr-check; merged; drift;
// Fresh; Spent against pace, machine only; simplicity, machine only.
//
// FRESH IS ASKED FIRST AND NOT WHERE THE LIST PUTS IT, which is the
// sketch's own body's order and is the right one: it is a PRECONDITION
// on the fact set rather than a rung of the ladder. Three rungs below it
// — the duplicate, the merged dead end, the no-op — read Forge, and a
// ladder that asked them over a cached standing would have decided
// before it noticed it was deciding. The list is the order of the
// REFUSALS; this is the order of the code, and where they differ the
// precondition wins.
//
// THE EVIDENCE GATE REFUSES ONLY NEGATIVE EVIDENCE — a FAILED attempt on
// the tip, overridden by --ignore into an advisory the body states — and
// ADMITS an unverified tip with the unverified advisory, as
// verdict/publish.go:281-307 does today. That matters on the road A7
// found: a host with no provider mints without enqueueing (exit 0,
// "unverified; install tart and `dockhand verify`"), and cli then runs
// Promote for --to-pr, which must not refuse the very branch the tool
// just told the person it could not verify. The MACHINE road is stricter
// by its own Grant: GrantSimpleBumps requires a Passed attempt on the
// tip, so an unverified tip is never published unattended.
//
// THE SKETCH SAYS "a Failed, Blocked or Unsupported attempt" AND THAT
// PARENTHESIS IS WRONG, measured against the three sources it rests on,
// so this ladder refuses on Failed alone:
//
//   - The lines it cites refuse on Failed and nothing else.
//     verdict/publish.go's human road is `AnyState(record.Failed) &&
//     !NoVerify`; Blocked in the same function becomes an ADVISORY, one
//     per blocked run; Unsupported is not tested anywhere in it.
//   - record's own words contradict both additions. Unsupported "means
//     the port declines the platform — known_fail. That refusal is often
//     the change working, so it is not a failure." Blocked is "untested,
//     not disproven". Refusing on either would refuse a change on the
//     strength of something the record says is not evidence against it.
//   - The design says the same thing twice in its own resolutions:
//     "publish.Authorize refuses only negative evidence, as the shipped
//     road does" (§16.11) and "refuses negative evidence only, as the
//     shipped road does" (§16.12).
//
// So Blocked is an advisory naming the neighbour, as it is today, and
// Unsupported is an advisory too — a platform declining a port is worth
// a reviewer's knowing and is never a reason to withhold.
//
// AN OPEN OWN PULL REQUEST AT THE SAME TIP IS A NO-OP PERMIT, never a
// Spent increment: Forge.OwnFound, Own open and its head sha == Tip
// together return a Permit with no steps, which Apply performs nothing
// for and records nothing for. The shipped tree treats an open PR as
// work done; a draft of this spine dropped that and an adversarial pass
// priced it — a resident dispatcher at 5m re-applying publication to
// every open PR, 288 forge writes per PR per day, each counted against
// the allowance. Cycle's candidate population also excludes a change
// whose publication row is Open at the tip's Content, so the no-op is
// the backstop and not the normal case.
//
// IT SITS BELOW THE REFUSALS AND NOT ABOVE THEM, which is the sketch's
// own body's shape — the pace is asked before it there — and it is a
// choice worth stating because the other order is defensible: nothing is
// spent by a no-op, so nothing needs authorizing. What decides it is
// that the change is in front of reviewers RIGHT NOW, and a hold, a
// failed build or a merged dead end found over it is a thing the
// operator should hear about rather than a thing to answer "nothing to
// do" to. It costs a dispatcher nothing either, because the candidate
// population already excludes these.
//
// It REFUSES Facts whose Forge.Fresh is false (ErrNotFresh), so a
// cached standing can never reach a decision, and a machine whose
// Spent has reached the pace (ErrPaceSpent). Permit.Running is an
// advisory naming `dockhand cancel`.
func Authorize(f Facts, pace Pace) (Permit, []Advisory, error) {
	if !f.Forge.Fresh {
		return Permit{}, nil, ErrNotFresh
	}
	if f.Invoker != record.Human && f.Invoker != record.Machine {
		return Permit{}, nil, fmt.Errorf("%w: %q", ErrNoInvoker, f.Invoker)
	}
	machine := f.Invoker == record.Machine
	vs := verdicts(f.Change, f.Attempts)
	var adv []Advisory

	// 1 · THE HOLD, first, and before any other reading of the change.
	// A person's hold refuses both roads — it is the human's own
	// instrument, often placed to stop themselves, and one a publication
	// walked past would be note-keeping rather than a brake. A crossing's
	// refuses the machine and advises the person, which is change.Held's
	// table and not this function's.
	if err := change.Held(f.Change, change.ActPublish, f.Invoker); err != nil {
		return Permit{}, nil, err
	}
	if f.Change.Crossing.Warns() {
		adv = append(adv, Advisory{Kind: AdviseCrossing,
			Text: "this change takes " + headline(f.Change).Port + " out of stable"})
	}

	// 2 · THE GRANT, machine only. It is asked before any evidence is
	// weighed because a machine that may publish nothing has nothing to
	// weigh evidence for, and the refusal it is owed says so rather than
	// complaining about its proof.
	if machine && f.Unattended == GrantNothing {
		return Permit{}, adv, ErrNoGrant
	}

	// 3 · THE EVIDENCE. See the paragraphs above for what "negative"
	// means and why the machine's reading is stricter than the person's.
	a, err := evidence(f, vs, machine)
	adv = append(adv, a...)
	if err != nil {
		return Permit{}, adv, err
	}

	// 4 · THE OPEN PROPOSAL, machine only. A finding nobody has answered
	// is a question addressed to a person, and there is nobody on the
	// unattended road to have read it. A person is TOLD and allowed
	// through — promoting past their own advisory is their answer — which
	// is the difference this gate was reserved for.
	for _, p := range proposals(f.Change) {
		if machine {
			return Permit{}, adv, fmt.Errorf("%w: %s", ErrProposalOpen, p.Kind)
		}
		adv = append(adv, Advisory{Kind: AdviseProposal,
			Text: fmt.Sprintf("publishing with %s still proposed; `dockhand dismiss %s` records that you looked and said no",
				p.Kind, f.Branch)})
	}

	// 5 · THE FORGE'S SILENCE. A question that went unanswered is a
	// refusal for a machine and an advisory for a person, and it is asked
	// HERE — before the duplicate and the merged dead end — because those
	// two read Forge, and an unanswered own-PR lookup reads as "this
	// branch has no pull request", which is how an unattended pass comes
	// to open a second one beside somebody's first.
	if f.Forge.Err != nil {
		if machine {
			return Permit{}, adv, &ForgeSilentError{Branch: f.Branch, What: "this branch's pull requests", Err: f.Forge.Err}
		}
		adv = append(adv, Advisory{Kind: AdviseForge,
			Text: fmt.Sprintf("could not ask the forge about this branch's pull requests: %v", f.Forge.Err)})
	}

	// 6 · THE DUPLICATE, unless --no-pr-check (a person's override, never
	// a machine's). The same-port pull requests the walk went past are
	// advisories either way, and they are said BEFORE the refusal for the
	// reason the walk produced them in that order.
	for _, other := range f.Forge.SamePort {
		adv = append(adv, Advisory{Kind: AdviseSamePort,
			Text: samePortText(other, headline(f.Change).Port, bumpVersion(headline(f.Change)))})
	}
	if f.Forge.DuplicateFound && !f.Asks.noPRCheck(f.Invoker) {
		d := f.Forge.Duplicate
		return Permit{}, adv, &DuplicateError{Number: d.Number, Title: d.Title, URL: d.HTMLURL}
	}

	// 7 · THE MERGED DEAD END, on both roads. There is nothing left to
	// publish, and pushing to that branch would resurrect work the project
	// has already taken; `dockhand cycle` retires it.
	if f.Forge.OwnFound && prMerged(f.Forge.Own) {
		return Permit{}, adv, &MergedError{Number: f.Forge.Own.Number, Branch: f.Branch, URL: f.Forge.Own.HTMLURL}
	}

	// 8 · THE DRIFT. A machine refuses a change whose own portdir moved
	// underneath it since it was cut — somebody else has touched this port
	// upstream, which is a judgment about whose change should land and not
	// one an unattended pass may make — and refuses a drift it could not
	// measure at all (rule 7). A person is advised and publishes anyway if
	// they mean to.
	if d, ok := driftRefusal(f.Drift); ok {
		if machine {
			return Permit{}, adv, d
		}
		adv = append(adv, Advisory{Kind: AdviseDrift, Text: d.Error()})
	} else if f.Drift.Compared && f.Drift.BehindBy > 0 {
		adv = append(adv, Advisory{Kind: AdviseDrift,
			Text: fmt.Sprintf("the branch is %d commits behind its base", f.Drift.BehindBy)})
	}

	// 9 · THE PACE, machine only, against the DURABLE count. A person is
	// never paced, so Promote hands in a zero Pace and is never asked.
	if machine {
		if !pace.Set {
			return Permit{}, adv, ErrPaceUnset
		}
		if !f.Spent.Counted() {
			return Permit{}, adv, ErrSpendUnknown
		}
		if f.Spent.Within(pace.Window, f.AsOf) >= pace.Max {
			return Permit{}, adv, fmt.Errorf("%w: %d in the last %s", ErrPaceSpent,
				f.Spent.Within(pace.Window, f.AsOf), pace.Window)
		}
	}

	// 10 · THE NO-OP. Already in front of reviewers at this tip: no
	// steps, nothing performed, nothing recorded, nothing spent.
	if f.Forge.OwnFound && prOpen(f.Forge.Own) && f.Forge.Own.Head.Sha == f.Tip {
		return Permit{granted: true, facts: f, running: running(f), title: f.Title, body: f.Body}, adv, nil
	}

	// 11 · SIMPLICITY AND DIRECTION, machine only: the 2026-09-06 ruling
	// in the two halves it was made of. The edits must be confined to the
	// version, checksum and vendored regions (change.Judge over the
	// REALIZED change, recomputed and never read back off the record), and
	// the version must not move backwards in the sense base actually
	// means — which is a separate observation because Judge sees kinds and
	// counts, and a downgrade whose epoch edit the planner could not emit
	// arrives at Judge as {Version, Checksum, VendoredBlock}, every one of
	// them permitted.
	if machine {
		if err := simplicity(f); err != nil {
			return Permit{}, adv, err
		}
	} else if f.Direction.EpochOwed {
		adv = append(adv, Advisory{Kind: AdviseEpoch,
			Text: "this change moves the version backwards; without an epoch bump MacPorts will not upgrade an existing install"})
	}

	// 12 · THE BODY'S SIZE, last, because it is the only refusal here
	// that is about the bytes rather than about the change, and because
	// refusing it costs nothing while every step after this point spends
	// something. The shipped road learned this bound from `gh pr create`,
	// AFTER the branch was already on the fork — the worst outcome the
	// verb had.
	if size := utf8.RuneCountInString(f.Body); f.BodyLimit > 0 && size > f.BodyLimit {
		return Permit{}, adv, &BodyTooLongError{Branch: f.Branch, Size: size, Limit: f.BodyLimit}
	}

	for _, port := range unprovenMembers(f.Change, vs) {
		adv = append(adv, Advisory{Kind: AdviseUnproven,
			Text: port + " is published without a pass of its own"})
	}
	return Permit{
		granted: true,
		facts:   f,
		steps:   steps(f),
		running: running(f),
		title:   f.Title,
		body:    f.Body,
	}, adv, nil
}

// steps is what this permit authorizes, in the order Apply performs
// them: the push always, then the pull request — opened where the branch
// has none, refreshed where it has one this publication is moving.
//
// --no-pr stops at the push, which is a publication too: it puts the
// branch on the person's own fork, which is theirs and deletable at
// will, and spends nobody's attention. It is the one shape whose row
// carries no PR number.
//
// A REFRESH IS NOT A SPEND and is reachable by a machine, which is the
// one place this road is more permissive than the shipped one. The
// shipped tree refused an unattended republication outright
// (MachineRepublishError) because it had no way to tell a dispatcher
// re-applying publication to every open PR on every tick from a
// dispatcher moving a pull request it opened itself. record.RefreshPR
// and the no-op above are that way: the no-op catches the unchanged tip,
// so a refresh only happens when the branch has actually moved, and
// Spent never counts one — which is exactly what the adversarial pass
// asked for.
func steps(f Facts) []record.StepKind {
	out := []record.StepKind{record.PushBranch}
	switch {
	case f.Asks.NoPR:
		return out
	case f.Forge.OwnFound && prOpen(f.Forge.Own):
		return append(out, record.RefreshPR)
	default:
		return append(out, record.OpenPR)
	}
}

// evidence is the third rung, with the two roads side by side.
//
// The person: a change that clears the gate goes out; a completed
// FAILURE refuses unless --ignore, which turns it into an advisory the
// body states; anything else publishes with the unverified advisory and
// one line per blocked or declined run.
//
// The machine: a pass is required. A failure refuses whatever the asks
// say — --ignore is a person's override and there is no person — a run
// still going is PENDING and not a refusal, and what is left is a tip
// with no evidence either way, which is where the human road's whole
// permissiveness gets inverted.
func evidence(f Facts, vs []Verdict, machine bool) ([]Advisory, error) {
	if machine {
		switch {
		case promotable(f.Change, vs):
			return nil, nil
		case anyState(vs, record.Failed):
			return nil, &FailedError{Branch: f.Branch, Tip: git.Abbrev(f.Tip), Platforms: failedOn(vs)}
		case len(unfinished(vs)) > 0:
			return nil, &PendingError{Branch: f.Branch, Platforms: unfinished(vs)}
		default:
			return nil, fmt.Errorf("%w: %s at %s", ErrUnproven, f.Branch, git.Abbrev(f.Tip))
		}
	}
	if promotable(f.Change, vs) {
		return nil, nil
	}
	if anyState(vs, record.Failed) {
		if !f.Asks.ignore(f.Invoker) {
			return nil, &FailedError{Branch: f.Branch, Tip: git.Abbrev(f.Tip), Platforms: failedOn(vs)}
		}
		return []Advisory{{Kind: AdviseIgnored,
			Text: "publishing past a failed verification (" + strings.Join(failedOn(vs), ", ") + "); the PR body will say so"}}, nil
	}
	adv := []Advisory{{Kind: AdviseUnverified, Text: "publishing unverified; the PR body will say so"}}
	for _, v := range vs {
		switch v.Run.State {
		case record.Blocked:
			// A blocked run is the one unverified shape with a story worth
			// telling: the change is untested because a neighbour is broken,
			// and the maintainer deciding to publish anyway deserves the name
			// of the neighbour in front of them.
			what := "verification"
			if named(f.Change) {
				what = v.Port + "'s verification"
			}
			adv = append(adv, Advisory{Kind: AdviseBlocked,
				Text: fmt.Sprintf("%s blocked on %s: %s", what, v.Platform, v.Run.Detail)})
		case record.Unsupported:
			adv = append(adv, Advisory{Kind: AdviseBlocked,
				Text: fmt.Sprintf("%s declines %s (known_fail)", v.Port, v.Platform)})
		case record.Passed, record.Failed, record.Queued, record.Submitting, record.Running,
			record.Canceled, record.Superseded, record.Errored, record.Withheld:
			// Not a story about why this tip is unverified: a pass and a
			// failure are handled above, and the rest are this machine's own
			// afternoon, which the body states per run and a gate has nothing
			// to add to.
		}
	}
	return adv, nil
}

// simplicity is the machine's last pair of rungs: the edit confinement
// change.Judge measured over the realized change, and the direction the
// version actually moves.
//
// The two are separate because they can fail independently and because
// only one of them is about the edits. change.Judge's own comment says
// so: excluding edit.EpochBump from the permitted kinds is belt and
// braces, NOT the mechanism, because a downgrade whose epoch edit the
// planner could not emit — the insertion path declines on a carrier that
// is not top-level, shares its line, or sits in a subport block, and
// real downgrades do — arrives with only permitted kinds in it. The
// refusal has to hang on the movement, computed from the realized delta,
// which does not care what a planner managed.
func simplicity(f Facts) error {
	switch f.Simplicity {
	case change.Simple:
	case change.NotSimple, change.Unjudged:
		return &NotSimpleError{Branch: f.Branch, Why: f.Why}
	default:
		// A verdict this build cannot read withholds, for change.Unjudged's
		// reason: a machine must not publish what it could not classify.
		return &NotSimpleError{Branch: f.Branch, Why: f.Why}
	}
	if !f.Direction.Movement.Compared {
		if f.Direction.Err != nil {
			return f.Direction.Err
		}
		return ErrDirectionUnknown
	}
	if f.Direction.EpochOwed {
		return fmt.Errorf("%w: %s -> %s", ErrEpochOwed,
			f.Direction.Movement.From.Version, f.Direction.Movement.To.Version)
	}
	return nil
}

// driftRefusal is the drift the machine may not publish past, as an
// error, and false where there is none to refuse over.
//
// Two shapes. A drift that could not be COMPARED is rule 7: BehindBy
// zero would say "up to date" and the machine would publish on the
// strength of a failure. A comparison that found the change's own
// portdir moved underneath it says somebody else has touched this port
// upstream since the change was cut — which is a judgment about whose
// change should land, and an unattended pass does not make one.
//
// Being merely BEHIND is not a refusal on either road. Every branch is
// behind a moving main within the hour, and a rule that refused on it
// would refuse everything.
func driftRefusal(d change.Drift) (error, bool) {
	switch {
	case !d.Compared:
		if d.Err != nil {
			return fmt.Errorf("%w: %w", ErrDrifted, d.Err), true
		}
		return fmt.Errorf("%w: the drift could not be measured", ErrDrifted), true
	case d.OverTree:
		return fmt.Errorf("%w: an upstream commit has touched this change's own portdir since it was cut", ErrDrifted), true
	}
	return nil, false
}

// prOpen and prMerged are the forge's own spellings read as the two
// facts a publication turns on, mapped HERE because this is where the
// boundary is: gh answers with the JSON GitHub sent and keeps no opinion
// about it, and every road that weighs a pull request has to agree about
// what "open" and "merged" mean or the roads disagree about one PR. The
// shipped tree made the same mapping at the same kind of boundary
// (engine.PRFact).
func prOpen(pr gh.PullRequest) bool   { return pr.State == "open" }
func prMerged(pr gh.PullRequest) bool { return pr.MergedAt != "" }
