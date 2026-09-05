package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// Accepting a proposal: the human's answer to "these dependents need a
// revision bump", carried out as one more commit on the branch that
// already carries the change.
//
// One commit and not N. The members move for one reason and it is the
// same reason — a library they link moved — so a reviewer reads one
// message, checks one criterion, and sees N files under it. Splitting
// it into a commit per dependent would spend a reviewer's attention N
// times on the same claim.
//
// It never mints and it never verifies past what the branch already
// asked for. The proposal was measured on the headline's own
// verification, so the new tip inherits that evidence rather than
// pretending to have earned it, and the cohort's own verification is
// submitted afterwards because each member's rev+1 names an archive
// that does not exist — which is what makes the rebuild the evidence.

// NoProposalError is the cohort verb meeting a branch with nothing to
// accept.
//
// Three shapes reach it and it says which. A branch with no findings at
// all was never measured; a branch whose only proposal is an
// instruction comment has the maintainer's word and no measurement to
// act on, which is the two-step remedy — verify, then come back; and a
// branch whose proposal is already answered has been here before.
type NoProposalError struct {
	Branch string
	Why    string
	Remedy string
}

func (e *NoProposalError) Error() string {
	msg := fmt.Sprintf("%s: %s", e.Branch, e.Why)
	if e.Remedy != "" {
		msg += " — " + e.Remedy
	}
	return msg
}

// DockhandExit: the declined band. Nothing is broken and nothing was
// written; dockhand understood the request and has nothing to carry
// out.
func (e *NoProposalError) DockhandExit() int { return exitcode.PlanDeclined }

// Code names the refusal for a machine.
func (e *NoProposalError) Code() string { return "no-proposal" }

// CohortOpts is what the cohort verb takes beyond the branch.
type CohortOpts struct {
	// NoVerify accepts the proposal without submitting the cohort's own
	// verification. The rebuild IS the evidence — each member's rev+1
	// names an archive that does not exist, so it builds from source
	// against the new library — so this is the deliberate opt-out and
	// not the default.
	NoVerify bool
	// Test also runs each member's test suite in the environment, and
	// Trace stays attached to stream the build. Both mean exactly what
	// they mean on the single-port road — the cohort's verification is a
	// verification — and both are why they are carried here rather than
	// dropped: a flag a verb accepts and ignores is worse than one it
	// refuses.
	Test  bool
	Trace bool
	// Platform is the release to verify the cohort on; the zero value
	// takes the provider's default. Already parsed by the caller, as
	// everywhere else in the engine: flag parsing is the CLI's business.
	Platform platform.Release
	// Exclude names members to leave out of the change entirely — not
	// bumped, not built. It is the counterpart to the cap that used to
	// choose for the user: the proposal names every dependent, and this
	// is how a person takes some of them.
	//
	// Not the same as a withheld member, which IS bumped and only left
	// out of the guest. An excluded port keeps its old revision, which
	// means the tree still owes it one — so the commit body lists it
	// among the ports examined and not bumped, where a reviewer can
	// disagree.
	Exclude []string
}

