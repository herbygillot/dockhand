package engine

// The reconciler's publish slot: the ONE machine publish path.
//
// Everything the human road does loosely, this road inverts, and each
// inversion is here rather than in promote because the argument for the
// loose version is always "a person is standing there" and there is no
// person. The list is short and it is the whole design:
//
//   - it acts on a change nobody has published yet, and treats one
//     already in front of reviewers as work done rather than as an error;
//   - it requires a PASS, where a person publishes an unverified change
//     with a complaint;
//   - it never cancels a running verification, where a person promoting
//     mid-build has thereby answered the question;
//   - it refuses while any finding is still proposed, where a person is
//     told and allowed through;
//   - it refuses and waits for the next pass when a forge lookup fails,
//     where a person gets a warning and an unticked checklist box;
//   - it re-fetches the distfiles from upstream before it pushes, and
//     holds the change if they no longer hash to what it records;
//   - it publishes at most a few changes a pass and never in a burst.
//
// AND ON THIS BUILD IT PUBLISHES NOTHING AT ALL. GateRing3 is the first
// thing the pass asks and the build-time constant behind it is false, so
// the slot below is exercised as a refusal. That is deliberate: the road
// and its inversions land now, tested, so that flipping one constant
// when the trust ladder's numbers are ruled on is a change to one line
// rather than to a design.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// DefaultPassPRCap is how many pull requests one unattended pass may
// open before it stops for the day.
//
// One. Not because one is the right number — the trust ladder will say
// what the right number is, per tier and per record of past merges — but
// because a cap that has never been ruled on must be the smallest number
// that still makes the road real. The cost of being wrong downward is
// that a change waits for the next pass; the cost of being wrong upward
// is a review queue with somebody's afternoon in it.
const DefaultPassPRCap = 1

// DefaultPublishPace is the minimum gap between two publications inside
// one pass.
//
// It exists in the same change as the cap and for the same reason: the
// two together are what make raising the cap safe, and a cap raised
// later beside a pacing that was never written is a burst. At the
// default cap of one it never fires, which is exactly what an
// unexercised safety belt looks like before the thing it belts is
// allowed to move.
//
// WHAT IT IS NOT IS A RATE. The slot is built fresh by each caller and
// keeps nothing across passes, so this constrains the second publication
// of one pass and constrains nothing about how often the pass runs: a
// cron entry every minute is paced exactly as one every ten. Saying so
// here rather than letting a reader infer a rate limit from the name,
// because the difference matters the day the cap is raised. Making it a
// real rate needs a last-publication moment that outlives the process —
// a ref, or a note of its own — and that is a durable-state decision
// with nobody's ruling behind it yet.
const DefaultPublishPace = 10 * time.Minute

// PassLimitError is the pass declining to publish more this time round:
// the cap is spent, or the pacing interval has not elapsed.
//
// It is in the pending band and not a refused one. Nothing is wrong with
// the change, nothing is wrong with the machine, and the remedy is to do
// nothing — the next pass picks it up. A caller that read this as a
// refusal would page somebody about a rate limit working.
type PassLimitError struct {
	Branch string
	// Why is the limit in words: which one, and what it is set to.
	Why string
}

func (e *PassLimitError) Error() string {
	return fmt.Sprintf("%s: not published this pass (%s); the next pass will take it up", e.Branch, e.Why)
}

// DockhandExit: the pending band — nobody's problem yet.
func (e *PassLimitError) DockhandExit() int { return exitcode.PromotionPending }

// Code names the outcome for a machine.
func (e *PassLimitError) Code() string { return "pass-limit" }

// ForgeLookupError is a forge question the pass could not get an answer
// to, on the road where an unanswered question must stop the work.
//
// The human road downgrades both of these lookups to warnings, and the
// comment there names its own condition: the box is left for the human
// to tick. There is no human. Worse than advisory, the own-PR lookup
// failing reads as "this branch has no pull request", which would make
// an unattended pass open a SECOND one against a queue that already has
// the first — the one cost this tool exists never to impose.
type ForgeLookupError struct {
	Branch string
	// What names the question — this branch's own pull request, the
	// same-port duplicate search — so a reader knows which answer is
	// missing.
	What string
	Err  error
}

