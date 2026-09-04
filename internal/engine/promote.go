package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// The two ways a promotion ends half done. Both leave the fork branch
// published and the pull request not, and that is the whole reason
// they are types: re-running a promote is not free — it force-pushes,
// it retitles, it spends a review notification — so a script has to be
// able to tell "nothing happened" from "the branch is up, finish the
// PR by hand". They carry the branch and the remote for the same
// reason: a caller told only that something failed cannot say where
// the work is standing.
//
// The words are unchanged from when these were plain errors; only
// their identity is new.
type PushedPRError struct {
	Branch string
	Remote string
	Err    error
}

func (e *PushedPRError) Error() string {
	return fmt.Sprintf("the branch is pushed; opening the PR failed: %v", e.Err)
}

func (e *PushedPRError) Unwrap() error { return e.Err }

// DockhandExit: the partial band — half the work stands.
func (e *PushedPRError) DockhandExit() int { return exitcode.PushedPRFailed }

// Code names the outcome for a machine.
func (e *PushedPRError) Code() string { return "pushed-pr-failed" }

// MachineRepublishError is an unattended publication meeting a branch
// whose pull request is already open.
//
// The slot decides this a phase earlier and calls it work already done,
// which is the right answer there: an idempotent pass must treat a
// change in front of reviewers as finished rather than as an error. What
// this type is for is the case where that phase gate did NOT decide it —
// a machine that reached the verb anyway, which is by construction a bug
// above the verb. The funnel that spends ring 3 says so instead of
// force-updating a review it did not open and printing a note about it.
//
// It is in the refused band at the machine gate, which is the definition
// of the code: a person typing promote over the same branch is
// re-promoting on purpose and is allowed it.
type MachineRepublishError struct {
	Branch string
	Number int
	URL    string
}

func (e *MachineRepublishError) Error() string {
	return fmt.Sprintf("%s: PR #%d is already open for this branch (%s) — an unattended publication does not update a review it did not open; `dockhand promote %s` refreshes it on a person's authority",
		e.Branch, e.Number, e.URL, e.Branch)
}

// DockhandExit: the refused band's machine gate — the road refused it,
// and a person asking for the same thing would be allowed it.
func (e *MachineRepublishError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *MachineRepublishError) Code() string { return "machine-republish" }

// PRRefreshError is the same partial publication met by a branch whose
// pull request already existed: the push landed and the PR still
// describes the change it used to carry.
type PRRefreshError struct {
	Branch string
	Remote string
	Number int
	Err    error
}

func (e *PRRefreshError) Error() string {
	return fmt.Sprintf("the branch is pushed; refreshing PR #%d failed: %v", e.Number, e.Err)
}

func (e *PRRefreshError) Unwrap() error { return e.Err }

// DockhandExit: the partial band — the branch moved, its description did
// not.
func (e *PRRefreshError) DockhandExit() int { return exitcode.PRRefreshFailed }

// Code names the outcome for a machine.
func (e *PRRefreshError) Code() string { return "pr-refresh-failed" }

// PromoteOpts is everything a promotion takes besides the branch:
// where it pushes, what the pull request says, and which of promote's
// refusals the caller has already answered.
type PromoteOpts struct {
	// Remote is the fork remote to push to; empty detects it by gh
	// login.
	Remote string
	// Title overrides the minted commit's subject.
	Title string
	// Closes is the Trac ticket the pull request closes.
	Closes string

	// NoPR pushes to the fork and stops there.
	NoPR bool
	// NoVerify publishes past a FAILED verification — the one refusal
	// that stands on evidence rather than on absence.
	NoVerify bool
	// NoPRCheck skips the search for a pre-existing open PR on the same
	// port.
	NoPRCheck bool
	// Force replaces the fork branch and refreshes the open PR.
	Force bool
	// Invoker is who asked for this publication. The zero value is
	// record.Human, which is every caller today: promote is a person
	// typing a verb.
	//
	// It is a parameter and never inferred from the call site, because
	// one gate turns on it. A change still carrying an unanswered
	// proposal publishes for a person — they are looking at the
	// advisory, and promoting anyway is their answer — and is refused
	// for the machine, which has nobody to have read it. A field that
	// could be widened by accident at a new call site is the one way
	// that refusal stops meaning anything.
	Invoker record.Driver
}