// BuildCohort accepts a branch's revbump proposal: plan every member
// from the branch tip's own Portfiles, add them as one commit, and
// submit the cohort's verification.
//
// The members are planned from the TIP and never from the working tree.
// That is the rule the whole verb turns on: the working tree is the
// user's, it may carry a half-finished edit or another branch entirely,
// and a plan computed over it would pledge a precondition hash for a
// file the commit will not contain.
//
// A member the planner cannot plan is declined by name and the cohort
// proceeds with the rest. Refusing the whole cohort over one Portfile
// whose shape does not say where a revision line belongs would leave
// every other dependent broken for the sake of consistency; naming the
// k that were left, with the remedy each of them needs, leaves a person
// something to finish by hand.
func (e *Engine) BuildCohort(ctx context.Context, repo *git.Repo, target string, o CohortOpts) error {
	branch, err := e.Resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	n, err := e.Ledger(repo).Read(ctx, tip)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return &NoProposalError{Branch: branch,
				Why:    "no verification record on the tip, so nothing has been proposed",
				Remedy: "`dockhand verify " + branch + "` measures it first"}
		}
		return err
	}
	proposal, ok := cohortProposal(n)
	if !ok {
		return noProposal(branch, n)
	}
	proposal, err = excludeMembers(proposal, o.Exclude)
	if err != nil {
		return err
	}
	for _, c := range proposal.Candidates {
		if c.Reason == excludedReason {
			fmt.Fprintf(e.Err, "%s: excluded, not bumped\n", c.Port)
		}
	}
	head := n.Headline()
	built, declined, err := e.planCohort(ctx, repo, tip, cohortPlanner(head, proposal), proposal)
	if err != nil {
		return err
	}
	if len(built) == 0 {
		return &NoProposalError{Branch: branch,
			Why:    fmt.Sprintf("every proposed member declined: %s", declinedNames(declined)),
			Remedy: "revbump them by hand; `dockhand dismiss " + branch + "` records that the proposal was answered"}
	}
	for _, d := range declined {
		fmt.Fprintf(e.Err, "%s: not bumped — %s\n", d.Port, d.Reason)
	}

	commit := git.Commit{
		Files:   cohortFiles(built),
		Message: render.CohortMessage(cohortCommit(n, head, proposal, built, declined)),
	}
	newTip, err := e.Extend(ctx, repo, ExtendRequest{
		Branch: branch, ExpectedTip: tip, Commit: commit,
		Subjects: cohortSubjects(proposal, built),
	})
	if err != nil {
		return err
	}
	// The answer is recorded after the commit and not before it. A
	// disposition written first would say a proposal had been accepted
	// by a commit that then failed to land; written here, a failure
	// leaves the finding proposed on a tip that carries the cohort,
	// which the machine gate holds and a person can see.
	e.acceptProposal(ctx, repo, newTip, branch, declined)
	fmt.Fprintln(e.Out, newTip)
	if o.NoVerify {
		return nil
	}
	apart := solo(proposal)
	// Handed to the submission rather than recorded here: the release is
	// resolved inside it, and a run keyed before that is keyed under no
	// release at all.
	var held []WithheldMember
	for _, c := range proposal.Candidates {
		if c.Solo && c.Proposed {
			held = append(held, WithheldMember{Port: c.Port, Why: withheldDetail(c, head.Port)})
		}
	}
	return e.submit(ctx, &Minted{Repo: repo, Branch: branch, Sha: newTip, RelPort: head.Portdir},
		submission{Port: head.Port, Release: o.Platform, Test: o.Test, Trace: o.Trace,
			Members: cohortRoster(head, built, apart), Withheld: held})
}

// cohortProposal is the finding the verb answers: the one proposal a
// measurement produced, still unanswered.
func cohortProposal(n record.Record) (record.Finding, bool) {
	for _, f := range n.Findings {
		if f.Kind == render.KindCohort && f.Disposition == record.Proposed {
			return f, true
		}
	}
	return record.Finding{}, false
}

// noProposal words the three ways there is nothing to accept.
func noProposal(branch string, n record.Record) error {
	for _, f := range n.Findings {
		if f.Kind == render.KindCohort {
			return &NoProposalError{Branch: branch,
				Why:    "the revbump proposal has already been answered (" + string(f.Disposition) + ")",
				Remedy: "`dockhand status` shows what the branch carries"}
		}
	}
	for _, f := range n.Findings {
		if f.Kind == render.KindInstruction && f.Disposition == record.Proposed {
			// The maintainer's own instruction. Building a cohort on the
			// comment alone would be the blanket revbump this tool must
			// never make, so what this refusal owes the reader is the state
			// of the measurement — and it must READ that state rather than
			// assume it.
			//
			// Assuming it was the bug: the sentence said nothing had
			// measured whether anything moved, three lines under a note
			// carrying an abi-unchanged finding with its own criterion, and
			// sent the reader back to `verify` — which re-measures and
			// reaches the same answer, forever, while the machine gate
			// keeps holding. A refusal that guesses about its own evidence
			// is the one thing this step is not allowed to do.
			return instructionRefusal(branch, n, f)
		}
	}
	return &NoProposalError{Branch: branch,
		Why:    "no revbump proposal on this branch",
		Remedy: "`dockhand status` shows what it carries"}
}

