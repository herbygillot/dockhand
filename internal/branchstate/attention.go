package branchstate

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// Kind is what an attention row asks of the person: something failed, a
// person's look or step is needed, or the branch is ready for one.
type Kind string

const (
	Failed   Kind = "failed"
	NeedsYou Kind = "needs_you"
	Ready    Kind = "ready"
)

// Reason is why a branch needs attention, typed, each with its words
// (Attention.Words) and the Next that moves it on, where status judged and
// worded each row in one function of the command (the roadmap's part 4,
// step 2).
type Reason string

const (
	PullRequestMaybeMerged Reason = "pull-request-maybe-merged"
	BranchGone             Reason = "branch-gone"
	StoppedCheck           Reason = "check-stopped"
	SomeoneElsePushed      Reason = "someone-else-pushed"
	ChangesRequested       Reason = "changes-requested"
	CIFailing              Reason = "ci-failing"
	OpenedByServe          Reason = "opened-by-serve"
	OnMaster               Reason = "on-master"
	HeldBeforeItsCheck     Reason = "held-before-check"
	FilesChangedSince      Reason = "files-changed-since"
	DependencyFailed       Reason = "dependency-failed"
	TargetFailed           Reason = "target-failed"
	RunFailed              Reason = "check-failed"
	RunAttention           Reason = "check-attention"
	BuildAgain             Reason = "build-again"
	Unbuilt                Reason = "unbuilt"
	ExtraFailed            Reason = "extra-failed"
	// LintWarned is a new port lint warned of, which submit asks to fix
	// or accept (the docs dig's I13).
	LintWarned      Reason = "lint-warned"
	CommitForReview Reason = "commit-for-review"
	Held            Reason = "held"
	// Awaiting is a concern the person decided to wait on, which blocks
	// its submission (the person's voice).
	Awaiting         Reason = "awaiting"
	SourceMoved      Reason = "source-moved"
	NotAssessed      Reason = "not-assessed"
	WaitingToSubmit  Reason = "waiting-to-submit"
	CheckWider       Reason = "check-wider"
	WorkPastMerge    Reason = "work-past-merge"
	Conflicting      Reason = "conflicting"
	AcceptedToSubmit Reason = "accepted-to-submit"
	NotPushed        Reason = "not-pushed"
)

// The verbs a verdict's Next names (refusal).
const (
	VerbStatus     = refusal.VerbStatus
	VerbRefresh    = refusal.VerbRefresh
	VerbAdopt      = refusal.VerbAdopt
	VerbWait       = refusal.VerbWait
	VerbSubmit     = refusal.VerbSubmit
	VerbEdit       = refusal.VerbEdit
	VerbArchive    = refusal.VerbArchive
	VerbCheck      = refusal.VerbCheck
	VerbLogs       = refusal.VerbLogs
	VerbTidy       = refusal.VerbTidy
	VerbOpenChecks = refusal.VerbOpenChecks
	VerbRerunCI    = refusal.VerbRerunCI
	VerbStart      = refusal.VerbStart
	VerbRebase     = refusal.VerbRebase
	VerbDecide     = refusal.VerbDecide
)

// Branch is what's recorded of a branch beside its checks, as attention
// reads it.
type Branch struct {
	Record model.Branch
	// Missing is a branch whose Git branch is gone; Cleaned one gone as
	// clean removes it, after its work landed or it was set aside.
	Missing, Cleaned bool
	// OnMaster is the master commit that has every change of the branch's
	// files, where master has them by another route.
	OnMaster model.ObjectID
	// Revision is the latest check's revision.
	Revision *model.Revision
	// Commits are how many commits the branch has beyond its base, and
	// Edited its files edited and not committed.
	Commits int
	Edited  []string
	// Edits are the branch's recorded edits, oldest first, which say a
	// hold before its check (Preparation).
	Edits []model.Edit
	// Held are what holds its submission, from its recorded assessments;
	// Moved the Git sources whose tag moved before its check.
	Held, Moved []model.Concern
	// Awaiting are the person's decisions to wait on a concern, which
	// block its submission until decided otherwise (the person's voice).
	Awaiting []model.Decision
	// NotAssessed is a branch some of whose files' assessments aren't
	// recorded yet, which submitting collects.
	NotAssessed bool
	// NoFork is a branch with no remote that pushes to a fork, and
	// Pushed one whose pull request has its head.
	NoFork, Pushed bool
	// OthersPushed is a pull request someone else pushed to, and
	// Failing the CI checks of it that fail.
	OthersPushed bool
	Failing      []string
	// CI are its failed CI jobs at the pull request's head, each as its
	// log was classified.
	CI []model.CIFailure
	// Ports are the ports its files change, in order.
	Ports []string
	// CheckOn is what a check that builds again where the latest one did
	// names, its --on values; none where build.default covers it.
	CheckOn []string
	// PastMerge are a merged branch's commits past what its pull request
	// merged, which start --from takes onto master (Then's item 7).
	PastMerge int
	// Wider is the check a passing attended check didn't build, where the
	// update gives a reviewer reason to ask for it: minmax for a floor it
	// raised, or a variant whose sources it changed (the docs dig's I47).
	Wider *Wider
}