// invoker is who this promotion is attributed to, with the zero value
// read as the caller every verb has today.
func (o PromoteOpts) invoker() record.Driver {
	if o.Invoker == "" {
		return record.Human
	}
	return o.Invoker
}

// noVerify, noPRCheck and force are the three overrides as THIS invoker
// may spend them, which is to say as a person and never as a machine.
//
// The first two are a person's judgment written down: "I have looked at
// the failure and I am publishing anyway", "I know about the other pull
// request". An unattended pass has no judgment to write down, so on that
// road the flags are not refused — they are simply not honoured, which
// is the reading that cannot be argued with by a caller that set them by
// accident. --no-verify unreachable from the machine road is the whole
// of the positive-evidence rule; the duplicate check unreachable is what
// keeps a rate-limited pass from opening a second pull request beside
// somebody's first.
//
// force is narrowed the same way and for a stronger reason than either.
// It selects a with-lease force-push and a retitle of an open pull
// request — the most damaging thing on this road — and no caller sets it
// on the machine side today. Reading it through a method rather than raw
// is what keeps that a property of the TYPE instead of a property of
// every call site remembering: a future caller filling in a struct field
// cannot hand the unattended road a force-push over somebody's review.
func (o PromoteOpts) noVerify() bool  { return o.NoVerify && o.invoker() == record.Human }
func (o PromoteOpts) noPRCheck() bool { return o.NoPRCheck && o.invoker() == record.Human }
func (o PromoteOpts) force() bool     { return o.Force && o.invoker() == record.Human }