// instructionRefusal words the comment-only case against what the note
// actually holds.
//
// Three states and three sentences. No ABI finding: nothing has
// measured, and verify is the step that would — the two-step remedy the
// step was written for. A measurement that found nothing, or one that
// could not be made: the measurement is quoted, because it is the
// evidence a person weighs the comment against, and the only two
// honest answers left are the hand and the dismissal. In neither of
// those does `verify` appear: running it again produces the same
// sentence, and offering it would be a loop.
func instructionRefusal(branch string, n record.Record, f record.Finding) error {
	abi, ok := abiFinding(n)
	if !ok {
		return &NoProposalError{Branch: branch,
			Why: "no ABI criterion yet: the comment in " + f.Source +
				" asks for revision bumps and nothing has measured whether anything moved",
			Remedy: "`dockhand verify " + branch + "` measures it, then run this again; `dockhand dismiss " +
				branch + "` records that you looked and said no"}
	}
	what := "the measurement found nothing to bump on"
	if abi.Kind == render.KindABIUnavailable {
		what = "the measurement could not be made"
	}
	return &NoProposalError{Branch: branch,
		Why: "the comment in " + f.Source + " asks for revision bumps and " + what +
			": " + abi.Criterion,
		Remedy: "revbump by hand what you judge the comment covers; `dockhand dismiss " +
			branch + "` records that you looked and said no"}
}

// abiFinding is the ABI finding a record carries, and whether it
// carries one at all.
func abiFinding(n record.Record) (record.Finding, bool) {
	for _, f := range n.Findings {
		switch f.Kind {
		case render.KindABIChanged, render.KindABIUnchanged, render.KindABIUnavailable:
			return f, true
		}
	}
	return record.Finding{}, false
}

// cohortPlanner builds the member planner from the proposal's own
// words: the headline it is about, what that moved to, and the
// criterion, verbatim.
func cohortPlanner(head record.Subject, f record.Finding) intent.CohortPlanner {
	return bumprevision.Cohort{Headline: head.Port, Target: head.Target, Reason: f.Criterion}
}

// planned is one member's plan with the ports it covers.
//
// The plan is per PORTDIR and the ports are per subject: judy's seven
// php*-Judy dependents share php/php-Judy, so one revision line moves
// all seven and one plan answers for all seven. Keeping both is what
// lets the commit carry one file and the record carry seven subjects.
type planned struct {
	Portdir string
	Ports   []string
	Plan    *plan.Plan
	Content []byte
}

// declinedMember is a member the planner would not plan.
type declinedMember struct {
	Port    string
	Portdir string
	Reason  string
}

// memberDecline is a refusal whose sentence is published and whose
// cause is not.
//
// Every other error in this file is fine to print: a planner's decline
// is already written for a person. A plumbing failure is not, and this
// one ends up in a pull request body — so the message is the repo's own
// voice and the wrapped cause stays reachable for errors.Is and for a
// reader unwrapping it, without being pasted under a reviewer's nose.
type memberDecline struct {
	why   string
	cause error
}

func (e *memberDecline) Error() string { return e.why }
func (e *memberDecline) Unwrap() error { return e.cause }

