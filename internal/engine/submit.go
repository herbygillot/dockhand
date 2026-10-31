package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/portcreate"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// UpstreamBranch is the branch pull requests are opened against.
const UpstreamBranch = "master"

// SubmitRequest asks to open or update a branch's pull request
// (Design v3 §9).
type SubmitRequest struct {
	Branch model.Branch
	// Head submits the committed head and leaves uncommitted edits out.
	Head bool
	// Draft opens the pull request as a draft, which unfinished or failing
	// checks allow.
	Draft bool
	// Bound is the commit a caller bound the submission to, as ship binds
	// the one its check was of; ApplySubmit refuses another (branchstate
	// .Publish). Empty where none was.
	Bound string
	// NoCheck submits without any check; the description says so.
	NoCheck bool
	// PendingCheck previews a submission whose check is still to run, for
	// ship, which checks first: having none yet does not stop it. Where and
	// Tests are where and with what tests that check was asked to build,
	// none where it wasn't: a finished check stands for the ask only where
	// it covers them (Covered), else CheckNeeded (the architecture review
	// of 5897a82, R3-8).
	PendingCheck bool
	Where        WhereRequest
	Tests        model.TestPolicy
	// Accept acknowledges failed revision-only targets or extras by port.
	Accept []string
	// Standing accepts each port Accept names whose every failure is a
	// dependency failing to install, by the dependency's version on the
	// image it failed on, for every branch until either changes.
	Standing bool
	// Title replaces the branch's title, and the pull request's.
	Title string
	// Types are the template's Type(s) that apply.
	Types []string
	// Remote names the Git remote of your fork when more than one could be.
	Remote           string
	SkipNotification bool
	// TestedBinaries and TestedVariants are the person's statements for
	// the template's last two items.
	TestedBinaries, TestedVariants bool
	// Rewrite replaces an open pull request's description with the one
	// dockhand composes, a person's own included, their note kept: an
	// adopted branch's description, which nothing recorded dockhand
	// writing, is otherwise theirs and stays (#34756 kept September's
	// Tested on under the v0.3.0 commit). Never someone else's.
	Rewrite bool
	// Note replaces the branch's note, which the description gives under
	// Description (model.Branch.Note), and empty clears it; nil keeps the
	// one recorded. It is recorded once the submission is applied.
	Note *string
}

// SubmitPlan is exactly what submit would do, bound to the branch head
// and the fork's branch head it saw.
type SubmitPlan struct {
	Request SubmitRequest
	Branch  model.Branch
	Commit  string
	Tree    string
	Commits []git.HistoryCommit
	Ports   []string
	// recorded is Ports read from every directory's change record.
	recorded bool
	// moved are the versions the change records move ports to, by port.
	moved    map[string]string
	Findings []commitrules.Finding
	// LeftOut are uncommitted files --head leaves out.
	LeftOut []string
	// ChangedPatches are the patches the branch changed under their own
	// names, which the base had: a notice, since the upstream assessment
	// reads the base's source for them and says nothing of the patch, and
	// a check applies it (v0.4.0's known issues).
	ChangedPatches []string
	// ModifiedBuilds are the commits whose Generated-By names a dockhand
	// built from uncommitted source, which nobody else can find: shown,
	// so they can be tidied again with a build of a pushed commit.
	ModifiedBuilds []string
	// UnfoundBuilds are the other builds the commits name in Generated-By
	// that nobody else can find either: dockhand's repository on GitHub
	// doesn't have what each was built from, or it names nothing. Shown,
	// holding nothing, as ModifiedBuilds are.
	UnfoundBuilds []UnfoundBuild
	// BuildsProblem says why GitHub couldn't be asked about them.
	BuildsProblem string

	Repository, HeadRepository, PushURL string
	// Login is who the submission pushes and writes the pull request as,
	// and where its token comes from, which the preview names (the rc9
	// full stage's D-C4).
	Login forge.Login
	// RemoteHead is the fork's branch as it was seen.
	RemoteHead git.RefValue
	// Replaces is true when the push replaces history rather than adding
	// to it.
	Replaces bool
	// NoFork is true where no fork was found to push to, which holds the
	// submission (branchstate.BlockNoFork); Unreached where the fork
	// refused this Mac (branchstate.BlockForkUnreached).
	NoFork, Unreached bool
	// Amended is someone else's commit the push replaces, which the
	// branch's reflog shows and the commit amends: said on the preview's
	// Push line, as only the reflog vouches for it (the rc5 full stage,
	// D-S1).
	Amended model.ObjectID
	// OthersPushed is what someone else pushed to the pull request since
	// dockhand last did, which holds the push; empty where none did.
	OthersPushed model.ObjectID
	// Existing is the pull request already open for the branch.
	Existing *forge.PullRequestObservation
	// Earlier is a pull request from the same head branch name that is
	// closed or merged, which isn't the branch's: a new one opens beside
	// it, and the preview says so.
	Earlier *forge.PullRequest

	Title string
	Body  string
	// BodyKept is true when a person's edits to the description are kept
	// as they are.
	BodyKept bool
	// Sections are what submitting again does to each part of an existing
	// pull request's description that dockhand writes; zero for a new one.
	Sections DescriptionSections
	Evidence *Evidence
	// AtBase are the changed ports that failed whose baseline fails them
	// the same way, each with the baselines that did: --accept takes
	// them, and the pull request says so (the person's word, 2026-10-17).
	AtBase map[string][]string
	// Accepted are the failed ports the submission accepts, each with why,
	// as the pull request's table and the preview's Checks line say it:
	// its baseline failing alike, the cause not established, or blocked
	// only by accepted failures.
	Accepted map[string]AcceptedFailure
	// Blocks are what stops the submission, each of its kind
	// (branchstate.Block), whose words Blocking says.
	Blocks []branchstate.Block
	// Standing are the standing decisions --standing asks for, which the
	// submission records; StandingAccepted the failed ports a standing
	// decision accepts, recorded or asked for.
	Standing         []model.Decision
	StandingAccepted map[string]AcceptedFailure
	// UncoveredCI are the releases MacPorts' CI builds on that the check
	// built on none of, which the preview and the pull request say.
	UncoveredCI []string
	// Upstream is what upstream's change means for each port the commit
	// changes, against the branch's base: its revision's assessments,
	// collected where they weren't recorded (the assessment design, D). A
	// person's submission shows them; only one nobody looks over is held
	// for them (D4).
	Upstream []PortComparison
	// Searched are the ports other pull requests were looked for under:
	// the ports whose source changed, terraform-1.16, rather than the
	// directory's terraform (batch 40); the directories' names where none
	// did.
	Searched []string
	Others   []forge.PullRequestSummary
	// SearchProblem says why other pull requests could not be looked for.
	SearchProblem string
	// Moved are the Git-fetched ports whose git.branch named another
	// commit when the check the submission rests on planned it than when
	// their update chose it (preparedSources), or names another now than
	// then (movedSources).
	Moved []model.Concern
	// Majors are the major-version holds of the branch's updates
	// (holds.majors), and Floors where it raises the oldest macOS a port
	// builds on, each of which holds a submission nobody looks over.
	Majors []model.Concern
	Floors []model.Concern
	// Undeclared are the libraries its check's builds link from a port
	// they don't declare in depends_lib, each of which holds a submission
	// nobody looks over (linkHolds).
	Undeclared []model.Concern
	// Decided are the person's decisions on its concerns that stand for
	// its files, by subject (the person's voice): an acceptance lets its
	// concern go for a submission nobody looks over, said, and given in
	// the description; one awaiting something blocks it.
	Decided map[string]model.Decision
	// Prereleases say each update that takes a port from a stable version
	// to a prerelease (LeavesStableWords), which holds nothing.
	Prereleases []string
	// After are the holds of a port whose holds name ports to submit
	// first, at the same version (afterHolds).
	After []model.Concern
	// Blocking is what stops the submission.
	Blocking []string
	// CheckNeeded is true when a PendingCheck plan has no check for its
	// files yet, or only one that built them somewhere they're required.
	CheckNeeded bool
	// Theirs is true for a pull request someone else opened: submit only
	// pushes to it, and never rewrites its title or description.
	Theirs bool
	// Note is the person's note the description gives under Description:
	// the one the request gives, or the branch's, trimmed and with Unix
	// line endings. NoteLeftOut is true when the description won't give
	// it, since its Description is a person's own, edited since dockhand
	// wrote it or left out, which submit keeps as it is.
	Note        string
	NoteLeftOut bool
	// Written is what dockhand has written of Body, recorded as what it
	// last wrote: Body, but each part kept as someone's own carries
	// dockhand's last text of it (prdescription.Written).
	Written string
	// described is a description the person wrote whole (Describe),
	// which a fresh read can't merge into.
	described bool

	facts bodyFacts
	// allowed are the rules the configuration's hold list sets apart for
	// a port (holds.allow, or its own), whose concerns Held leaves out.
	allowed func(port string) []string
}