// Wider is a check beyond the passing one, and why the update asks it.
type Wider struct {
	Next Next
	Why  string
}

// Attention is one row of what a branch needs: its kind, why, and what
// moves it on.
type Attention struct {
	Kind   Kind
	Reason Reason
	Branch string
	// What the reason is about, where it's about one: a check, a port, an
	// environment, a pull request, what its provider said, and the ports
	// it names.
	Run         *model.Run
	Port        string
	Environment model.Environment
	// Environments are where the latest check built, which files changed
	// since have no pass on.
	Environments []model.Environment
	PullRequest  *model.PullRequest
	Detail       string
	Ports        []string
	// CI are the failed CI jobs it says, each check's latest.
	CI []model.CIFailure
	// Revision is the latest check's revision, and Holds what holds it,
	// where the reason says them.
	Revision *model.Revision
	Holds    []model.Concern
	// Remade is the first target an environment change made build again.
	Remade *evidence.TargetEvidence
	// Serve is a branch serve prepared, whose submission is paused.
	Serve bool
	Next  Next
}

// Vocabulary words what branchstate's sentences name, each by its owner:
// an environment, a revision, a time, what made a target build again, and
// what holds a submission.
type Vocabulary interface {
	Environment(model.Environment) string
	Revision(model.Revision) string
	Ago(time.Time) string
	Remade(evidence.TargetEvidence) string
	Holds([]model.Concern) []string
	Unmet(model.Unmet) string
	// CIFailure is a classified CI failure, as "fetching
	// jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn answered 403) on
	// macos-15".
	CIFailure(model.CIFailure) string
	// CIPorts are each port the failed jobs built, with its result:
	// "exiv2 ✓, gpsbabel ✗ (build)"; empty where no log said any.
	CIPorts([]model.CIFailure) string
}