// planCohort plans every proposed member, in the proposal's own order,
// from the Portfile the branch tip holds.
//
// Serially, over one short-lived evaluator. The cap is eight members
// and each one is a shadow evaluation of a second or so, so a pool
// would buy a handful of seconds and cost the concurrency question the
// reverse index has already had to answer once — and the answer to
// "which member failed" is worth more here than the seconds.
func (e *Engine) planCohort(ctx context.Context, repo *git.Repo, tip string, planner intent.CohortPlanner, f record.Finding) ([]planned, []declinedMember, error) {
	ev, err := e.Session(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer ev.Close()
	root, err := e.Temp()
	if err != nil {
		return nil, nil, err
	}

	var built []planned
	var declined []declinedMember
	at := map[string]int{}
	refused := map[string]string{}
	for _, c := range f.Candidates {
		if !c.Proposed {
			continue
		}
		if i, seen := at[c.Portdir]; seen {
			// A second subject in a directory already planned. One
			// revision line moves them together, so it is one plan and
			// this member rides it.
			built[i].Ports = append(built[i].Ports, c.Port)
			continue
		}
		if why, seen := refused[c.Portdir]; seen {
			// And symmetrically: a directory that already declined
			// declines for every subport in it. Each member is still named
			// — a person needs the list of what to do by hand — but the
			// evaluation is paid for once, which for php/php-Judy's seven
			// subports is seven Tcl round trips saved on one answer.
			declined = append(declined, declinedMember{Port: c.Port, Portdir: c.Portdir, Reason: why})
			continue
		}
		p, content, derr := e.planMember(ctx, repo, tip, root, ev, planner, c)
		if derr != nil {
			refused[c.Portdir] = derr.Error()
			declined = append(declined, declinedMember{Port: c.Port, Portdir: c.Portdir, Reason: derr.Error()})
			continue
		}
		at[c.Portdir] = len(built)
		built = append(built, planned{Portdir: c.Portdir, Ports: []string{c.Port}, Plan: p, Content: content})
	}
	return built, declined, nil
}

// planMember plans one portdir and returns the plan with the bytes it
// produces.
//
// The Portfile comes from the tip through git and the rest of the
// portdir from the checkout: a shadow needs the whole directory —
// patchfiles, a files/ subtree — and only the Portfile is being
// changed. A portdir the checkout does not carry declines by name
// rather than being planned against a directory that is not there.
func (e *Engine) planMember(ctx context.Context, repo *git.Repo, tip string, root tempdir.Root, ev port.Oracle, planner intent.CohortPlanner, c record.Candidate) (*plan.Plan, []byte, error) {
	if c.Portdir == "" {
		return nil, nil, errors.New("the proposal names no portdir for it, so there is nothing to edit")
	}
	src, err := repo.BlobAt(ctx, tip, c.Portdir+"/"+macports.PortfileName)
	if err != nil {
		// Worded here rather than passed through. This string is written
		// verbatim into the candidate's reason and published under
		// "Examined and not bumped", so git's own sentence — a sha, a
		// `git cat-file: fatal:`, a quoted path — would be the one row of
		// a pull request body that is a plumbing transcript. The cause
		// still travels, for errors.Is and for anyone unwrapping; it just
		// does not travel to the reviewer.
		return nil, nil, &memberDecline{cause: err,
			why: "the branch tip carries no " + c.Portdir + "/" + macports.PortfileName +
				" — the index names a port this checkout does not have"}
	}
	dir := filepath.Join(repo.Root, filepath.FromSlash(c.Portdir))
	h := port.New(tree.Target{Portdir: dir}, ev).WithTempDir(root)
	p, err := planner.PlanMember(ctx, h, src, c.Portdir)
	if err != nil {
		return nil, nil, err
	}
	content, err := p.Materialize(src)
	if err != nil {
		return nil, nil, err
	}
	return p, content, nil
}

// cohortFiles is the commit's file list: one edited Portfile per
// planned portdir, in the order the members must be built, each
// followed by whatever whole files its plan rewrites beside it — the
// same list planOnBase builds for a single plan. No cohort planner
// produces one today, a revbump fetching nothing; the carry is here so
// that the day one does, the commit holds what the plan says and not
// half of it.
func cohortFiles(built []planned) []git.File {
	out := make([]git.File, 0, len(built))
	for _, b := range built {
		out = append(out, git.File{Path: b.Portdir + "/" + macports.PortfileName, Content: b.Content})
		for _, f := range b.Plan.Files {
			out = append(out, git.File{Path: b.Portdir + "/" + f.Path, Content: []byte(f.Content)})
		}
	}
	return out
}

// cohortSubjects are the members the new commit adds to the change, in
// the order they must be built.
//
// One per PORT and not per plan, because a subject is what gets built
// and a subport is its own build: php/php-Judy is one edit and seven
// installs. The reason each one carries is the criterion, so a reader
// of the note meets the same sentence the commit body states.
func cohortSubjects(f record.Finding, built []planned) []record.Subject {
	out := make([]record.Subject, 0, len(built))
	for _, b := range built {
		for _, p := range b.Ports {
			out = append(out, record.Subject{
				Port:    p,
				Names:   []string{p},
				Portdir: b.Portdir,
				Intent:  "bump-revision",
				Target:  revisionTarget(b.Plan),
				Reason:  f.Criterion,
			})
		}
	}
	return out
}

// revisionTarget is what a member moved to, read off the plan's own
// slug — "netdata-rev3" is revision 3 — rather than re-derived. Empty
// where the slug does not carry one, because a wrong target in a record
// is worse than an absent one.
func revisionTarget(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	if _, rest, found := strings.Cut(p.Slug, "-rev"); found {
		return "rev" + rest
	}
	return ""
}

// cohortRoster is the submission's roster: the headline first, then
// every planned member in build order.
//
// The headline rides along because a dependent is built against the new
// library and the guest's own tree does not have it — the whole cohort
// is one build, in one environment, in dependency order.
// solo names the members the cohort bumps but does not build, because a
// member it does build declares a conflict with them.
func solo(f record.Finding) map[string]bool {
	out := map[string]bool{}
	for _, c := range f.Candidates {
		if c.Solo {
			out[strings.ToLower(c.Port)] = true
		}
	}
	return out
}

// cohortRoster is what the guest is asked to build, which is not the
// same list as what the commit edits.
//
// A member that conflicts with one already in the roster is bumped by
// the commit and left out here: MacPorts will not activate both, so
// staging the pair spends a guest proving the second cannot install and
// stops every member behind it. It is owed nothing further (maintainer's
// ruling, 2026-09-04): the person is told it was withheld, on stderr
// and in the body, and that is the whole of what the tool does about
// it. Building it anyway — deactivating the sibling first — is an
// option a person may be given, recorded in docs/todo.md, not a debt
// the tool carries.
func cohortRoster(head record.Subject, built []planned, apart map[string]bool) []Member {
	out := []Member{{Port: head.Port, Portdir: head.Portdir}}
	for _, b := range built {
		for _, p := range b.Ports {
			if apart[strings.ToLower(p)] {
				continue
			}
			out = append(out, Member{Port: p, Portdir: b.Portdir})
		}
	}
	return out
}

// cohortCommit gathers what the commit message says from the note and
// the plans.
func cohortCommit(n record.Record, head record.Subject, f record.Finding, built []planned, declined []declinedMember) render.CohortCommit {
	c := render.CohortCommit{
		Port: head.Port, Target: head.Target,
		Criterion: f.Criterion,
		Limits:    abiLimits(n),
		Listed:    f.Candidates,
	}
	for _, q := range Instructions(n) {
		c.Quotes = append(c.Quotes, render.CohortQuote{Source: q.Source, Quote: q.Quote})
	}
	for _, b := range built {
		for _, p := range b.Ports {
			c.Members = append(c.Members, render.CohortMember{
				Port: p, Portdir: b.Portdir, Reason: reasonFor(f, p)})
		}
	}
	for _, d := range declined {
		c.Declined = append(c.Declined, render.CohortDecline{Port: d.Port, Portdir: d.Portdir, Reason: d.Reason})
	}
	return c
}

// reasonFor is a candidate's own reason, looked up by port.
func reasonFor(f record.Finding, port string) string {
	for _, c := range f.Candidates {
		if c.Port == port {
			return c.Reason
		}
	}
	return ""
}

// acceptProposal records the human's answer on the new tip: the
// proposal accepted, the instruction comments it also answers accepted
// with it, and the members that declined marked as what actually
// happened to them.
//
// The candidates are amended rather than left as the proposal wrote
// them, because the note is what the pull request body vouches for: a
// member that was proposed and then declined by its own Portfile's
// shape is a port a person still has to bump, and a row that still read
// "proposed" would put it in the body as revbumped.
//
// A failure is a warning and not the verb's answer. The commit is
// written and the cohort is on the branch; reporting that as a failure
// because a disposition could not be updated would be the more
// misleading of the two, and what it costs is that the machine gate
// keeps holding — which is the safe direction.
func (e *Engine) acceptProposal(ctx context.Context, repo *git.Repo, tip, branch string, declined []declinedMember) {
	why := map[string]string{}
	for _, d := range declined {
		why[d.Port] = d.Reason
	}
	if err := e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		changed := false
		for i := range r.Findings {
			f := &r.Findings[i]
			if f.Disposition != record.Proposed {
				continue
			}
			switch f.Kind {
			case render.KindCohort:
				for j := range f.Candidates {
					c := &f.Candidates[j]
					reason, was := why[c.Port]
					if !c.Proposed || !was {
						continue
					}
					c.Proposed = false
					c.Reason = "proposed, then declined: " + reason + " — do this one by hand"
				}
				f.Disposition = record.Accepted
				changed = true
			case render.KindInstruction:
				// The comment asked for revision bumps and this commit made
				// the ones the measurement supports. It is answered by the
				// same act, and leaving it proposed would hold publication
				// for a question a person has already answered.
				f.Disposition = record.Accepted
				changed = true
			}
		}
		if !changed {
			return ledger.ErrUnchanged
		}
		return nil
	}); err != nil && !errors.Is(err, ledger.ErrUnchanged) {
		fmt.Fprintf(e.Err, "warning: recording the accepted proposal on %s: %v\n", branch, err)
	}
}