func (e *ForgeLookupError) Error() string {
	return fmt.Sprintf("%s: could not ask the forge about %s (%v); an unattended publication will not guess, and the next pass will ask again",
		e.Branch, e.What, e.Err)
}

func (e *ForgeLookupError) Unwrap() error { return e.Err }

// DockhandExit: upstream's band — the forge answered badly or not at
// all, nothing local is wrong, and the same question may answer in an
// hour.
func (e *ForgeLookupError) DockhandExit() int { return exitcode.WitnessAPI }

// Code names the outcome for a machine.
func (e *ForgeLookupError) Code() string { return "forge-lookup-failed" }

// StealthHeldError is a change held because its distfiles no longer hash
// to what it records.
//
// The publication stops and a person is asked. It exits in the refused
// band because that is whose problem it is — the change is now a
// question about upstream, and no amount of retrying will answer it.
type StealthHeldError struct {
	Branch    string
	Mismatch  []ChecksumMismatch
	HoldFault error
}

func (e *StealthHeldError) Error() string {
	s := fmt.Sprintf("%s: upstream no longer serves the bytes this change records (%s) — held, with a %s finding for a person to answer",
		e.Branch, joinLines(mismatchLines(e.Mismatch)), StealthSuspected)
	if e.HoldFault != nil {
		s += fmt.Sprintf("; the hold could not be written (%v), so nothing but this refusal is stopping it", e.HoldFault)
	}
	return s
}

// DockhandExit: the refused band — a branch deliberately held back from
// publication, which is the code that word was reserved for.
func (e *StealthHeldError) DockhandExit() int { return exitcode.Held }

// Code names the refusal for a machine.
func (e *StealthHeldError) Code() string { return "stealth-suspected" }

// errNoPortToSearch is the duplicate check with nothing to search by. A
// person is told and carries on; the machine road cannot, because "I did
// not look" and "I looked and there was nothing" are the same silence to
// a caller that only sees a pull request appear.
var errNoPortToSearch = errors.New("neither the note nor the title names a port")

// mismatchLines renders the mismatches for a refusal's sentence.
func mismatchLines(m []ChecksumMismatch) []string {
	out := make([]string, 0, len(m))
	for _, one := range m {
		out = append(out, one.String())
	}
	return out
}

// PublishSlot is the machine's publish road, as the caller asks for it
// and as the pass answers.
//
// It is handed in by pointer and read back afterwards, because a pass
// has two kinds of thing to say about publication and they go to
// different places: what happened to each branch belongs under that
// branch in the report, and what the pass as a whole should exit with
// belongs to the caller. Results carries the second.
type PublishSlot struct {
	// MaxPRs caps the pull requests one pass may open. Zero takes
	// DefaultPassPRCap; a negative number publishes nothing, which is the
	// honest way for a caller to turn the road off without pretending it
	// is not there.
	MaxPRs int
	// Pace is the minimum gap between two publications in this pass. Zero
	// takes DefaultPublishPace. It is a within-pass stop and not a rate —
	// see DefaultPublishPace, which says what that costs.
	Pace time.Duration

	// Results is what the pass did, one entry per branch it considered,
	// in the order it considered them. Written by the pass.
	Results []PublishResult

	spent int
	last  time.Time
}

// PublishResult is one branch's outcome on the machine road.
type PublishResult struct {
	Branch string
	// Published says a pull request was opened or updated for it.
	Published bool
	// NoOp says there was nothing to do: the change is already out.
	NoOp bool
	// Err is the refusal or the pending answer. A nil Err with Published
	// false and NoOp false cannot happen and would be a bug in the pass.
	Err error
}

// Outcome is what the pass should exit with, and it deliberately reports
// only the waiting.
//
// A refusal is stated on the branch it is about and does not become the
// pass's status. On this build every candidate is refused with
// machine-publish-disabled, and a cron entry that exited non-zero every
// ten minutes because a road it was never asked to walk is closed would
// be a broken machine to everyone reading the log. What DOES deserve the
// exit is unfinished work: a verification still running, a forge that
// would not answer, a cap that stopped the pass short. Those say "ask
// again later", which is exactly what the pending band means.
func (s *PublishSlot) Outcome() error {
	for _, r := range s.Results {
		if r.Err == nil {
			continue
		}
		var coder exitcode.Coder
		if !errors.As(r.Err, &coder) {
			continue
		}
		if exitcode.Family(coder.DockhandExit()) == "pending" {
			return r.Err
		}
	}
	return nil
}