// PortComparison is what upstream's change means for a port a revision
// changes.
type PortComparison struct {
	Port       string
	Comparison model.UpstreamComparison
}

// Answer records the person's statements for the template's last two
// items, which only they can make, and writes the description again.
func (p *SubmitPlan) Answer(testedBinaries, testedVariants bool) {
	p.Request.TestedBinaries, p.Request.TestedVariants = testedBinaries, testedVariants
	p.facts.TestedBinaries, p.facts.TestedVariants = testedBinaries, testedVariants
	p.Body = pullRequestBody(p.facts)
	p.Written = p.Body
	if p.Existing != nil {
		last := ""
		if p.Branch.PullRequest != nil {
			last = p.Branch.PullRequest.Body
		}
		if p.Request.Rewrite && !p.Theirs {
			p.Sections = DescriptionSections{Description: SectionRefreshed, Types: SectionRefreshed, TestedOn: SectionRefreshed}
			p.BodyKept, p.NoteLeftOut = false, false
			return
		}
		p.Body, p.Sections = prdescription.Merge(p.Existing.PullRequest.Body, last, p.Body, len(p.Request.Types) > 0)
		p.Written = prdescription.Written(p.Body, last, p.Sections)
		// Someone else's description is never rewritten (ApplySubmit).
		if p.Theirs {
			p.Sections = DescriptionSections{Description: SectionKept, Types: SectionKept, TestedOn: SectionKept}
		}
		p.BodyKept = p.Sections.TestedOn == SectionKept || p.Sections.TestedOn == SectionAbsent
		p.NoteLeftOut = p.Note != "" && (p.Sections.Description == SectionKept || p.Sections.Description == SectionAbsent)
	}
}

// DescriptionSaysNothing reports a Description dockhand writes that would
// say nothing of the change (prdescription.SaysNothing), for submit to
// point at --note; one a person keeps is theirs.
func (p SubmitPlan) DescriptionSaysNothing() bool {
	if p.Theirs || p.Sections.Description == SectionKept || p.Sections.Description == SectionAbsent {
		return false
	}
	return prdescription.SaysNothing(p.facts.description())
}

// NoteRepeatsCommits reports a note that mostly repeats the commits'
// bodies, which the Description gives already (prdescription).
func (p SubmitPlan) NoteRepeatsCommits() bool {
	return p.Note != "" && prdescription.NoteRepeatsCommits(p.facts.description())
}

// NoteKept reports a note from an earlier submit, which this one keeps,
// across rewritten commits too, since no --note was given.
func (p SubmitPlan) NoteKept() bool { return p.Note != "" && p.Request.Note == nil }

// Describe replaces the description with one the person wrote, which
// gives their note however they left it.
func (p *SubmitPlan) Describe(body string) {
	p.Body, p.Written, p.BodyKept, p.NoteLeftOut, p.described = body, body, true, false, true
	p.Sections = DescriptionSections{Description: SectionKept, Types: SectionKept, TestedOn: SectionKept}
}

// unchangedSince reads the pull request again, where the plan writes its
// title or description, and makes the plan's text against what it is now
// where someone edited it on GitHub since the preview: the merge keeps
// their edit as it keeps any, and a title left as it was follows theirs.
// Where the plan can't merge, as for a description the person wrote whole
// or one --rewrite-description replaces, it refuses, naming the change.
// The planned title and body were written with nothing checked, so an
// edit made while submit waited for confirmation was lost (Codex's review
// of 2ad896d2, W7). GitHub has no conditional write for a pull request's
// title or body: an edit in the moment between this read and the write
// can still be lost; unattended, there's no wait for one to land in.
func (e *Engine) unchangedSince(ctx context.Context, plan *SubmitPlan) error {
	if plan.Existing == nil || plan.Theirs {
		return nil
	}
	fresh, err := e.forge().Observe(ctx, plan.Existing.PullRequest.Ref)
	if err != nil {
		return fmt.Errorf("reading #%d again before writing it: %w", plan.Existing.PullRequest.Ref.Number, err)
	}
	was := plan.Existing.PullRequest
	now := fresh.PullRequest
	if now.Title == was.Title && now.Body == was.Body {
		return nil
	}
	var changed []string
	if now.Title != was.Title {
		changed = append(changed, "title")
	}
	if now.Body != was.Body {
		changed = append(changed, "description")
	}
	if now.Body != was.Body && (plan.described || plan.Request.Rewrite) {
		return fmt.Errorf("#%d's %s changed on GitHub since the preview, and this submission would replace it; nothing was pushed: run dockhand submit again to see it", was.Ref.Number, prose.And(changed))
	}
	title := plan.Title
	if plan.Request.Title == "" && plan.Branch.Title == "" && plan.Title == was.Title {
		title = now.Title
	}
	plan.Existing = &fresh
	plan.Answer(plan.Request.TestedBinaries, plan.Request.TestedVariants)
	plan.Title = title
	progress.Report(ctx, "#%d's %s changed on GitHub since the preview; this submission keeps the change", was.Ref.Number, prose.And(changed))
	return nil
}

// Head is the fork's head as "owner/repo:branch".
func (p SubmitPlan) Head() string { return p.HeadRepository + ":" + p.RemoteBranch() }

// ToDraft reports whether a draft submission returns its open pull request
// to draft before it pushes: a draft request lets failing or unchecked
// work through, which only a pull request that is a draft may carry, and
// an update edited only the title and body, so failing work reached a
// ready pull request (Codex's review of 2ad896d2, W3). The forge leaves
// one already a draft as it is.
func (p SubmitPlan) ToDraft() bool { return p.Request.Draft && p.Existing != nil }

// WasDraft reports whether the branch's record says its pull request is a
// draft, as dockhand made it or last read it.
func (p SubmitPlan) WasDraft() bool {
	pr := p.Branch.PullRequest
	return pr != nil && (pr.Draft || pr.Observed != nil && pr.Observed.Draft)
}

// ForkOwner is the account that owns the repository the submission pushes
// to, and whether it isn't the login's own: a fork's owner check gives way
// under DOCKHAND_PULL_REQUESTS's sandbox and to --remote, so GitHub's
// permission is all that lets the login push there, and the pull request
// is the login's, not the owner's (the rc9 full stage's D-C4). Pushing to
// someone else's pull request (Theirs) is that already, and isn't another.
func (p SubmitPlan) ForkOwner() (owner string, another bool) {
	owner, _, _ = strings.Cut(p.HeadRepository, "/")
	return owner, !p.Theirs && p.Login.Account != "" && owner != "" && !strings.EqualFold(owner, p.Login.Account)
}

// RemoteBranch is the fork's branch submit pushes to: the one the pull
// request was opened from, which renaming the local branch does not move
// (a pull request's head can't change), else the branch's own name.
func (p SubmitPlan) RemoteBranch() string {
	if pr := p.Branch.PullRequest; pr != nil {
		if _, name := pr.HeadParts(); name != "" {
			return name
		}
	}
	return p.Branch.Name
}