// Dismiss records that a person looked at a branch's proposals and said
// no.
//
// Dismissal is an answer and not an absence, which is why it is written
// down: a finding that vanished when declined would be proposed again
// on the next pass, and the measurement behind it does not change
// because somebody disagreed with what to do about it. The pull request
// body says so too — a dismissed cohort is a decision a reviewer is
// entitled to see.
func (e *Engine) Dismiss(ctx context.Context, repo *git.Repo, target string) error {
	branch, err := e.Resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	var dismissed []string
	err = e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		for i := range r.Findings {
			if r.Findings[i].Disposition != record.Proposed {
				continue
			}
			r.Findings[i].Disposition = record.Dismissed
			dismissed = append(dismissed, r.Findings[i].Kind)
		}
		if len(dismissed) == 0 {
			return ledger.ErrUnchanged
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(dismissed) == 0 {
		// The update left the note alone, which Update reports as success
		// — it is the ordinary answer for a closure with nothing to
		// change. What was asked for was an answer to a question, and
		// there is none to answer.
		return &NoProposalError{Branch: branch,
			Why:    "nothing is proposed on this branch",
			Remedy: "`dockhand status` shows what it carries"}
	}
	fmt.Fprintf(e.Out, "dismissed on %s: %s\n", branch, strings.Join(dismissed, ", "))
	fmt.Fprintln(e.Err, "the measurement stands on the note; only the answer to it changed")
	return nil
}

// declinedNames lists the members that would not plan, for the refusal
// that says every one of them did.
func declinedNames(declined []declinedMember) string {
	names := make([]string, 0, len(declined))
	for _, d := range declined {
		names = append(names, d.Port)
	}
	return strings.Join(names, ", ")
}

// abiLimits is the caveat the measurement travels with, read off the
// finding that made it.
//
// It is the judgment's own constant and not a sentence written here: a
// commit body that reworded what the criterion cannot see would be a
// second version of the one claim this whole step exists to keep
// singular. A record with no ABI finding — a cohort accepted on
// something else — carries no caveat rather than an invented one.
func abiLimits(n record.Record) string {
	for _, f := range n.Findings {
		if f.Kind == render.KindABIChanged || f.Kind == render.KindABIUnchanged {
			return verdict.ABILimits
		}
	}
	return ""
}

// withheldDetail words why a member was bumped and not built.
//
// It names the sibling rather than the rule, because the sibling is
// what a reader can check: `port info --conflicts` answers it, and "the
// cohort's co-residency rule" answers nothing.
func withheldDetail(c record.Candidate, head string) string {
	with := conflictNamedIn(c.Reason)
	if with == "" {
		return "it cannot share a guest with another member of this cohort"
	}
	return "it conflicts with " + with + ", which this cohort builds"
}

// conflictNamedIn lifts the sibling's name out of the candidate reason
// the proposal wrote, so the two sentences cannot drift apart.
func conflictNamedIn(reason string) string {
	const marker = "conflicts with "
	i := strings.Index(reason, marker)
	if i < 0 {
		return ""
	}
	rest := reason[i+len(marker):]
	if j := strings.Index(rest, ","); j >= 0 {
		return rest[:j]
	}
	return rest
}

// excludedReason is the candidate reason an excluded member carries. It
// is matched as well as written, so it is one constant.
const excludedReason = "excluded by --exclude: not bumped by this change"

// excludeMembers takes the named ports out of a proposal, returning the
// finding as the commit should record it.
//
// A name that matches no proposed member is an error and not a shrug. A
// person excluding "imagemagick" from a proposal holding "ImageMagick"
// means to exclude it, and a verb that silently bumped it anyway would
// have taken an instruction and done the opposite while reporting
// success.
func excludeMembers(f record.Finding, exclude []string) (record.Finding, error) {
	if len(exclude) == 0 {
		return f, nil
	}
	want := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		if name = strings.TrimSpace(name); name != "" {
			want[strings.ToLower(name)] = true
		}
	}
	out := make([]record.Candidate, 0, len(f.Candidates))
	hit := map[string]bool{}
	kept := 0
	for _, c := range f.Candidates {
		if c.Proposed && want[strings.ToLower(c.Port)] {
			hit[strings.ToLower(c.Port)] = true
			c.Proposed, c.Solo, c.Reason = false, false, excludedReason
			out = append(out, c)
			continue
		}
		if c.Proposed {
			kept++
		}
		out = append(out, c)
	}
	var unknown []string
	for name := range want {
		if !hit[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return f, &UnknownMemberError{Names: unknown, Members: proposedNames(f)}
	}
	if kept == 0 {
		return f, &EmptyCohortError{}
	}
	f.Candidates = out
	return f, nil
}

// proposedNames lists what the proposal put forward, for an error that
// has to say what the user could have named instead.
func proposedNames(f record.Finding) []string {
	var out []string
	for _, c := range f.Candidates {
		if c.Proposed {
			out = append(out, c.Port)
		}
	}
	sort.Strings(out)
	return out
}

// UnknownMemberError reports --exclude naming a port the proposal does
// not put forward.
type UnknownMemberError struct {
	Names   []string
	Members []string
}

func (e *UnknownMemberError) Error() string {
	return fmt.Sprintf("--exclude names %s, which this proposal does not put forward; its members are %s",
		strings.Join(e.Names, ", "), strings.Join(e.Members, ", "))
}

// DockhandExit: the declined band. The request was understood and
// nothing was written.
func (e *UnknownMemberError) DockhandExit() int { return exitcode.PlanDeclined }

// Code names the refusal for a machine.
func (e *UnknownMemberError) Code() string { return "unknown-member" }

// EmptyCohortError reports --exclude leaving the cohort with nothing to
// bump.
type EmptyCohortError struct{}

func (e *EmptyCohortError) Error() string {
	return "--exclude leaves no member to bump; `dockhand dismiss` is how a proposal is declined outright"
}

// DockhandExit: the declined band.
func (e *EmptyCohortError) DockhandExit() int { return exitcode.PlanDeclined }

// Code names the refusal for a machine.
func (e *EmptyCohortError) Code() string { return "empty-cohort" }