// Attend is what a branch needs, at most one row: from its pull request's
// last reading, its record, and its checks' standing (Check), where status
// re-derived the standing from the runs beside it, so an active baseline
// hid the row and a failed extra read as ready.
func Attend(facts Facts) []Attention {
	b := facts.Branch
	name := b.Record.ShortName()
	row := func(kind Kind, reason Reason, next Next, fill func(*Attention)) []Attention {
		next.Branch = name
		a := Attention{Kind: kind, Reason: reason, Branch: name, Next: next}
		if fill != nil {
			fill(&a)
		}
		return []Attention{a}
	}
	if b.Cleaned {
		return nil
	}
	if b.Missing && b.Record.PullRequest != nil {
		// Status never reads the network, so it can't know the pull
		// request merged: a branch cleaned by hand after its merge read as
		// trouble, adopt suggested, until --refresh (the sand-runner
		// session, batch 31).
		return row(NeedsYou, PullRequestMaybeMerged, Next{Verb: VerbRefresh}, func(a *Attention) { a.PullRequest = b.Record.PullRequest })
	}
	if b.Missing {
		return row(NeedsYou, BranchGone, Next{Verb: VerbAdopt, Args: []string{"<new name>"}}, nil)
	}
	check := facts.Check
	if run := check.Stopped; run != nil {
		return row(NeedsYou, StoppedCheck, Next{Verb: VerbWait, Args: []string{run.Name()}}, func(a *Attention) { a.Run = run })
	}
	if rows := pullRequestAttention(facts, row); len(rows) > 0 {
		return rows
	}
	// Work that went on past a merge is a follow-up's, started from master
	// with those commits picked onto it; wasmedge's was made by hand.
	if b.Record.State == model.BranchMerged && b.PastMerge > 0 {
		return row(Ready, WorkPastMerge, Next{Verb: VerbStart, Args: []string{"--from", name}}, func(a *Attention) {
			a.PullRequest, a.Detail = b.Record.PullRequest, prose.Plural(b.PastMerge, "commit")
		})
	}
	if pr := b.Record.PullRequest; pr != nil && b.Record.Origin == model.OriginServe && (pr.Observed == nil || pr.Observed.State == model.PullRequestOpen) {
		return row(Ready, OpenedByServe, Next{Verb: VerbStatus}, func(a *Attention) { a.PullRequest = pr })
	}
	// A branch set aside asks nothing of its checks.
	if b.Record.State == model.BranchArchived {
		return nil
	}
	// Work that landed on master by another route asks nothing more of
	// checks, only to be set aside (cleaning up duckdb-cxx14, finding 1).
	if b.OnMaster != "" && b.Record.PullRequest == nil {
		return row(Ready, OnMaster, Next{Verb: VerbArchive}, func(a *Attention) { a.Detail = string(b.OnMaster.Short()) })
	}
	standing := check.Standing()
	// A branch an unattended ship held at its committed edit, a patch not
	// applying, has no check to say; it says the hold (finding 2).
	if prepared := Preparation(b); standing == NotChecked && prepared.Disposition == HeldBeforeCheck {
		return row(NeedsYou, HeldBeforeItsCheck, Next{Verb: VerbCheck}, func(a *Attention) { a.Detail = prepared.Why })
	}
	if standing == Checking || standing == NotChecked {
		return nil
	}
	run := check.Latest
	// A check of other files, however it ended, is asked again, as submit
	// refuses for these files' missing check (the direction synthesis's
	// second contradiction).
	if !check.Current {
		if b.Revision == nil || run == nil || !run.State.Finished() {
			return nil
		}
		// It names where that check built, and its Next checks there, as
		// BuildAgain's does, where build.default's platforms were
		// suggested after an amend of a pass on two releases (field
		// testing's outdated-ports session).
		again := Next{Verb: VerbCheck}
		for _, on := range b.CheckOn {
			again.Args = append(again.Args, "--on", on)
		}
		var built []model.Environment
		if check.Evidence != nil {
			built = check.Evidence.Plan.Environments
		}
		return row(NeedsYou, FilesChangedSince, again, func(a *Attention) { a.Run, a.Revision, a.Environments = run, b.Revision, built })
	}
	if check.Evidence == nil {
		return nil
	}
	again := Next{Verb: VerbCheck}
	for _, on := range b.CheckOn {
		again.Args = append(again.Args, "--on", on)
	}
	// A failed check whose every failure a decision accepts goes on as a
	// pass does, said as accepted (C8-3).
	accepted := standing == CheckFailed && check.Publishable()
	if accepted {
		standing = CheckPassed
	}
	switch standing {
	case CheckFailed:
		for _, target := range check.Evidence.Failed() {
			for i, result := range target.Outcomes {
				if result.Outcome != model.OutcomeFailed {
					continue
				}
				reason := TargetFailed
				if result.Phase == model.PhaseDependencies {
					reason = DependencyFailed
				}
				next := Next{Verb: VerbLogs, Args: []string{run.Name(), target.Target.Target.Name}}
				// A failure its baseline has alike is the verdict's to say
				// with --accept, which dockhand never takes itself (Then's
				// item 4).
				if _, atBase := check.AtBase[target.Target.Target.Name]; atBase {
					next = Next{Verb: VerbSubmit, Args: []string{"--accept", target.Target.Target.Name}}
				}
				return row(Failed, reason, next, func(a *Attention) {
					a.Run, a.Port, a.Environment, a.Detail = run, target.Target.Target.Name, check.Evidence.Plan.Environments[i], result.Detail
					if reason == TargetFailed {
						a.Detail = string(result.Phase)
					}
				})
			}
		}
		return row(Failed, RunFailed, Next{Verb: VerbLogs, Args: []string{run.Name()}}, func(a *Attention) { a.Run, a.Detail = run, run.Detail })
	case CheckAttention:
		return row(NeedsYou, RunAttention, Next{Verb: VerbLogs, Args: []string{run.Name()}}, func(a *Attention) { a.Run, a.Detail = run, run.Detail })
	case CheckIncomplete:
		var unchecked, remade, extras []string
		var first *evidence.TargetEvidence
		for _, target := range check.Evidence.Failed() {
			switch {
			case len(target.Remade()) > 0 && target.Missing():
				if first == nil {
					first = &target
				}
				remade = append(remade, target.Target.Target.Name)
			case target.Missing():
				unchecked = append(unchecked, target.Target.Target.Name)
			default:
				extras = append(extras, target.Target.Target.Name)
			}
		}
		// A check that builds them again builds where this one did, which
		// build.default may not name: what still stands is reused, and the
		// rest built (the hugo exercise's re-checks after the guest fix).
		switch {
		case len(remade) > 0:
			return row(NeedsYou, BuildAgain, again, func(a *Attention) { a.Run, a.Ports, a.Remade = run, remade, first })
		case len(unchecked) > 0:
			return row(NeedsYou, Unbuilt, again, func(a *Attention) { a.Run, a.Ports = run, unchecked })
		case len(extras) > 0:
			// A failed extra from --also or a revision-only port, which
			// submit asks to accept: status read the check as ready.
			accept := submitNext(b)
			for _, extra := range extras {
				accept.Args = append(accept.Args, "--accept", extra)
			}
			return row(NeedsYou, ExtraFailed, accept, func(a *Attention) { a.Run, a.Ports = run, extras })
		}
		return nil
	case CheckPassed:
		var linted []string
		for _, block := range LintBlocks(*check.Evidence, check.Accepted) {
			linted = append(linted, block.Port)
		}
		switch {
		case len(b.Edited) > 0:
			return row(Ready, CommitForReview, Next{Verb: VerbTidy}, func(a *Attention) { a.Revision = b.Revision })
		case b.Record.PullRequest == nil && len(linted) > 0:
			// A new port's lint warnings count as ✗, which submit asks to
			// fix or accept, as a failed extra's.
			accept := submitNext(b)
			for _, port := range linted {
				accept.Args = append(accept.Args, "--accept", port)
			}
			return row(NeedsYou, LintWarned, accept, func(a *Attention) { a.Run, a.Ports = run, linted })
		case b.Record.PullRequest == nil && len(b.Awaiting) > 0:
			// The person is waiting on something for one of its
			// concerns, which submit refuses until it's decided.
			waiting := b.Awaiting[0]
			return row(NeedsYou, Awaiting, decideNext(waiting.Subject), func(a *Attention) { a.Detail = waiting.Subject + ": awaiting " + waiting.Words })
		case b.Record.PullRequest == nil && len(b.Held) > 0:
			// Paused where serve prepared it, as serve holds it; a person's
			// own submission shows it and goes on, as submit does (the
			// direction synthesis's third contradiction). Its Next is the
			// decision that answers the first, where a rule raised it, so
			// its rule's name is found where it holds (the prioritization
			// thread's word, 2026-10-27).
			next := submitNext(b)
			if i := slices.IndexFunc(b.Held, func(c model.Concern) bool { return c.Rule != "" && c.Port != "" && c.Origin != model.FromSubmission }); i >= 0 {
				next = decideNext(model.ConcernSubject(b.Held[i]))
			}
			return row(NeedsYou, Held, next, func(a *Attention) { a.Holds, a.Serve = b.Held, b.Record.Origin == model.OriginServe })
		case b.Record.PullRequest == nil && len(b.Moved) > 0:
			// The check built another source than the update chose: serve
			// waits for a look, and a person's submission shows it.
			return row(NeedsYou, SourceMoved, submitNext(b), func(a *Attention) {
				a.Holds, a.Serve = b.Moved, b.Record.Origin == model.OriginServe
			})
		case b.Record.PullRequest == nil && b.NotAssessed:
			// Status collects nothing; submitting assesses what isn't yet.
			return row(Ready, NotAssessed, submitNext(b), nil)
		case b.Record.PullRequest == nil && accepted:
			return row(Ready, AcceptedToSubmit, submitNext(b), func(a *Attention) { a.Run = run })
		case b.Record.PullRequest == nil && b.Wider != nil:
			return row(Ready, CheckWider, b.Wider.Next, func(a *Attention) { a.Detail = b.Wider.Why })
		case b.Record.PullRequest == nil:
			return row(Ready, WaitingToSubmit, submitNext(b), nil)
		case !b.Pushed:
			return row(Ready, NotPushed, submitNext(b), func(a *Attention) { a.PullRequest = b.Record.PullRequest })
		}
	}
	return nil
}