// PlanSubmit works out what submit would push and publish, and why it
// may not, changing nothing.
func (e *Engine) PlanSubmit(ctx context.Context, request SubmitRequest) (SubmitPlan, error) {
	// The record, not the caller's copy: what was last pushed decides
	// whether someone else has pushed since.
	var branch model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(request.Branch.ID)
		return err
	}); err != nil {
		return SubmitPlan{}, err
	}
	request.Branch = branch
	plan := SubmitPlan{Request: request, Branch: branch, Repository: e.PullRequestRepository()}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return plan, err
	}
	head, final, err := worktree.WorkingTree(ctx)
	if err != nil {
		return plan, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head, string(branch.Base)})
	if err != nil {
		return plan, err
	}
	plan.Commit, plan.Tree = head, trees[head]
	if final != plan.Tree {
		if plan.LeftOut, err = worktree.ChangedPaths(ctx, plan.Tree, final); err != nil {
			return plan, err
		}
		if !request.Head {
			return plan, &Uncommitted{Paths: plan.LeftOut}
		}
	}
	if plan.Commits, err = worktree.History(ctx, string(branch.Base), head); err != nil {
		return plan, err
	}
	if len(plan.Commits) == 0 {
		return plan, &Uncommitted{Branch: branch.Name}
	}
	if err := e.commitsAreOwn(ctx, worktree, branch, string(branch.Base), head, len(plan.Commits), "submitted"); err != nil {
		return plan, err
	}
	for _, commit := range plan.Commits {
		if commitmsg.ModifiedBuild(commit.Message) {
			plan.ModifiedBuilds = append(plan.ModifiedBuilds, commit.ID)
		}
	}
	analysis, err := e.analyze(ctx, branch.Base, model.ObjectID(plan.Tree))
	if err != nil {
		return plan, err
	}
	changed := analysis.Paths
	// The ports are the subports the revision's change records say it
	// changes: terraform-1.16, where the directory is terraform's.
	records, err := e.revisionChanges(ctx, branch.ID, branch.Base, model.ObjectID(plan.Tree), true)
	if err != nil {
		return plan, err
	}
	var notes map[string]string
	plan.Ports, notes = recordedPorts(analysis.Scope.Ports, records)
	plan.recorded = len(notes) == 0
	plan.moved = movedVersions(records)
	newPorts := e.newPorts(ctx, worktree, model.Source{Commit: model.ObjectID(head), Tree: model.ObjectID(plan.Tree), Base: branch.Base}, trees[string(branch.Base)], changed, analysis.Scope)
	plan.Findings = commitrules.CheckCommits(e.ruleCommits(ctx, model.Source{Commit: branch.Base, Base: branch.Base, Tree: model.ObjectID(trees[string(branch.Base)])},
		model.Source{Commit: model.ObjectID(head), Base: branch.Base, Tree: model.ObjectID(plan.Tree)}, plan.Commits))
	e.sayWhose(ctx, branch)
	portfiles, err := e.portfileFindings(ctx, worktree, trees[string(branch.Base)], plan.Tree, changed)
	if err != nil {
		return plan, err
	}
	plan.Findings = append(plan.Findings, portfiles...)
	plan.ChangedPatches = changedPatches(ctx, worktree, trees[string(branch.Base)], changed)
	// What create guessed and marked unconfirmed is a person's to confirm
	// before MacPorts sees it: serie's preview said ready with six marks
	// left, nomaintainer among them (the rc6 full stage, B8).
	for _, path := range changed {
		if !strings.HasSuffix(path, "/Portfile") {
			continue
		}
		if _, text, err := worktree.File(ctx, plan.Tree, path); err == nil {
			if marked := portcreate.Marked(text); len(marked) > 0 {
				plan.block(branchstate.Block{Kind: branchstate.BlockUnconfirmed, Path: path, Marks: marked, Mark: portcreate.Unconfirmed})
			}
		}
	}
	for _, commit := range plan.Commits {
		if commit.Merge() {
			plan.block(branchstate.Block{Kind: branchstate.BlockMerge, Commit: model.ObjectID(commit.ID)})
		}
	}

	// What's recorded of the branch at this commit, read once for the
	// phases below: the failures accepted, and the edits dockhand made.
	var accepted []string
	var standing []model.Decision
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		list, err := r.Decisions(branch.ID, model.FindingFailure, head)
		for _, d := range list {
			accepted = append(accepted, d.Subject)
		}
		if err != nil {
			return err
		}
		if standing, err = r.StandingDecisions(model.FindingEnvironmentFailure); err != nil {
			return err
		}
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return plan, err
	}
	// The phases, in order: the evidence and what blocks publishing it;
	// where submit pushes, and the pull request open there (Existing),
	// which the title and the search for others read.
	if err := e.evidence(ctx, &plan, accepted, standing); err != nil {
		return plan, err
	}
	plan.Moved = e.movedSources(ctx, plan.Evidence)
	moved := plan.Moved
	if err := e.destination(ctx, worktree, &plan); err != nil {
		return plan, err
	}
	plan.Note = branch.Note
	if request.Note != nil {
		plan.Note = strings.TrimSpace(strings.ReplaceAll(*request.Note, "\r\n", "\n"))
		// Their description is theirs, which submit never rewrites, so a
		// note would be recorded and never given.
		if plan.Theirs && plan.Note != "" {
			return plan, fmt.Errorf("--note: #%d was opened from %s, so its description stays theirs; say it in a comment on #%d instead", plan.Branch.PullRequest.Number, plan.HeadRepository, plan.Branch.PullRequest.Number)
		}
	}
	if request.Rewrite && plan.Theirs {
		return plan, fmt.Errorf("--rewrite-description: #%d was opened from %s, so its description stays theirs", plan.Branch.PullRequest.Number, plan.HeadRepository)
	}
	e.title(&plan)
	plan.UnfoundBuilds, plan.BuildsProblem = e.unfoundBuilds(ctx, plan.Commits)
	if plan.Upstream, err = e.revisionComparisons(ctx, branch, model.ObjectID(plan.Tree), true); err != nil {
		return plan, err
	}
	e.searchOthers(ctx, &plan)
	errorsFound := commitrules.Errors(plan.Findings)
	squashed := !slices.ContainsFunc(plan.Findings, func(f commitrules.Finding) bool { return f.Code == "follow-up" || f.Code == "merge" })
	plan.Moved = append(append(preparedSources(plan.Evidence, edits), plan.Moved...), assessedSources(plan.Evidence, plan.Upstream)...)
	plan.Majors = e.majorHolds(edits)
	plan.Prereleases = prereleases(edits)
	plan.After = e.afterHolds(ctx, edits)
	plan.Floors = e.floorHolds(ctx, branch.ID, branch.Base, model.ObjectID(plan.Tree))
	plan.Undeclared = e.linkHolds(ctx, plan.Evidence)
	plan.Decided = e.decided(ctx, branch.ID, plan.Tree)
	var reviewed []string
	for _, subject := range slices.Sorted(maps.Keys(plan.Decided)) {
		decision := plan.Decided[subject]
		if decision.Answer == model.Accepted {
			reviewed = append(reviewed, DecisionWords(decision))
		} else if !request.Draft {
			plan.block(branchstate.Block{Kind: branchstate.BlockAwaiting, Port: subject, Awaited: decision.Words})
		}
	}
	facts := bodyFacts{Commits: plan.Commits, Evidence: plan.Evidence, NoCheck: request.NoCheck, Accepted: plan.Accepted, Types: request.Types,
		Updated:     len(plan.moved) > 0,
		RulesPassed: !errorsFound, Squashed: squashed, Searched: plan.SearchProblem == "", Others: plan.Others,
		TestedBinaries: request.TestedBinaries, TestedVariants: request.TestedVariants, SkipNotification: request.SkipNotification, NewPorts: newPorts,
		Note: plan.Note, UncoveredCI: plan.UncoveredCI, HandEdits: e.submittedHandEdits(ctx, branch, plan.Tree), LeftOut: leftOut(edits), HTTPS: httpsSwitches(edits),
		Installed: e.installedLines(ctx, plan.Evidence, edits), Reviewed: reviewed}
	plan.facts = facts
	plan.allowed = e.allowedFor
	plan.Answer(request.TestedBinaries, request.TestedVariants)
	// What took the network to find is kept for status, which never reads
	// it, to say as this plan found it (C2's rest): the moved sources and
	// the other pull requests. Keeping it is an observation, which a plan
	// may record; a failure to keep it fails nothing the plan says.
	network := append(slices.Clone(moved), othersConcerns(plan.Others, plan.SearchProblem)...)
	_ = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		return tx.SetNetworkHolds(branch.ID, model.ObjectID(plan.Tree), e.now(), network)
	})
	return plan, nil
}