// Promote publishes a verified branch: push it to the user's fork under
// its own name, then open the pull request against the upstream
// repository. The PR is ring 3 — other people's attention — and this is
// the only thing that spends it (cli.md); everything before the PR is
// the user's own fork, deletable at will.
//
// Nothing about the pull request is stored: the branch→PR link is
// derived (D21) — the push writes ordinary tracking config, and any
// later lookup queries pulls by head ref, the same way gh itself does.
// That derivation is also what makes a second promotion of one branch
// converge on the pull request the first opened instead of opening a
// second one.
//
// What IS recorded is the publication itself, as an audit row: the act
// is the thing history wants, and it cannot be reconstructed later from
// a pull request that has since been edited, merged or closed.
func (e *Engine) Promote(ctx context.Context, repo *git.Repo, target string, o PromoteOpts) error {
	branch, err := e.Resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	// The build-time gate, first and before any side effect. A machine
	// that reached this verb reads no note, cancels nothing and spends no
	// gh call: it is told what it is on this build and stops. Ordering it
	// ahead of the verdict gate is what makes the refusal a machine gets
	// here machine-publish-disabled rather than a complaint about its
	// evidence, so the suite pins the refusal that is actually operating.
	if err := GateRing3(o.invoker(), e.MachinePublish); err != nil {
		return err
	}

	// Promote refuses an unverified tip: the PR spends reviewer
	// attention, and the private backends exist to predict the shared
	// one's verdict before that happens. Without a local verify
	// provider there is nothing to refuse toward — the machine cannot
	// verify — so the promotion proceeds unverified, says so, and the
	// PR body says so too, which is the candour reviewers accept.
	// A promote issued mid-verification is itself the user's answer
	// about the running build: cancel it with a warning and proceed —
	// the tool removes friction, the note records the cancellation,
	// and the PR reads as whatever evidence remains. The body names
	// that cancellation as the reason it carries no verdict, which is
	// a change from the rule this comment used to state: the PR said
	// only verified or not, and "not" was one fixed sentence blaming a
	// missing verification environment on a machine that had one and
	// had just been told not to wait for it. What stays local is the
	// prose — a build log's words, a worker's name — and never the
	// cause.
	//
	// A MACHINE NEVER CANCELS. Every sentence of the paragraph above is
	// about a person's intent — what promoting mid-verification says
	// about the running build — and an unattended pass has no intent to
	// read. Left ungated, one pass would kill every verification in
	// progress on the machine and then publish the unverified result it
	// had just created. The one cancellation a machine may cause is a
	// supersede, which happens at mint and is about the branch rather
	// than about anybody's patience.
	// The evidence is read BEFORE anything is cancelled (maintainer's
	// ruling, 2026-09-04). Cancelling first would write canceled runs
	// into the record this very gate is about to judge, and the gate
	// would then be judging a state the promotion itself produced. The
	// cancellations still happen — a person promoting without waiting
	// means it — but they happen to builds the verdict has already been
	// taken without.
	n, verified, err := e.Ledger(repo).EvidenceFor(ctx, tip)
	if err != nil {
		return err
	}
	if o.invoker() == record.Human {
		freed, err := e.cancelRuns(ctx, repo, tip, "canceled: promoted without waiting", false)
		if err != nil && !errors.Is(err, git.ErrNoNote) {
			// A tip with no note is the ordinary unverified shape and holds
			// nothing to cancel; anything else is a ledger this promotion
			// cannot reason about.
			return err
		}
		if len(freed) > 0 {
			fmt.Fprintf(e.Err, "canceled %d running verification(s) — promoting without waiting\n", len(freed))
		}
	}
	// The gate itself is a judgment about the verdict set, so it is made
	// where the other judgments are; what is left here is saying so.
	//
	// The ask says Human outright rather than passing this promotion's
	// invoker through. promote IS the human road — the command layer
	// refuses it in auto mode with promote-is-human, and GateRing3 above
	// has already stopped any machine that got past that — so the
	// evidence rules here are a person's by construction. Handing the
	// machine's own rules a phase this verb never looks up would make an
	// unset field decide the verb's behaviour, and the machine's rules
	// belong to the road that has the facts they need: the reconciler's
	// slot, which builds its ask with the pull request it just read.
	gate := verdict.DecidePublish(verdict.PublishAsk{
		Record: n, Promotable: verified, Branch: branch,
		Tip: git.Abbrev(tip), NoVerify: o.noVerify(), By: record.Human,
	})
	if gate.Refusal != nil {
		return gate.Refusal
	}
	// The hold, after the evidence gate and before every network call.
	//
	// After the evidence gate, because a change that failed its build and
	// was then held has two things wrong with it and the build is the one
	// a reader needs first — the same ordering the machine's own slot uses
	// for its findings gate, and for the same reason.
	//
	// Before the network, because a held branch must cost no gh round trip
	// and no push: below this line is ForkRemote, two pull-request lookups
	// and the push itself, and every one of them would be work spent on a
	// change a person has already said stops.
	//
	// AND IT REFUSES THIS ROAD TOO. Every other gate around it is about
	// the machine and is nil for a person by construction. A hold is the
	// human's own instrument — often placed to stop themselves — and one
	// that `dockhand promote` walked past would be note-keeping rather
	// than a brake.
	if err := GateHold(n, branch, "the publication"); err != nil {
		return err
	}
	// The machine gate, beside the verdict gate and not inside it: what
	// a verdict set is worth is a judgment about evidence, and this is a
	// judgment about whether anybody has read a question. An unattended
	// road is refused; a person is told what they are publishing past
	// and allowed to, which is the difference the code was reserved for.
	if err := GateMachinePublish(n, branch, o.invoker()); err != nil {
		return err
	}
	for _, f := range Proposals(n) {
		fmt.Fprintf(e.Err, "promoting with %s still proposed; `dockhand dismiss %s` records that you looked and said no\n", f.Kind, branch)
	}
	for _, line := range gate.Blocked {
		fmt.Fprintln(e.Err, line)
	}
	if gate.SayUnverified {
		fmt.Fprintln(e.Err, "promoting unverified; the PR will say so")
	}
	// The dependents do not gate, so this is the only place a person is
	// told before the pull request exists. Said here and not left to the
	// body: the body is read by a reviewer afterwards, and the question
	// "do I want to publish this" is the author's, now.
	for _, line := range verdict.DependentsNotProven(n) {
		fmt.Fprintln(e.Err, line)
	}
	// The publication as the audit will record it, filled in as it
	// happens: a push with no pull request is a publication too, and it
	// is the one whose number stays zero.
	pub := Publication{MintSha: tip, Branch: branch,
		Port: n.Headline().Port, Target: n.Headline().Target,
		Verified: verified, Invoker: o.invoker(),
		// Who ASKED, as the record remembers, beside who is publishing.
		// The two part company on exactly the shape the slot exists to
		// produce — a person queues a change and an unattended pass puts it
		// out — and that is the row the trust ladder's numerator counts.
		AskedBy: n.AskedBy}

	forkRemote, forkOwner, err := gh.ForkRemote(ctx, e.Gh, repo, o.Remote)
	if err != nil {
		return err
	}
	if o.NoPR {
		if err := e.push(ctx, repo, forkRemote, forkOwner, branch, o.force(), o.invoker()); err != nil {
			return err
		}
		e.recordPublication(ctx, repo, pub)
		return nil
	}

	upstream, err := gh.UpstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	// The branch's own commits, oldest last (rev-list order): the
	// oldest is the one dockhand minted, and its subject is already in
	// project format (`<port>: <description>`) — later commits are
	// fixups whose subjects would make bad titles. The count also
	// answers the template's squashed-and-minimized checkbox.
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	ownCommits, err := repo.OwnCommits(ctx, tip, primary)
	if err != nil {
		return err
	}
	title := o.Title
	if title == "" {
		subject := tip
		if len(ownCommits) > 0 {
			subject = ownCommits[len(ownCommits)-1]
		}
		title, err = repo.Subject(ctx, subject)
		if err != nil {
			return err
		}
	}
	// A branch that already has its own open PR is re-promotion, not
	// duplication: the push below updates that PR in place, and opening
	// a second one would be the duplicate this verb refuses elsewhere.
	// Looked up by the fork owner, never by tracking config — a branch
	// --replace just re-minted has none until the push restores it.
	ownPR, ownFound, err := gh.QueryPR(ctx, e.Gh, upstream, forkOwner, branch)
	if err != nil {
		// A person is warned and carries on: they can see the queue, and
		// the worst case is a checklist box left unticked. THE MACHINE
		// REFUSES, because ownFound=false is not "no pull request" but
		// "nobody knows", and carrying on with it would open a second pull
		// request for a branch that already has one.
		if o.invoker() == record.Machine {
			return &ForgeLookupError{Branch: branch, What: "this branch's own pull request", Err: err}
		}
		fmt.Fprintf(e.Err, "warning: could not check for this branch's own PR: %v\n", err)
		ownFound = false
	}
	own := PRFact(ownPR, ownFound)
	if err := verdict.MergedDeadEnd(own, branch); err != nil {
		return err
	}
	// The machine's answer to its own open pull request, and it is HERE
	// rather than beside the convergence below because of what is between
	// the two: the push. Read in the order the code runs, the convergence
	// arm updates an open review's head and then prints a note saying so,
	// which is right for a person re-promoting on purpose and is a machine
	// force-updating somebody's review on nobody's authority. The slot's
	// phase gate is what should have stopped this a phase earlier; a
	// funnel that spent ring 3 on the assumption that it did would be
	// trusting a caller it exists not to trust.
	if o.invoker() == record.Machine && own.Found && own.Open {
		return &MachineRepublishError{Branch: branch, Number: own.Number, URL: own.URL}
	}

	checkedPRs := false
	if !o.noPRCheck() {
		port := verdict.PortName(n.Headline().Port, title)
		machine := o.invoker() == record.Machine
		switch prs, serr := gh.OpenPortPRs(ctx, e.Gh, upstream, port); {
		case port == "" && machine:
			return &ForgeLookupError{Branch: branch, What: "open pull requests on this port",
				Err: errNoPortToSearch}
		case port == "":
			fmt.Fprintln(e.Err, "warning: no port name to search open PRs by; skipping the duplicate check")
		case serr != nil && machine:
			return &ForgeLookupError{Branch: branch, What: "open pull requests on this port", Err: serr}
		case serr != nil:
			// The search is advisory: a rate-limited or offline lookup
			// must not block a promotion, it just leaves the checklist
			// box for the human. The comment names its own condition —
			// there IS no human on the machine road, which is why the two
			// arms above refuse instead.
			fmt.Fprintf(e.Err, "warning: could not search for open PRs: %v\n", serr)
		default:
			checkedPRs = true
			// The advisories are for the PRs the search walked past
			// before the duplicate, so they are said before the refusal
			// is returned — the same order the walk itself produced.
			dup := verdict.CheckDuplicates(prFacts(prs), own, title)
			for _, note := range dup.Notes {
				fmt.Fprintln(e.Err, note)
			}
			if dup.Refusal != nil {
				return dup.Refusal
			}
		}
	}

	if err := e.push(ctx, repo, forkRemote, forkOwner, branch, o.force(), o.invoker()); err != nil {
		return err
	}
	// The ticket the mint already recorded stands unless this promotion
	// names another. A change planned with --closes carries the trailer
	// in its commit and the number in its record; making the promoter
	// retype it would be asking twice for the same fact and getting a
	// body that disagrees with the commit for the trouble.
	closes := o.Closes
	if closes == "" {
		closes = n.ClosesTicket
	}
	// The tip and not the record's sha: EvidenceFor may have answered
	// this tip with a record earned over the identical tree at another
	// commit, and the head this push publishes is the one a reviewer can
	// look up.
	body := render.PRBody(n, verified, render.PRBodyOpts{
		Version: e.Version, Head: tip, Closes: closes,
		OwnCommits: len(ownCommits), CheckedPRs: checkedPRs})
	if own.Found && own.Open {
		if o.force() {
			// A replaced branch usually means a new version: the PR's
			// commits moved with the push, and its title and body are
			// stale until told otherwise.
			if _, err := e.publishGh(ctx, o.invoker(), "pr", "edit", fmt.Sprint(own.Number), "--repo", upstream,
				"--title", title, "--body", body); err != nil {
				return &PRRefreshError{Branch: branch, Remote: forkRemote, Number: own.Number, Err: err}
			}
			fmt.Fprintf(e.Err, "PR #%d replaced: branch force-pushed, title and body refreshed\n", own.Number)
		} else {
			fmt.Fprintf(e.Err, "PR #%d already open for this branch; the push updated it\n", own.Number)
		}
		fmt.Fprintln(e.Out, own.URL)
		pub.PRNumber = own.Number
		e.recordPublication(ctx, repo, pub)
		return nil
	}

	args := []string{"pr", "create", "--repo", upstream,
		"--head", forkOwner + ":" + branch, "--title", title, "--body", body}
	url, err := e.publishGh(ctx, o.invoker(), args...)
	if err != nil {
		return &PushedPRError{Branch: branch, Remote: forkRemote, Err: err}
	}
	url = strings.TrimSpace(url)
	fmt.Fprintln(e.Out, url)
	pub.PRNumber = prNumberIn(url)
	e.recordPublication(ctx, repo, pub)
	return nil
}