// decideNext is the decision that answers a concern, by its subject,
// "jq license-changed": dockhand decide license-changed jq --because "…".
func decideNext(subject string) Next {
	port, rule, _ := strings.Cut(subject, " ")
	// A moving source's subject names its commits after the rule.
	rule, _, _ = strings.Cut(rule, " ")
	return Next{Verb: VerbDecide, Args: []string{rule, port, "--because", `"…"`}}
}

// submitNext is the submit a branch is ready for, with the fork it needs
// first where no remote pushes to one: a Next: line names only what
// would take the branch (the M1's rerun, D-S6).
func submitNext(b Branch) Next {
	next := Next{Verb: VerbSubmit}
	if b.NoFork {
		next.After = AfterFork
	}
	return next
}

// pullRequestAttention is what the forge's last reading asks of you:
// someone else's push, requested changes, or failing CI.
func pullRequestAttention(facts Facts, row func(Kind, Reason, Next, func(*Attention)) []Attention) []Attention {
	b := facts.Branch
	pr := b.Record.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.State != model.PullRequestOpen {
		return nil
	}
	look := Next{Verb: VerbStatus}
	if len(b.Ports) > 0 {
		look = Next{Verb: VerbEdit, Args: []string{b.Ports[0]}}
	}
	with := func(a *Attention) { a.PullRequest = pr }
	switch {
	case b.OthersPushed:
		return row(NeedsYou, SomeoneElsePushed, Next{Verb: VerbSubmit}, with)
	case pr.Observed.Review == model.ReviewChangesRequested:
		return row(NeedsYou, ChangesRequested, look, with)
	case pr.Observed.Conflicts:
		// principles.md promises it: "reports, and offers the command".
		return row(NeedsYou, Conflicting, Next{Verb: VerbRebase}, with)
	case pr.Observed.Checks == model.ChecksFailing:
		// A failure every failed job of which was a fetch, as a mirror's
		// 403, is one another run may get past: its Next runs them again
		// (garage #35174, field testing, 2026-10-03). Any other is looked
		// at where CI shows it.
		next := Next{Verb: VerbOpenChecks, Args: []string{pr.Repository, fmt.Sprint(pr.Number)}}
		failures := LatestCI(b.CI)
		if len(failures) > 0 && !slices.ContainsFunc(failures, func(f model.CIFailure) bool { return f.Cause != model.CIFetch }) {
			next = Next{Verb: VerbRerunCI}
		}
		return row(Failed, CIFailing, next, func(a *Attention) {
			a.PullRequest, a.Ports, a.CI = pr, b.Failing, failures
		})
	}
	return nil
}