// evidence finds the evidence for the commit's files and applies the
// publication rule, accepted being the failures already accepted at this
// commit.
func (e *Engine) evidence(ctx context.Context, plan *SubmitPlan, accepted []string, standing []model.Decision) error {
	request := plan.Request
	for _, kind := range request.Types {
		if !prdescription.IsType(kind) {
			return fmt.Errorf("--type %q is not one of the template's: %s", kind, strings.Join(prdescription.Types(), ", "))
		}
	}
	if request.NoCheck {
		if len(request.Accept) > 0 {
			return errors.New("--accept requires a failed check and cannot be combined with --no-check")
		}
		if request.Standing {
			return errors.New("--standing accepts a failure a check found, and cannot be combined with --no-check")
		}
		return nil
	}
	evidence, found, err := e.EvidenceFor(ctx, plan.Branch.ID, model.ObjectID(plan.Tree))
	if err != nil {
		return err
	}
	if !found {
		if request.PendingCheck {
			plan.CheckNeeded = true
			return nil
		}
		if request.Draft {
			return nil
		}
		// A check of these files still to finish is the one to wait for,
		// not a new one to run.
		var pending, passed []model.Run
		if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			var err error
			if pending, err = runsOfTree(r, plan.Branch.ID, model.ObjectID(plan.Tree), model.RunQueued, model.RunRunning); err != nil {
				return err
			}
			passed, err = r.Runs(store.RunFilter{Branch: plan.Branch.ID, States: []model.RunState{model.RunPassed}, Limit: 1})
			return err
		}); err != nil {
			return err
		}
		if len(pending) > 0 {
			run := pending[0]
			plan.block(branchstate.Block{Kind: branchstate.BlockCheckPending, Run: run.Name(), State: run.State})
			return nil
		}
		// A check passed of other files, as before a rebase, is reused
		// where a port reads the same files: the check needed is quick, and
		// is said so, since a rebase's refusal read as a whole build again
		// (the rc6 full stage, B5).
		if len(passed) > 0 {
			plan.block(branchstate.Block{Kind: branchstate.BlockCheckOfOtherFiles, Run: passed[0].Name()})
			return nil
		}
		plan.block(branchstate.Block{Kind: branchstate.BlockNoCheck})
		return nil
	}
	// ship runs the check these files still need, where an
	// earlier one built them only somewhere: running it is the decision,
	// and the check reuses what stands. Refusing for the earlier check's
	// gaps left --check doing nothing (field testing, grafana 13.2.4:
	// check-323 passed macOS 12 alone, and macOS 26 was required). A
	// failure stands, and is refused as before.
	if request.PendingCheck && evidence.Unfinished() {
		plan.CheckNeeded = true
		return nil
	}
	// What stands may still not be the check asked, --build or --tests
	// another's (Codex's review, medium 4).
	if request.PendingCheck && (request.Where.explicit() || request.Tests != "") {
		covered, err := e.coversAsked(ctx, plan.Branch, evidence, request.Where, request.Tests)
		if err != nil {
			return err
		}
		if !covered {
			plan.CheckNeeded = true
			return nil
		}
	}
	plan.Evidence = &evidence
	plan.UncoveredCI = uncoveredCI(e.ciReleases(ctx, plan.Tree), evidence.Plan.Environments)
	if plan.AtBase, err = e.failsAtBase(ctx, plan.Branch.ID, evidence); err != nil {
		return err
	}
	if request.Standing && len(request.Accept) == 0 {
		return errors.New("--standing takes the ports --accept names: dockhand submit --accept <port> --standing")
	}
	if err := branchstate.AcceptanceProblem(evidence, request.Accept, plan.AtBase, request.Standing); err != nil {
		return err
	}
	// The standing decisions asked for count here, as those recorded do,
	// and are recorded with the submission.
	if request.Standing {
		for _, port := range request.Accept {
			i := slices.IndexFunc(evidence.Targets, func(t TargetEvidence) bool { return t.Target.Target.Name == port && t.Failing() })
			failures, _ := branchstate.EnvironmentFailures(evidence, evidence.Targets[i])
			for _, failure := range failures {
				if !slices.ContainsFunc(plan.Standing, func(d model.Decision) bool {
					return d.Subject == failure.Decision.Subject && d.Evidence == failure.Decision.Evidence
				}) {
					plan.Standing = append(plan.Standing, failure.Decision)
				}
			}
		}
		standing = append(slices.Clone(standing), plan.Standing...)
	}
	plan.StandingAccepted = branchstate.StandingAccepted(evidence, standing)
	plan.Accepted = branchstate.AcceptedReasons(evidence, slices.Concat(accepted, request.Accept), plan.AtBase, plan.StandingAccepted)
	if !request.Draft {
		for _, block := range branchstate.PublicationBlocks(evidence, slices.Concat(accepted, request.Accept), plan.AtBase, plan.StandingAccepted) {
			plan.block(block)
		}
	}
	return nil
}

// destination finds where submit pushes: your fork, or for a pull request
// someone else opened, their branch, when GitHub lets you push to it; the
// pull request already open for the branch; and the fork's branch as it
// stands.
func (e *Engine) destination(ctx context.Context, worktree *git.Repository, plan *SubmitPlan) error {
	f := e.forge()
	var err error
	if plan.Login, err = f.Login(ctx); err != nil {
		return loginError("submit", err)
	}
	login := plan.Login.Account
	remotes, err := worktree.Remotes(ctx)
	if err != nil {
		return err
	}
	remoteName := ""
	if theirs := theirRepository(plan.Branch, login); theirs != "" {
		plan.HeadRepository, plan.Theirs = theirs, true
		if remoteName, plan.PushURL, err = e.theirRemote(ctx, remotes, theirs); err != nil {
			return err
		}
	} else {
		fork, err := e.fork(ctx, remotes, login, plan.Request.Remote)
		// No fork to push to holds the submission, said with the rest of
		// its plan, which pushes nothing: submit --plan refused at once.
		if missing := (*noForkError)(nil); errors.As(err, &missing) {
			plan.NoFork = true
			plan.block(branchstate.Block{Kind: branchstate.BlockNoFork, NoFork: err.Error()})
			return nil
		}
		if err != nil {
			return err
		}
		plan.HeadRepository, remoteName, plan.PushURL = fork.Repository, fork.Remote, fork.PushURL
	}
	plan.RemoteHead, err = worktree.RemoteHead(ctx, plan.PushURL, plan.RemoteBranch())
	// A fork that refuses this Mac's key, or asks credentials it hasn't,
	// holds the submission, said with the rest of its plan, which pushes
	// nothing: D-N5's preview stopped there, its moved tag unsaid (rc1's
	// full stage).
	if errors.Is(err, git.ErrSSHRefused) || errors.Is(err, git.ErrNoHTTPSCredentials) {
		plan.Unreached = true
		plan.block(branchstate.Block{Kind: branchstate.BlockForkUnreached, Unreached: err.Error()})
		err = nil
	}
	if err != nil {
		return err
	}
	if plan.RemoteHead.Exists && plan.RemoteHead.Object != plan.Commit {
		above, err := worktree.IsAncestor(ctx, plan.RemoteHead.Object, plan.Commit)
		plan.Replaces = err != nil || !above
	}

	var observed forge.PullRequestObservation
	if pr := plan.Branch.PullRequest; pr != nil {
		observed, err = f.Observe(ctx, pullRequestRef(pr.Repository, pr.Number))
	} else {
		observed, err = f.Find(ctx, forge.PullRequestQuery{Repository: e.PullRequestRepository(), HeadRepository: plan.HeadRepository, HeadBranch: plan.RemoteBranch(), BaseBranch: UpstreamBranch})
	}
	if err != nil {
		return err
	}
	if !observed.Found {
		return nil
	}
	pr := observed.PullRequest
	if pr.State != forge.PullRequestOpen {
		// One found by the head branch's name alone, closed or merged, is
		// an earlier branch's of the same name, as update --new names every
		// update of a port to a version alike: rc3's closed test pull
		// request dead-ended rc5's update of go-reflex to the same version,
		// with "start a new branch", which names it the same (the rc5 full
		// stage, A4). A new pull request opens.
		if plan.Branch.PullRequest == nil {
			plan.Earlier = &pr
			return nil
		}
		return fmt.Errorf("#%d is %s; start a new branch for further work (dockhand start)", pr.Ref.Number, pr.State)
	}
	plan.Existing = &observed
	if theirRepository(plan.Branch, login) != "" {
		if err := e.mayPushTheirs(ctx, plan, login, pr); err != nil {
			return err
		}
	}
	// A commit built from what someone else pushed goes, leased to their
	// commit: one on top of it, which keeps it beneath, or one made from it,
	// as theirs amended in turn, which MacPorts' one commit a change asks
	// for, where the branch's reflog shows it at their commit
	// (py-coremltools, #35220, the Vx language port session at 7a38bfff
	// and a0470412) and the commit is theirs amended: on their parent, or
	// with their changes in its tree. A reflog that once saw their commit
	// isn't enough: pulled, reset away, and replaced, it was overwritten
	// (the rc4 full stage, D-S1). One over a commit the branch was never
	// at holds.
	builtOn := plan.RemoteHead.Exists && model.ObjectID(plan.RemoteHead.Object) == pr.RemoteHead && !plan.Replaces
	if !builtOn && plan.RemoteHead.Exists && model.ObjectID(plan.RemoteHead.Object) == pr.RemoteHead {
		if builtOn, err = worktree.BranchWasAt(ctx, plan.Branch.Name, string(pr.RemoteHead)); err != nil {
			return err
		}
		if builtOn {
			if builtOn, err = worktree.Amends(ctx, plan.Commit, string(pr.RemoteHead)); err != nil {
				return err
			}
		}
		if last := plan.Branch.PullRequest; builtOn && last != nil && last.Pushed != "" && pr.RemoteHead != last.Pushed {
			plan.Amended = pr.RemoteHead
		}
	}
	if last := plan.Branch.PullRequest; last != nil && last.Pushed != "" && pr.RemoteHead != last.Pushed && string(pr.RemoteHead) != plan.Commit && !builtOn {
		plan.OthersPushed = pr.RemoteHead
		plan.block(branchstate.Block{Kind: branchstate.BlockOthersPushed, Pushed: &branchstate.Push{Number: pr.Ref.Number, At: pr.RemoteHead, Last: last.Pushed, Remote: remoteName, Branch: plan.RemoteBranch()}})
	}
	return nil
}