// recordPublication appends the audit's opening row, reporting a
// failure to write it as a warning and nothing more. By the time this
// runs the change is public: telling the user their promotion failed
// because a note could not be appended would be the more misleading of
// the two answers, and the exit code would be a lie about the PR.
func (e *Engine) recordPublication(ctx context.Context, repo *git.Repo, p Publication) {
	if err := e.Publish(ctx, repo, p); err != nil {
		fmt.Fprintf(e.Err, "warning: recording the publication: %v\n", err)
	}
}

// prNumberIn reads the pull request's number out of the URL `gh pr
// create` prints, which is the only thing it prints. A URL that does
// not end in a number yields zero — the audit row would rather say it
// does not know the number than claim a wrong one.
func prNumberIn(url string) int {
	n, err := strconv.Atoi(path.Base(url))
	if err != nil {
		return 0
	}
	return n
}

// push publishes the branch to the fork: an ordinary push, or the
// with-lease force that replaces a re-minted branch's copy.
//
// IT IS THE ONLY PUSH IN THE ENGINE, and the gate is inside it rather
// than beside its call sites for exactly that reason. A gate above the
// callers dominates the pushes this tree has today; a gate in the funnel
// dominates the ones it will have tomorrow, including the ones written
// by somebody who never read this file. git.Repo.Push and PushForce stay
// exported because git is a library, so what keeps the funnel a funnel
// is this method plus the structural test that fails if either is called
// anywhere else in internal/.
func (e *Engine) push(ctx context.Context, repo *git.Repo, remote, owner, branch string, force bool, by record.Driver) error {
	if err := GateRing3(by, e.MachinePublish); err != nil {
		return err
	}
	if force {
		if err := repo.PushForce(ctx, remote, branch); err != nil {
			return err
		}
		fmt.Fprintf(e.Err, "force-pushed %s to %s (%s)\n", branch, remote, owner)
		return nil
	}
	if err := repo.Push(ctx, remote, branch); err != nil {
		return err
	}
	fmt.Fprintf(e.Err, "pushed %s to %s (%s)\n", branch, remote, owner)
	return nil
}