// cap and pace are the slot's own settings with their defaults applied.
func (s *PublishSlot) cap() int {
	if s.MaxPRs == 0 {
		return DefaultPassPRCap
	}
	return s.MaxPRs
}

func (s *PublishSlot) pace() time.Duration {
	if s.Pace == 0 {
		return DefaultPublishPace
	}
	return s.Pace
}

// admit asks the cap and the pacing whether this pass may publish one
// more thing, now.
//
// Both limits answer the same way — not this pass — because both mean
// the same thing to a reader: the change is fine and the machine is
// pacing itself.
func (s *PublishSlot) admit(branch string, now time.Time) error {
	if s.spent >= s.cap() {
		return &PassLimitError{Branch: branch,
			Why: fmt.Sprintf("this pass has already opened its %d pull request(s)", s.cap())}
	}
	if !s.last.IsZero() && now.Sub(s.last) < s.pace() {
		return &PassLimitError{Branch: branch,
			Why: fmt.Sprintf("the last publication was %s ago and the pacing interval is %s",
				now.Sub(s.last).Round(time.Second), s.pace())}
	}
	return nil
}

// took records a publication against the cap and the pacing.
func (s *PublishSlot) took(now time.Time) {
	s.spent++
	s.last = now
}

// publishPass is the reconciler's fourth phase.
//
// It runs after retire and before drain, and the ordering is an argument
// about what each phase may see. After retire, because a change whose
// pull request just merged is not a change to publish and its branch is
// about to be deleted. Before drain, because the standings this pass
// took are what it judges: the drain starts runs, so a slot that ran
// after it would be weighing records the same pass had just moved from
// queued to submitting, and would call "started ten seconds ago" a
// verification still pending — which it is, but the pass would then be
// waiting on itself.
//
// Nothing here prints. Each branch's outcome goes under that branch in
// the report, the way every other phase's prose does.
func (e *Engine) publishPass(ctx context.Context, repo *git.Repo, rep *render.Report, s *PublishSlot) {
	// The build gate, asked once for the pass and not once per branch. On
	// a build with the road disabled there is nothing branch-specific to
	// say, and saying the same sentence under every branch would bury the
	// report it is printed in.
	if err := GateRing3(record.Machine, e.MachinePublish); err != nil {
		s.Results = append(s.Results, PublishResult{Err: err})
		return
	}
	for i := range rep.Branches {
		b := &rep.Branches[i]
		if !slotCandidate(b) {
			continue
		}
		res := PublishResult{Branch: b.Branch}
		res.Published, res.NoOp, res.Err = e.publishOne(ctx, repo, b, s, rep.Now)
		if res.Err != nil {
			b.Prose = append(b.Prose, render.Line{Stream: render.ToErr,
				Text: res.Err.Error()})
		}
		s.Results = append(s.Results, res)
	}
}

// slotCandidate says whether the machine road has any business with this
// branch at all.
//
// The ask is the record's own Destination. A machine publishes what
// somebody asked to be published and nothing else: a change bound for a
// verdict was asked for an answer, not for a pull request, and a change
// bound for the branch was asked for neither. That is the same rule the
// drain already applies to starting a run, and for the same reason —
// acting past the ask is inventing it.
//
// A held change is not a candidate either. A hold is a person saying
// stop, and skipping is what stop means to an idempotent pass: not a
// refusal to report every ten minutes, just work the pass does not do.
func slotCandidate(b *render.BranchReport) bool {
	switch {
	case b.ObserveErr != "":
		// A standing that could not be read is not a standing to publish
		// on. The report already says so on the branch's own line.
		return false
	case b.Note == nil:
		return false
	case b.Note.Destination != record.ToPublished:
		return false
	case b.Note.Hold != nil:
		return false
	case b.Note.SupersededBy != "":
		return false
	}
	return true
}