// theirRepository is the head repository of a branch's pull request when
// someone other than login opened it from their own repository; empty
// for your own.
func theirRepository(branch model.Branch, login string) string {
	pr := branch.PullRequest
	if pr == nil {
		return ""
	}
	repository, _ := pr.HeadParts()
	owner, _, _ := strings.Cut(repository, "/")
	if repository == "" || strings.EqualFold(owner, login) {
		return ""
	}
	return repository
}

// theirRemote is how to push to someone's repository: a remote that
// already pushes there, else their repository's GitHub address in the
// form your remotes use.
func (e *Engine) theirRemote(ctx context.Context, remotes []git.Remote, repository string) (string, string, error) {
	ssh := false
	for _, remote := range remotes {
		if name, err := e.forge().NameFromRemote(remote.PushURL); err == nil && strings.EqualFold(name, repository) {
			return remote.Name, remote.PushURL, nil
		}
		ssh = ssh || strings.HasPrefix(remote.PushURL, "git@github.com:") || strings.HasPrefix(remote.PushURL, "ssh://")
	}
	address := github.Remote(repository, ssh)
	return address, address, nil
}

// mayPushTheirs checks that GitHub lets you push to someone's pull
// request: they allow maintainers to edit it, and you have write access
// to MacPorts' repository. Otherwise the plan says what stands in the way.
func (e *Engine) mayPushTheirs(ctx context.Context, plan *SubmitPlan, login string, pr forge.PullRequest) error {
	if !pr.MaintainerCanModify {
		plan.block(branchstate.Block{Kind: branchstate.BlockMaintainerEdits, Pushed: &branchstate.Push{Number: pr.Ref.Number, Author: pr.Author, Head: plan.Head()}})
		return nil
	}
	permission, err := e.forge().Permission(ctx, e.PullRequestRepository(), login)
	if err != nil {
		return fmt.Errorf("reading your access to %s: %w", e.PullRequestRepository(), err)
	}
	if !slices.Contains([]string{"admin", "maintain", "write"}, permission) {
		plan.block(branchstate.Block{Kind: branchstate.BlockNoWriteAccess, Pushed: &branchstate.Push{Number: pr.Ref.Number, Author: pr.Author, Head: plan.Head(), Repository: e.PullRequestRepository(), Permission: permission}})
	}
	return nil
}

// commitsAreOwn refuses a branch whose commits since its base include
// commits master has: its history moved past the base this database
// keeps, as another database on the same checkout can move it, and a pull
// request from it would carry master's commits, and a title from master's
// subject (rc2's full stage, 2026-10-31: submit titled dhtest's pull
// request "lang/sbcl: update to sbcl-2.6.10"); tidy would have folded
// them into the branch's own. Master is the one dockhand last fetched;
// where none was, there's nothing to compare. undone is what wasn't done.
func (e *Engine) commitsAreOwn(ctx context.Context, worktree *git.Repository, branch model.Branch, base, head string, commits int, undone string) error {
	master, ok := e.lastMaster(ctx)
	if !ok {
		return nil
	}
	own, err := worktree.CountCommits(ctx, string(master), head)
	if err != nil {
		return err
	}
	if own >= commits {
		return nil
	}
	return refusal.New(refusal.Attention, "%s's history has %s master already has, past the base %s this database records for it, so nothing was %s: its Git branch was moved elsewhere, as by dockhand on another database", branch.ShortName(), prose.Plural(commits-own, "commit"), model.ObjectID(base).Short(), undone).
		Then(refusal.Next{Verb: refusal.VerbRebase, Branch: branch.ShortName()})
}

// title keeps a title given at any step; otherwise a single commit's
// subject, or the subject of the only port's first commit. Dockhand never
// composes one, and an existing pull request keeps its own.
func (e *Engine) title(plan *SubmitPlan) {
	switch {
	case plan.Request.Title != "":
		plan.Title = plan.Request.Title
	case plan.Existing != nil:
		plan.Title = plan.Existing.PullRequest.Title
	case plan.Branch.Title != "":
		plan.Title = plan.Branch.Title
	case len(plan.Commits) == 1:
		plan.Title = plan.Commits[0].Subject()
	case len(plan.Ports) == 1:
		plan.Title = portTitle(plan.Ports[0], plan.moved[plan.Ports[0]], plan.Commits)
	default:
		plan.Title = updateWithRebuilds(plan.Commits)
	}
	if plan.Title == "" {
		plan.block(branchstate.Block{Kind: branchstate.BlockNoTitle})
	}
}

// portTitle titles a one-port branch of several commits: the port's
// commit whose subject names the version the branch moves it to, else
// its first. abook's "abook: fix formatting" then "abook: update to 0.6.2"
// titled the pull request for the fix (the abook session, 2026-10-28).
func portTitle(port, version string, commits []git.HistoryCommit) string {
	first := ""
	for _, commit := range commits {
		subject := commit.Subject()
		if !strings.HasPrefix(subject, port+":") {
			continue
		}
		if version != "" && slices.Contains(strings.Fields(subject), version) {
			return subject
		}
		if first == "" {
			first = subject
		}
	}
	return first
}

// movedVersions are the versions the change records move each port to.
func movedVersions(records map[string]model.ChangeRecord) map[string]string {
	moved := map[string]string{}
	for _, record := range records {
		for _, port := range record.Ports {
			if version, ok := record.MovedVersion(port.Port); ok {
				moved[port.Port] = version
			}
		}
	}
	return moved
}

// updateWithRebuilds is the subject of the one commit of a branch that
// isn't a rebuild for it, where every other is, as update
// --revbump-dependents makes them: "libunibreak: update to 8.0" beside
// "taisei: rebuild for libunibreak 8.0" titles the pull request. It's
// empty for any other branch (field testing, batch 12).
func updateWithRebuilds(commits []git.HistoryCommit) string {
	var update string
	var rebuilds []string
	for _, commit := range commits {
		subject := commit.Subject()
		if _, rest, ok := strings.Cut(subject, ": rebuild for "); ok {
			rebuilds = append(rebuilds, rest)
			continue
		}
		if update != "" {
			return ""
		}
		update = subject
	}
	port, _, ok := strings.Cut(update, ":")
	if !ok || len(rebuilds) == 0 {
		return ""
	}
	for _, rest := range rebuilds {
		if !strings.HasPrefix(rest, port+" ") {
			return ""
		}
	}
	return update
}

// searchOthers looks for other open pull requests for the same ports.
func (e *Engine) searchOthers(ctx context.Context, plan *SubmitPlan) {
	except := 0
	if plan.Existing != nil {
		except = plan.Existing.PullRequest.Ref.Number
	}
	// The ports the revision's change records say it changes are searched
	// for, terraform-1.16 rather than the terraform its directory is
	// named for (field testing, 2026-10-02); a directory's name where it
	// has no record. Without one, the ports whose source changed narrow
	// it, as before records.
	ports := plan.Ports
	var changed []string
	for _, found := range plan.Upstream {
		// A directory whose ports couldn't be read is recorded by its
		// path, which names no port to search under.
		if !strings.Contains(found.Port, "/") {
			changed = append(changed, found.Port)
		}
	}
	if len(changed) > 0 && !plan.recorded {
		ports = changed
	}
	plan.Searched = ports
	plan.Others, plan.SearchProblem = e.pullRequestsNow(ctx, ports, except)
}