// publishGh is the only way engine code reaches the forge with a WRITE.
// Every other gh call in this tree is a read — a search, a pulls query,
// `api user` — and goes through e.Gh unguarded, because reading spends
// nothing of ring 3.
//
// e.Gh is a raw string-args seam: any file in this package can build any
// argv, so a gate that lived beside the two write call sites would be a
// fact about today's call graph rather than an invariant. This is the
// funnel, the composition root wires a runner that refuses the same
// verbs underneath it, and the structural test fails if `pr create` or
// `pr edit` is spelled anywhere else.
func (e *Engine) publishGh(ctx context.Context, by record.Driver, args ...string) (string, error) {
	if err := GateRing3(by, e.MachinePublish); err != nil {
		return "", err
	}
	return e.Gh(ctx, args...)
}

// PrecheckPublish asks promote's two ring-3 questions over a change
// that has NOT been minted yet: does the branch this plan would create
// already have a merged pull request, and does an open one already
// propose the same title.
//
// It exists for --to-pr's immediate form, where minting and publishing
// are one invocation. Both refusals below would otherwise arrive after
// the branch existed, leaving the user a branch they did not want and a
// non-zero status about a publication that was never going to happen —
// and the merged case would leave it pointing at work the project has
// already taken. Asking first costs two forge READS and no ring 3.
//
// It is a precheck and never the gate. Promote asks both questions again
// over the branch it is publishing, because that is the road that
// pushes, and a check whose answer aged out between the mint and the
// push must be the one nearest the push. What this buys is the mint that
// does not happen.
//
// The lookups are advisory in exactly the way the human road's are: a
// forge that will not answer leaves the box unticked and a warning on
// stderr. This form is reachable only by a person — the boundary refuses
// every other invoker before it — so the reasoning promote states for
// warning rather than refusing holds here unchanged.
func (e *Engine) PrecheckPublish(ctx context.Context, repo *git.Repo, p *plan.Plan) error {
	// The branch and the title the mint WOULD produce, derived the way the
	// mint derives them rather than passed in: a precheck asking about a
	// different branch than the one about to exist would be a check with
	// nothing behind it.
	branch, title, port := git.MintBranchName(p.Slug), p.Summary, p.Port
	upstream, err := gh.UpstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	_, forkOwner, err := gh.ForkRemote(ctx, e.Gh, repo, "")
	if err != nil {
		return err
	}
	ownPR, ownFound, err := gh.QueryPR(ctx, e.Gh, upstream, forkOwner, branch)
	if err != nil {
		fmt.Fprintf(e.Err, "warning: could not check for this branch's own PR: %v\n", err)
		ownFound = false
	}
	own := PRFact(ownPR, ownFound)
	if err := verdict.MergedDeadEnd(own, branch); err != nil {
		return err
	}
	name := verdict.PortName(port, title)
	if name == "" {
		fmt.Fprintln(e.Err, "warning: no port name to search open PRs by; skipping the duplicate check")
		return nil
	}
	prs, serr := gh.OpenPortPRs(ctx, e.Gh, upstream, name)
	if serr != nil {
		fmt.Fprintf(e.Err, "warning: could not search for open PRs: %v\n", serr)
		return nil
	}
	dup := verdict.CheckDuplicates(prFacts(prs), own, title)
	for _, note := range dup.Notes {
		fmt.Fprintln(e.Err, note)
	}
	return dup.Refusal
}

// prFacts maps a list gh returned. Every entry exists, so every one of
// them is Found.
func prFacts(prs []gh.PullRequest) []verdict.PRFact {
	out := make([]verdict.PRFact, 0, len(prs))
	for _, pr := range prs {
		out = append(out, PRFact(pr, true))
	}
	return out
}