// publishOne walks one candidate through the machine road's gates, in
// the order the gates have to be asked in.
func (e *Engine) publishOne(ctx context.Context, repo *git.Repo, b *render.BranchReport,
	s *PublishSlot, now time.Time) (published, noop bool, err error) {
	// The note, READ AGAIN, and not the one the report is carrying.
	//
	// That snapshot was taken during observation, before retire ran and
	// before any other phase did. A person running `dockhand hold` while a
	// pass walks the namespace is the ordinary way a change stops, and a
	// hold placed in that window is exactly the write this road must
	// honour — the pump already re-reads under its lock for the same
	// reason. Below this line are a distfile download from upstream and a
	// push, and hold.go's contract is that a held branch costs neither; on
	// the stale copy the fresh hold was not consulted until Promote, which
	// is after the fetch.
	//
	// One ledger read per CANDIDATE, which is the handful of branches
	// somebody asked to be published, not one per branch in the namespace.
	n, err := e.Ledger(repo).Read(ctx, b.Tip)
	if err != nil {
		return false, false, err
	}
	// A pull request state the pass could not read is not "no pull
	// request". Refusing here, before the phase is computed, is what stops
	// the unknown from being rendered as the zero fact — and it is the
	// difference between waiting a pass and opening a duplicate.
	if b.Retire.Err != "" {
		return false, false, &ForgeLookupError{Branch: b.Branch,
			What: "this branch's own pull request", Err: fmt.Errorf("%s", b.Retire.Err)}
	}
	phase, err := e.phaseOf(ctx, repo, b)
	if err != nil {
		return false, false, &ForgeLookupError{Branch: b.Branch,
			What: "this branch's own pull request", Err: err}
	}
	d := verdict.DecidePublish(verdict.PublishAsk{
		Record:     n,
		Promotable: n.Promotable(),
		Branch:     b.Branch,
		Tip:        git.Abbrev(b.Tip),
		By:         record.Machine,
		Phase:      phase,
	})
	if d.NoOp {
		return false, true, nil
	}
	if d.Refusal != nil {
		return false, false, d.Refusal
	}
	// The open-proposal gate after the evidence gate and not before it.
	// A change that failed its build and also carries an unanswered
	// question has two things wrong with it, and the build is the one a
	// reader needs first; a change that PASSED and carries a question is
	// exactly what this gate was reserved for, and it is the only shape
	// that reaches this line.
	if err := GateMachinePublish(n, b.Branch, record.Machine); err != nil {
		return false, false, err
	}
	// The hold, over the note this phase just re-read. Promote asks it too
	// and that ask is the authoritative one, but it is on the far side of
	// the re-witness: a change a person stopped must not cost a distfile
	// download to find that out.
	if err := GateHold(n, b.Branch, "the publication"); err != nil {
		return false, false, err
	}
	// The cap and the pacing, against a clock read HERE and not the pass's
	// own. rep.Now is read once so that two branches cannot disagree about
	// how long a run has been going; sharing it with the pacing would mean
	// every publication in one pass happened at the same instant, so after
	// the first the elapsed gap would be zero forever and a raised cap
	// would admit nothing.
	if err := s.admit(b.Branch, time.Now()); err != nil {
		return false, false, err
	}
	// The re-witness is the last thing before the push and after every
	// cheap refusal, because it is the only gate here that costs a
	// download. A change that was going to be refused for its evidence,
	// its findings or the pass's own cap must be refused without fetching
	// anything.
	if err := e.rewitnessBeforePush(ctx, repo, b, n, now); err != nil {
		return false, false, err
	}
	// The publication itself is promote's, with the invoker declared.
	// One publisher, so the machine road cannot drift from the human one
	// on what a pull request says, how a fork remote is found, or how a
	// re-publication converges on the pull request already open.
	if err := e.Promote(ctx, repo, b.Branch, PromoteOpts{
		Invoker: record.Machine,
		Closes:  n.ClosesTicket,
	}); err != nil {
		return false, false, err
	}
	s.took(time.Now())
	return true, false, nil
}