// Submitted is what submit did: the pull request it opened or updated,
// and whether it pushed, and rewrote an existing one's title or
// description.
type Submitted struct {
	PullRequest forge.PullRequest
	Created     bool
	Pushed      bool
	Edited      SubmitEdits
	// Drafted is an open pull request returned to draft before the push.
	Drafted bool
}

// SubmitEdits are what a submission rewrote of an existing pull request:
// its title, its description, or both. A pull request whose description
// is its author's (SubmitPlan.Theirs) is rewritten in neither.
type SubmitEdits struct {
	Title, Body bool
}

// ErrStaleSubmit reports a plan whose branch or fork moved since it was
// made.
var ErrStaleSubmit = errors.New("the branch changed after the submission was prepared")

// ApplySubmit pushes the planned commit to your fork, only while the fork's
// branch is as the plan saw it, and opens or updates the pull request.
func (e *Engine) ApplySubmit(ctx context.Context, plan SubmitPlan, answered model.Answered) (Submitted, error) {
	// The one gate of a submission, whoever submits it: what blocks it,
	// the commit it was bound to, and, where nobody looks, what holds it
	// (branchstate.Publish; R3-10, where ship and serve each held it).
	publication := branchstate.Publication{Branch: plan.Branch.ShortName(), Blocks: plan.Blocks, Holds: undecided(holding(plan.Concerns(), plan.allowed), plan.Decided),
		Commit: model.ObjectID(plan.Commit), Bound: model.ObjectID(plan.Request.Bound)}
	if refusal := branchstate.Publish(publication, branchstate.Mode{Answered: answered}, Vocabulary{}); refusal != nil {
		return Submitted{}, refusal
	}
	// GitHub refuses a body past its limit after the push, so no pull
	// request opens; dockhand's own parts are trimmed under it, and a
	// person's kept part that passes it is said before anything is pushed.
	if !plan.Theirs && !prdescription.Fits(plan.Body) {
		return Submitted{}, fmt.Errorf("the description is %s characters, past GitHub's limit of %s, with what was kept of it as someone's own; shorten it on GitHub, or submit --rewrite-description; nothing was pushed", prose.Count(utf8.RuneCountInString(plan.Body)), prose.Count(prdescription.Limit))
	}
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return Submitted{}, err
	}
	if head, _, err := worktree.Branch(ctx, plan.Branch.Name); err != nil || head != plan.Commit {
		return Submitted{}, fmt.Errorf("%w: %s moved; run dockhand submit again", ErrStaleSubmit, plan.Branch.Name)
	}
	// The title and description it writes are made against them as they
	// are now, before anything is pushed (unchangedSince).
	if err := e.unchangedSince(ctx, &plan); err != nil {
		return Submitted{}, err
	}
	var result Submitted
	// A draft submission returns the pull request to draft before it
	// pushes what may be failing work to it (ToDraft).
	if plan.ToDraft() {
		if err := e.draftFirst(ctx, plan); err != nil {
			return Submitted{}, err
		}
		result.Drafted = true
	}
	if !plan.RemoteHead.Exists || plan.RemoteHead.Object != plan.Commit {
		// A fork's branch that holds the commit already, as a submit
		// racing this one leaves it, is pushed (git.Push).
		if err := worktree.Push(ctx, git.Push{Remote: plan.PushURL, Branch: plan.RemoteBranch(), Commit: plan.Commit, ExpectedRemote: plan.RemoteHead}); err != nil {
			var conflict *git.RefConflict
			if errors.As(err, &conflict) {
				return Submitted{}, fmt.Errorf("%w: %s changed on %s since submit looked; nothing was pushed", ErrStaleSubmit, plan.RemoteBranch(), plan.HeadRepository)
			}
			return Submitted{}, err
		}
		result.Pushed = true
	}
	failpoint.Hit("submit.pushed")
	progress.VerboseReport(ctx, "Opening the pull request for %s", plan.Head())
	input := forge.PullRequestInput{Repository: plan.Repository, BaseBranch: UpstreamBranch, HeadBranch: plan.RemoteBranch(), HeadRepository: plan.HeadRepository,
		Desired: forge.PullRequestContent{Head: model.ObjectID(plan.Commit), Title: plan.Title, Body: plan.Body}, Draft: plan.Request.Draft}
	var observed forge.PullRequestObservation
	if plan.Existing == nil {
		observed, err = e.forge().Create(ctx, input)
		result.Created = true
		if err != nil {
			observed, err = e.createdAnyway(ctx, plan, err)
		}
	} else {
		ref := plan.Existing.PullRequest.Ref
		input.ExistingPR = &ref
		observed = *plan.Existing
		if !plan.Theirs {
			result.Edited = SubmitEdits{Title: plan.Title != plan.Existing.PullRequest.Title, Body: plan.Body != plan.Existing.PullRequest.Body}
		}
		if result.Edited.Title || result.Edited.Body {
			observed, err = e.forge().Update(ctx, input)
		}
	}
	if err != nil {
		return result, fmt.Errorf("pushed %s to %s, but could not create or update the pull request: %w; run dockhand submit again to finish", model.ObjectID(plan.Commit).Short(), plan.Head(), err)
	}
	// The window a kill is worst in: GitHub has the pull request, and the
	// record hasn't it yet (the rc6 full stage, D-I4).
	failpoint.Hit("submit.created")
	result.PullRequest = observed.PullRequest
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		branch, err := tx.Branch(plan.Branch.ID)
		if err != nil {
			return err
		}
		// What dockhand wrote, not a person's edit it kept, is recorded,
		// so the next submit still tells the two apart.
		body := plan.Written
		if body == "" {
			body = plan.Body
		}
		if plan.Theirs {
			body = plan.Existing.PullRequest.Body
		}
		// The draft state asked for: a new pull request's, or one
		// returned to draft; otherwise the one recorded stands.
		draft := plan.Request.Draft
		if branch.PullRequest != nil && plan.Existing != nil && !result.Drafted {
			draft = branch.PullRequest.Draft
		}
		var seen *model.PullRequestObservation
		if branch.PullRequest != nil && branch.PullRequest.Number == observed.PullRequest.Ref.Number {
			seen = branch.PullRequest.Observed
		}
		branch.PullRequest = &model.PullRequest{Repository: plan.Repository, Number: observed.PullRequest.Ref.Number, Head: plan.Head(),
			Pushed: model.ObjectID(plan.Commit), Body: body, Draft: draft, Observed: seen}
		if plan.Request.Title != "" {
			branch.Title = plan.Request.Title
		}
		if plan.Request.Note != nil {
			branch.Note = plan.Note
		}
		if err := tx.UpdateBranch(branch); err != nil {
			return err
		}
		for _, port := range plan.Request.Accept {
			if err := tx.AddDecision(model.Decision{Branch: branch.ID, Finding: model.FindingFailure, Subject: port, Evidence: plan.Commit, Answer: model.Accepted, At: e.now()}); err != nil {
				return err
			}
		}
		for _, decision := range plan.Standing {
			decision.At = e.now()
			if err := tx.AddDecision(decision); err != nil {
				return err
			}
		}
		verb := "updated"
		if result.Created {
			verb = "opened"
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchSubmit, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s #%d with %s", verb, observed.PullRequest.Ref.Number, model.ObjectID(plan.Commit).Short())})
		return err
	})
	if err != nil {
		return result, fmt.Errorf("pull request #%d is open with commit %s, but dockhand could not save its record: %w; run dockhand submit again to find and record it", observed.PullRequest.Ref.Number, model.ObjectID(plan.Commit).Short(), err)
	}
	return result, nil
}

// createdAnyway reads back whether a pull request is open for the fork's
// branch after creating one failed: the reply may have been lost after
// GitHub opened it, or GitHub refused one because a submit racing this
// one opened it first. One found is this branch's, and is not written
// again; with none, the failure stands.
func (e *Engine) createdAnyway(ctx context.Context, plan SubmitPlan, failed error) (forge.PullRequestObservation, error) {
	found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: e.PullRequestRepository(), HeadRepository: plan.HeadRepository, HeadBranch: plan.RemoteBranch(), BaseBranch: UpstreamBranch})
	if err != nil || !found.Found || found.PullRequest.State != forge.PullRequestOpen {
		return forge.PullRequestObservation{}, failed
	}
	return found, nil
}