// Words are the row's sentence, as status prints it beside its branch,
// what it names worded by their owners.
func (a Attention) Words(v Vocabulary) string {
	pr := a.PullRequest
	age := func(at time.Time) string { return " (" + v.Ago(at) + ")" }
	paused := func(words string) string {
		if a.Serve {
			return "passed; submission paused for review: " + words
		}
		return "passed; " + words
	}
	switch a.Reason {
	case PullRequestMaybeMerged:
		return fmt.Sprintf("Git branch not found; pull request #%d may have merged", pr.Number)
	case BranchGone:
		return "Git branch not found"
	case StoppedCheck:
		// The row's command resumes it, said once (part 5, item 4).
		return a.Run.Name() + " stopped because its process ended. Use the command below to resume it, or dockhand cancel --branch " + a.Branch + " to cancel it"
	case SomeoneElsePushed:
		return fmt.Sprintf("someone else pushed to #%d%s", pr.Number, age(pr.Observed.At))
	case ChangesRequested:
		at := pr.Observed.At
		if !pr.Observed.ReviewedAt.IsZero() {
			at = pr.Observed.ReviewedAt
		}
		return fmt.Sprintf("#%d: changes requested%s", pr.Number, age(at))
	case CIFailing:
		// A failure no reading named says its log's first error line,
		// where there is one (Then's item 7: xapian-core's said nothing).
		var classified []string
		for _, failure := range a.CI {
			if failure.Cause != model.CIUnclassified || failure.Error != "" {
				classified = append(classified, v.CIFailure(failure))
			}
		}
		ports := ""
		if words := v.CIPorts(a.CI); words != "" {
			ports = " (CI: " + words + ")"
		}
		if len(classified) > 0 {
			return fmt.Sprintf("#%d: MacPorts CI failed: %s%s%s", pr.Number, strings.Join(classified, "; "), ports, age(pr.Observed.At))
		}
		return fmt.Sprintf("#%d: MacPorts CI is failing for %s%s%s", pr.Number, strings.Join(a.Ports, ", "), ports, age(pr.Observed.At))
	case OpenedByServe:
		return fmt.Sprintf("#%d was opened by serve without human review", pr.Number)
	case OnMaster:
		return "its changes are on master already, as of " + a.Detail
	case HeldBeforeItsCheck:
		return HeldWords(a.Detail)
	case FilesChangedSince:
		ended := string(a.Run.State)
		if a.Run.State == model.RunAttention {
			ended = "needed attention"
		}
		if len(a.Environments) > 0 {
			var where []string
			for _, environment := range a.Environments {
				where = append(where, v.Environment(environment))
			}
			return v.Revision(*a.Revision) + " " + ended + " on " + prose.And(where) + "; the files have changed since, with no check there"
		}
		return v.Revision(*a.Revision) + " " + ended + "; the files have changed since"
	case DependencyFailed:
		// The dependency is named beside it, as MacPorts says it.
		return fmt.Sprintf("%s: a dependency failed on %s (%s): %s", a.Port, v.Environment(a.Environment), a.Run.Name(), a.Detail)
	case TargetFailed:
		return fmt.Sprintf("%s failed at %s on %s (%s)", a.Port, a.Detail, v.Environment(a.Environment), a.Run.Name())
	case RunFailed:
		return a.Run.Name() + " failed: " + a.Detail
	case RunAttention:
		return a.Run.Name() + " needs attention: " + a.Detail
	case BuildAgain:
		return fmt.Sprintf("%s passed, but since then %s; %s must be built there again", a.Run.Name(), v.Remade(*a.Remade), strings.Join(a.Ports, ", "))
	case Unbuilt:
		return fmt.Sprintf("%s passed, but no check of these files built %s", a.Run.Name(), strings.Join(a.Ports, ", "))
	case ExtraFailed:
		return fmt.Sprintf("%s passed the checks required for submission, but %s did not pass. Review the failure before accepting it for submission", a.Run.Name(), strings.Join(a.Ports, ", "))
	case LintWarned:
		return fmt.Sprintf("%s passed, but lint --nitpick warned of the new %s %s. Fix what it says (dockhand status %s), or accept it for submission", a.Run.Name(), map[bool]string{true: "port", false: "ports"}[len(a.Ports) == 1], prose.And(a.Ports), a.Branch)
	case CommitForReview:
		return v.Revision(*a.Revision) + " passed; commit it for review"
	case Held, SourceMoved:
		lines := v.Holds(a.Holds)
		if a.Reason == SourceMoved {
			lines = []string{a.Holds[0].Detail}
			if !a.Serve {
				return "passed; " + lines[0]
			}
		}
		first := lines[0]
		if len(lines) > 1 {
			first += fmt.Sprintf(" (and %d more: dockhand status %s)", len(lines)-1, a.Branch)
		}
		if a.Reason == Held && !a.Serve {
			return paused(first + "; run submit to review it")
		}
		return paused(first)
	case NotAssessed:
		return "passed; the upstream changes have not yet been assessed. Submit will assess them"
	case Awaiting:
		return "awaiting your decision on " + a.Detail
	case WaitingToSubmit:
		return "passed; waiting for you to submit"
	case Conflicting:
		return fmt.Sprintf("#%d conflicts with master", pr.Number)
	case WorkPastMerge:
		return fmt.Sprintf("#%d merged; %s past the merge %s in it, for a follow-up", pr.Number, a.Detail, map[bool]string{true: "isn't", false: "aren't"}[a.Detail == "1 commit"])
	case CheckWider:
		return "passed on this Mac's release alone; " + a.Detail + ", so a wider check comes first; submit still goes on as it stands"
	case AcceptedToSubmit:
		return a.Run.Name() + "'s failures are accepted; waiting for you to submit"
	case NotPushed:
		return fmt.Sprintf("passed; #%d has not been updated with these changes", pr.Number)
	}
	return string(a.Reason)
}

// LatestCI are a head's CI failures, each check's latest attempt alone: a
// job run again and failed again is said once, as it last failed.
func LatestCI(failures []model.CIFailure) []model.CIFailure {
	latest := map[string]model.CIFailure{}
	var order []string
	for _, f := range failures {
		kept, ok := latest[f.Check]
		if !ok {
			order = append(order, f.Check)
		}
		if !ok || f.Run > kept.Run || f.Run == kept.Run && f.Attempt > kept.Attempt {
			latest[f.Check] = f
		}
	}
	var kept []model.CIFailure
	for _, check := range order {
		kept = append(kept, latest[check])
	}
	return kept
}