// phaseOf is how far this change has already travelled toward review,
// and it never reads that off a lookup which did not happen.
//
// The report answers the same question BY TRACKING CONFIG: retire()
// returns before any forge call when git knows no upstream for the
// branch, leaving the zero PRFact behind and Promoted false. On the
// report that is the right economy — the branch's line says "never
// promoted" and a person reads it. Here the zero fact would be read as
// PhaseInFlight, and absent collapsing into unknown is the one thing
// this road may not do. The cases are not hypothetical: a branch
// --replace just re-minted has no tracking config until a push restores
// it, a fresh clone of the tree has none for any branch, and `git branch
// --unset-upstream` removes it outright — every one of them a branch
// that may have an open pull request the pass would then open a second
// one beside.
//
// So where retire() actually asked, its answer stands, and where it did
// not, the slot asks itself — by the FORK OWNER, which is the derivation
// promote already trusts (D21) and the one that does not depend on local
// config at all. It costs one forge read per candidate, which is a
// change somebody asked to be published, not held and not superseded:
// the handful this pass was about to push anyway.
func (e *Engine) phaseOf(ctx context.Context, repo *git.Repo, b *render.BranchReport) (verdict.Phase, error) {
	if b.Retire.Promoted {
		return verdict.PhaseOf(b.Retire.PR), nil
	}
	upstream, err := gh.UpstreamRepo(ctx, repo)
	if err != nil {
		return "", err
	}
	_, owner, err := gh.ForkRemote(ctx, e.Gh, repo, "")
	if err != nil {
		return "", err
	}
	pr, found, err := gh.QueryPR(ctx, e.Gh, upstream, owner, b.Branch)
	if err != nil {
		return "", err
	}
	return verdict.PhaseOf(PRFact(pr, found)), nil
}

// rewitnessBeforePush re-fetches the change's distfiles from upstream
// and holds the change when they no longer hash to what it records.
//
// The hold is written before the refusal is returned, and the refusal
// says so if the write failed. A refusal alone stops THIS pass; the hold
// is what stops the next one, and a suspicion that had to be
// re-established every ten minutes would be re-fetched every ten
// minutes too.
func (e *Engine) rewitnessBeforePush(ctx context.Context, repo *git.Repo,
	b *render.BranchReport, n record.Record, now time.Time) error {
	mismatch, err := e.rewitness(ctx, repo, b.Tip, n)
	if err != nil {
		// The check did not run. That is not evidence of a stealth update
		// and it is not evidence of anything else; it is a gate that could
		// not be asked, so the publication does not happen.
		return &ForgeLookupError{Branch: b.Branch, What: "the distfiles upstream now serves", Err: err}
	}
	if len(mismatch) == 0 {
		return nil
	}
	holdErr := e.holdForStealth(ctx, repo, b.Tip, n.Ports(), mismatch, now)
	return &StealthHeldError{Branch: b.Branch, Mismatch: mismatch, HoldFault: holdErr}
}

// holdForStealth writes the hold and the proposed finding onto the
// change's record, in one update.
//
// One update because they are one fact: the hold says the change stops
// and the finding says why, and a note that carried either alone would
// be a stop with no reason or a reason nothing acted on. The clock read
// is the pass's own, handed in — record stamps nothing itself.
func (e *Engine) holdForStealth(ctx context.Context, repo *git.Repo, tip string,
	ports []string, mismatch []ChecksumMismatch, now time.Time) error {
	err := e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		changed := false
		if r.Hold == nil {
			r.Hold = &record.Hold{
				Reason: "checksums re-witnessed at publication time and differ",
				At:     now,
			}
			changed = true
		}
		asked := false
		for _, f := range r.Findings {
			if f.Kind == StealthSuspected {
				// Already asked. Appending a second copy every pass would
				// bury the answer to the first one under its own repetitions.
				asked = true
			}
		}
		if !asked {
			r.Findings = append(r.Findings, StealthFinding(ports, mismatch, now))
			changed = true
		}
		if !changed {
			return ledger.ErrUnchanged
		}
		return nil
	})
	if err != nil && !errors.Is(err, ledger.ErrUnchanged) {
		return err
	}
	return nil
}