// draftFirst returns a submission's open pull request to draft. Where an
// organization refuses dockhand's app, the GitHub CLI does it when it's
// signed in as the account dockhand is, as Ready's does; otherwise nothing
// is pushed, and the refusal names the command that does it.
func (e *Engine) draftFirst(ctx context.Context, plan SubmitPlan) error {
	ref := plan.Existing.PullRequest.Ref
	_, err := e.forge().MarkDraft(ctx, ref)
	if err == nil {
		return nil
	}
	why := ""
	if errors.Is(err, forge.ErrAppRestricted) {
		var byCLI bool
		if byCLI, why = e.byCLI(ctx, func() error { return e.GitHubCLI.MarkDraft(ctx, ref) }, "return it to draft"); byCLI {
			return nil
		}
	}
	err = fmt.Errorf("returning #%d to draft: %w\nNothing was pushed. Return it to draft on its page, %s, or with the GitHub CLI: gh pr ready %d --undo --repo %s, then submit --draft again", ref.Number, err, github.PullRequestURL(ref.Repository, ref.Number), ref.Number, ref.Repository)
	if why != "" {
		err = fmt.Errorf("%w\n%s", err, why)
	}
	return err
}

// readyByCLI marks a pull request ready with the GitHub CLI, when it's
// signed in as the account dockhand acts for, since the change is then
// made by the same person through another app; why it didn't, otherwise.
func (e *Engine) readyByCLI(ctx context.Context, ref forge.PullRequestRef) (bool, string) {
	return e.byCLI(ctx, func() error { return e.GitHubCLI.MarkReady(ctx, ref) }, "mark it ready")
}

// byCLI does what the GitHub CLI does for the account dockhand acts for,
// when it's signed in as that account, saying why it didn't otherwise:
// what, as "mark it ready", names it in the reason.
func (e *Engine) byCLI(ctx context.Context, do func() error, what string) (bool, string) {
	if e.GitHubCLI == nil {
		return false, ""
	}
	login, err := e.GitHubCLI.Login(ctx)
	if err != nil {
		return false, fmt.Sprintf("The GitHub CLI wasn't used: %v.", err)
	}
	mine, err := e.forge().AuthenticatedUser(ctx)
	if err != nil {
		return false, fmt.Sprintf("The GitHub CLI wasn't used: dockhand's own account couldn't be read: %v.", err)
	}
	if !strings.EqualFold(login, mine) {
		return false, fmt.Sprintf("The GitHub CLI wasn't used: it's signed in as %s, and dockhand as %s.", login, mine)
	}
	if err := do(); err != nil {
		return false, fmt.Sprintf("The GitHub CLI couldn't %s either: %v.", what, err)
	}
	return true, ""
}

// Ready takes a branch's draft pull request out of draft, so it is ready
// for review (Design v3 §9's submit --ready). Where an organization
// refuses dockhand's app, the GitHub CLI does it when it's signed in as
// the account dockhand is (D8), and byCLI says so.
func (e *Engine) Ready(ctx context.Context, branch model.Branch) (_ model.Branch, byCLI bool, err error) {
	branch, err = e.Branch(ctx, branch.ID)
	if err != nil {
		return branch, false, err
	}
	pr := branch.PullRequest
	if pr == nil {
		return branch, false, fmt.Errorf("%s has no pull request yet; dockhand submit opens one", branch.Name)
	}
	ref := pullRequestRef(pr.Repository, pr.Number)
	if _, err := e.forge().MarkReady(ctx, ref); err != nil {
		// GitHub may refuse dockhand's app what it allows another, as an
		// organization that restricts which apps may act for its members
		// does (the hugo exercise's sshuttle run, finding 2).
		var why string
		if errors.Is(err, forge.ErrAppRestricted) {
			byCLI, why = e.readyByCLI(ctx, ref)
		}
		if !byCLI {
			err = fmt.Errorf("marking #%d ready for review: %w\nIt's still a draft. Mark it ready on its page, %s, or with the GitHub CLI, which signs in as its own app: gh pr ready %d --repo %s", pr.Number, err, github.PullRequestURL(pr.Repository, pr.Number), pr.Number, pr.Repository)
			if why != "" {
				err = fmt.Errorf("%w\n%s", err, why)
			}
			return branch, false, err
		}
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		current, err := tx.Branch(branch.ID)
		if err != nil {
			return err
		}
		current.PullRequest.Draft = false
		if current.PullRequest.Observed != nil {
			current.PullRequest.Observed.Draft = false
		}
		if err := tx.UpdateBranch(current); err != nil {
			return err
		}
		branch = current
		message := fmt.Sprintf("marked #%d ready for review", pr.Number)
		if byCLI {
			message += " with the GitHub CLI"
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchReady, Level: model.LevelInfo, Message: message})
		return err
	})
	return branch, byCLI, err
}

// RequestReview asks the reviewers who requested changes on a branch's
// pull request, as GitHub last reported them, to review it again.
func (e *Engine) RequestReview(ctx context.Context, branch model.Branch) ([]string, error) {
	pr := branch.PullRequest
	if pr == nil || pr.Observed == nil || len(pr.Observed.ChangesRequestedBy) == 0 {
		return nil, nil
	}
	logins := pr.Observed.ChangesRequestedBy
	if err := e.forge().RequestReviewers(ctx, pullRequestRef(pr.Repository, pr.Number), logins); err != nil {
		return nil, fmt.Errorf("asking %s to review #%d again: %w", strings.Join(logins, ", "), pr.Number, err)
	}
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchRerequest, Level: model.LevelInfo,
			Message: fmt.Sprintf("asked %s to review #%d again", "@"+strings.Join(logins, ", @"), pr.Number)})
		return err
	})
	return logins, err
}

// hasForkRemote says whether any Git remote pushes to a GitHub repository
// other than MacPorts' own, as a fork or a sandbox would, by its address
// alone: a Next: line suggests a submit only
// where one could push (the M1's rerun, D-S6). A remote that can't be read
// counts as one, so nothing is withheld on a guess.
func (e *Engine) hasForkRemote(ctx context.Context) bool {
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return true
	}
	for _, remote := range remotes {
		name, err := github.RemoteRepository(remote.PushURL)
		if err == nil && !strings.EqualFold(name, UpstreamRepository) {
			return true
		}
	}
	return false
}

// loginError says why asking GitHub who you are failed: a rate limit is
// one to wait out, never a login wanting (field testing, batch 11: every
// submit after a drain said it needed a login, and the limit had run out).
//
// Only a login missing or refused is said as one: a request that never
// got an answer, as in GitHub's outage, is said as GitHub out of reach
// (field testing, batch 12: yank's http2 timeout read as a login wanting).
func loginError(what string, err error) error {
	if limited := (*forge.RateLimitError)(nil); errors.As(err, &limited) {
		return fmt.Errorf("%s waits on GitHub: %w", what, err)
	}
	if errors.Is(err, forge.ErrAuthentication) {
		return fmt.Errorf("%s needs your GitHub login: %w", what, err)
	}
	return fmt.Errorf("%s couldn't ask GitHub who you are: %w", what, err)
}

// ForkRefuses says why your fork refuses this Mac's push, asked once with
// git ls-remote of its push URL before a batch that pushes starts its
// checks: for want of an SSH key loaded or HTTPS credentials, which a
// batch met only after checks that passed and pushes that failed, after a
// reboot (rc1's bump batch). nil where the fork answers, or can't be found
// or asked for another reason, which submit says as now.
func (e *Engine) ForkRefuses(ctx context.Context) error {
	fork, err := e.Fork(ctx, "")
	if err != nil || fork.PushURL == "" {
		return nil
	}
	_, err = e.Repo.RemoteHead(ctx, fork.PushURL, UpstreamBranch)
	if errors.Is(err, git.ErrSSHRefused) || errors.Is(err, git.ErrNoHTTPSCredentials) {
		return fmt.Errorf("your fork %s can't be pushed to from this Mac, so nothing was shipped: %w", fork.Repository, err)
	}
	return nil
}

// Fork finds your fork: the one Git remote that pushes to a fork of
// MacPorts' repository your GitHub login owns, or the remote named. With
// a sandbox (Options.PullRequests), the fork is the sandbox itself, which
// must be a fork of MacPorts' repository: its pull requests are within it.
func (e *Engine) Fork(ctx context.Context, remote string) (buildenv.Fork, error) {
	login, err := e.forge().AuthenticatedUser(ctx)
	if err != nil {
		return buildenv.Fork{}, loginError("finding your fork", err)
	}
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return buildenv.Fork{}, err
	}
	return e.fork(ctx, remotes, login, remote)
}

// noForkError is no remote found that pushes to the fork, or to the one
// named: the submission's to hold, its words the way on.
type noForkError struct{ words string }

func (n *noForkError) Error() string { return n.words }

func (e *Engine) fork(ctx context.Context, remotes []git.Remote, login, named string) (buildenv.Fork, error) {
	f := e.forge()
	sandbox := e.Sandboxed()
	var candidates []string
	var fork buildenv.Fork
	for _, remote := range remotes {
		name, err := f.NameFromRemote(remote.PushURL)
		if err != nil || strings.EqualFold(name, UpstreamRepository) {
			continue
		}
		owner, _, _ := strings.Cut(name, "/")
		mine := named == "" && strings.EqualFold(owner, login)
		if sandbox {
			mine = named == "" && strings.EqualFold(name, e.PullRequestRepository())
		}
		if named != "" && remote.Name == named || mine {
			candidates = append(candidates, remote.Name+" ("+name+")")
			fork = buildenv.Fork{Repository: name, Remote: remote.Name, PushURL: remote.PushURL}
		}
	}
	switch {
	case len(candidates) == 0 && named != "":
		return buildenv.Fork{}, &noForkError{fmt.Sprintf("there is no remote %s that pushes to a GitHub repository other than %s", named, UpstreamRepository)}
	case len(candidates) == 0 && sandbox:
		return buildenv.Fork{}, &noForkError{fmt.Sprintf("no Git remote pushes to the sandbox %s that pull requests go to", e.PullRequestRepository())}
	case len(candidates) == 0:
		return buildenv.Fork{}, &noForkError{fmt.Sprintf("no Git remote pushes to a fork of %s that %s owns; fork it on GitHub, then git remote add fork https://github.com/%s/macports-ports.git", UpstreamRepository, login, login)}
	case len(candidates) > 1:
		return buildenv.Fork{}, fmt.Errorf("several remotes push to your forks: %s; choose one with --remote", strings.Join(candidates, ", "))
	}
	if sandbox && !strings.EqualFold(fork.Repository, e.PullRequestRepository()) {
		return buildenv.Fork{}, fmt.Errorf("%s isn't the sandbox %s that pull requests go to", fork.Repository, e.PullRequestRepository())
	}
	info, err := f.RepositoryInfo(ctx, fork.Repository)
	var moved *forge.RepositoryMovedError
	if errors.As(err, &moved) {
		// Refused, not followed: the remote names the repository dockhand
		// pushes to, and a person points it at the new one (the trust rule;
		// the rc8 full stage's F3).
		url := moved.CloneURL
		if strings.HasPrefix(fork.PushURL, "git@") || strings.HasPrefix(fork.PushURL, "ssh://") {
			url = "git@github.com:" + moved.To + ".git"
		}
		return buildenv.Fork{}, fmt.Errorf("%s is now %s on GitHub, renamed or transferred, and remote %s still names the old one; point it at the new one: git remote set-url %s %s", moved.From, moved.To, fork.Remote, fork.Remote, url)
	}
	if err != nil {
		return buildenv.Fork{}, err
	}
	if !strings.EqualFold(info.Parent, UpstreamRepository) {
		return buildenv.Fork{}, fmt.Errorf("%s is not a fork of %s; dockhand pushes only to your fork", fork.Repository, UpstreamRepository)
	}
	return fork, nil
}

// newPorts are the ports a branch adds, each a Portfile its base doesn't
// have, as the submitted files evaluate them, a moved one aside, which the
// base has at another directory. One that can't be evaluated is left out:
// the description says less, and nothing more is wrong.
func (e *Engine) newPorts(ctx context.Context, worktree *git.Repository, source model.Source, baseTree string, changed []string, scope macports.Scope) []NewPort {
	reader, err := e.portReader()
	if err != nil {
		return nil
	}
	var ports []NewPort
	for _, path := range changed {
		directory, ok := macports.PortDirectoryOf(path)
		if !ok || path != directory+"/Portfile" || scope.MovedFrom(directory) != directory {
			continue
		}
		if before, _, err := worktree.File(ctx, baseTree, path); err != nil || before.Exists {
			continue
		}
		evaluated, err := reader.Ports(ctx, source, directory, model.Environment{}, nil)
		if err != nil || len(evaluated) == 0 {
			continue
		}
		port := evaluated[0]
		ports = append(ports, NewPort{Name: port.Name, Version: port.Version, Description: port.Description(),
			Homepage: port.Options["homepage"], License: macports.LicenseWords(port.Options["license"])})
	}
	return ports
}

// coversAsked reports whether the finished check evidence judges stands for
// a check asked where and with tests: its plan, made of the same revision,
// is what Covered compares with.
func (e *Engine) coversAsked(ctx context.Context, branch model.Branch, evidence Evidence, place WhereRequest, tests model.TestPolicy) (bool, error) {
	where, err := e.Where(ctx, place)
	if err != nil {
		return false, err
	}
	revision, err := e.Revision(ctx, evidence.Run.Revision)
	if err != nil {
		return false, err
	}
	want, err := e.PlanCheck(ctx, PlanRequest{Revision: revision, Environments: where.Environments, Span: where.Span, Profile: where.Profile, Tests: cmp.Or(tests, e.options.Tests)})
	if err != nil {
		return false, err
	}
	_, covered, err := e.Covered(ctx, branch.ID, model.ObjectID(revision.Source.Tree), want)
	return covered, err
}

// block records what stops the submission, of its kind and in its words.
func (p *SubmitPlan) block(b branchstate.Block) {
	p.Blocks = append(p.Blocks, b)
	p.Blocking = append(p.Blocking, b.Words(Vocabulary{}))
}

// PushKind is what a submission's push would do to the fork's branch:
// nothing, for someone else's push there, or nothing new; replace it, add
// to it, or create it.
type PushKind string

const (
	PushOthersPushed PushKind = "others-pushed"
	PushNothingNew   PushKind = "nothing-new"
	PushReplaces     PushKind = "replaces"
	PushAdds         PushKind = "adds"
	PushCreates      PushKind = "creates"
	// PushNoFork is no fork found to push to (SubmitPlan.NoFork).
	PushNoFork PushKind = "no-fork"
	// PushUnreached is a fork this Mac can't push to (SubmitPlan.Unreached).
	PushUnreached PushKind = "unreached"
)

// Push is what the plan's push would do, the one reading of its remote
// head, which submit's preview and its JSON word (part 4, item 2). What a
// block holds isn't said as what the push would do (the rc6 full stage,
// D-S1: "replaces the fork's branch at d2b5022 …").
func (p SubmitPlan) Push() PushKind {
	switch {
	case p.NoFork:
		return PushNoFork
	case p.Unreached:
		return PushUnreached
	case p.OthersPushed != "":
		return PushOthersPushed
	case p.RemoteHead.Exists && p.RemoteHead.Object == p.Commit:
		return PushNothingNew
	case p.Replaces:
		return PushReplaces
	case p.RemoteHead.Exists:
		return PushAdds
	}
	return PushCreates
}

// Uncommitted is a submission of a branch whose edits aren't committed,
// Paths, or that has no commits above master yet. submit commits them as
// tidy would, previewed as tidy previews them, and submits those commits
// (Design v3 §3's chaining, where it refused and named tidy); a submission
// of the committed head alone takes --head.
type Uncommitted struct {
	Branch string
	Paths  []string
}

func (u *Uncommitted) Error() string {
	if len(u.Paths) > 0 {
		return fmt.Sprintf("these edits are not committed: %s. Commit them with dockhand tidy, or submit only what is committed with --head", listPaths(u.Paths))
	}
	return fmt.Sprintf("%s has no commits above master yet; commit your edits with dockhand tidy", u.Branch)
}

// changedPatches are the changed paths that are patches in a port's files
// directory, as MacPorts' patchfiles name them, that the base has too:
// changed under their own names.
func changedPatches(ctx context.Context, worktree *git.Repository, base string, changed []string) []string {
	var patches []string
	for _, file := range changed {
		directory, name := path.Split(file)
		if !strings.HasSuffix(directory, "/files/") || !(strings.HasPrefix(name, "patch") || strings.HasSuffix(name, ".diff") || strings.HasSuffix(name, ".patch")) {
			continue
		}
		if _, _, err := worktree.File(ctx, base, file); err == nil {
			patches = append(patches, file)
		}
	}
	return patches
}
