package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
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
	// NoCheck submits without any check; the description says so.
	NoCheck bool
	// PendingCheck previews a submission whose check is still to run, for
	// submit --check: having none yet does not stop it.
	PendingCheck bool
	// Accept acknowledges failed revision-only targets or extras by port.
	Accept []string
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
	Findings []commitrules.Finding
	// LeftOut are uncommitted files --head leaves out.
	LeftOut []string
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
	// RemoteHead is the fork's branch as it was seen.
	RemoteHead git.RefValue
	// Replaces is true when the push replaces history rather than adding
	// to it.
	Replaces bool
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
	// Blocking is what stops the submission.
	Blocking []string
	// CheckNeeded is true when a PendingCheck plan has no check for its
	// files yet.
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

	facts bodyFacts
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

// Describe replaces the description with one the person wrote, which
// gives their note however they left it.
func (p *SubmitPlan) Describe(body string) {
	p.Body, p.Written, p.BodyKept, p.NoteLeftOut = body, body, true, false
	p.Sections = DescriptionSections{Description: SectionKept, Types: SectionKept, TestedOn: SectionKept}
}

// Head is the fork's head as "owner/repo:branch".
func (p SubmitPlan) Head() string { return p.HeadRepository + ":" + p.RemoteBranch() }

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
			return plan, fmt.Errorf("these edits are not committed: %s. Commit them with dockhand tidy, or submit only what is committed with --head", listPaths(plan.LeftOut))
		}
	}
	if plan.Commits, err = worktree.History(ctx, string(branch.Base), head); err != nil {
		return plan, err
	}
	if len(plan.Commits) == 0 {
		return plan, fmt.Errorf("%s has no commits above master yet; commit your edits with dockhand tidy", branch.Name)
	}
	for _, commit := range plan.Commits {
		if commitmsg.ModifiedBuild(commit.Message) {
			plan.ModifiedBuilds = append(plan.ModifiedBuilds, commit.ID)
		}
	}
	changed, err := worktree.ChangedPaths(ctx, trees[string(branch.Base)], plan.Tree)
	if err != nil {
		return plan, err
	}
	// The ports are the subports the revision's change records say it
	// changes: terraform-1.16, where the directory is terraform's.
	records, err := e.revisionChanges(ctx, branch.ID, branch.Base, model.ObjectID(plan.Tree), true)
	if err != nil {
		return plan, err
	}
	var notes map[string]string
	plan.Ports, notes = recordedPorts(macports.ScopeOf(changed).Ports, records)
	plan.recorded = len(notes) == 0
	newPorts := e.newPorts(ctx, worktree, model.Source{Commit: model.ObjectID(head), Tree: model.ObjectID(plan.Tree), Base: branch.Base}, trees[string(branch.Base)], changed)
	plan.Findings = commitrules.CheckCommits(e.ruleCommits(ctx, model.Source{Commit: branch.Base, Base: branch.Base, Tree: model.ObjectID(trees[string(branch.Base)])}, plan.Commits))
	portfiles, err := portfileFindings(ctx, worktree, trees[string(branch.Base)], plan.Tree, changed)
	if err != nil {
		return plan, err
	}
	plan.Findings = append(plan.Findings, portfiles...)
	for _, commit := range plan.Commits {
		if commit.Merge() {
			plan.Blocking = append(plan.Blocking, fmt.Sprintf("commit %s is a merge; MacPorts asks for a rebase instead", short(model.ObjectID(commit.ID))))
		}
	}

	// What's recorded of the branch at this commit, read once for the
	// phases below: the failures accepted, and the edits dockhand made.
	var accepted []string
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		list, err := r.Acceptances(branch.ID, model.ObjectID(head))
		for _, a := range list {
			accepted = append(accepted, a.Port)
		}
		if err != nil {
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
	if err := e.evidence(ctx, &plan, accepted); err != nil {
		return plan, err
	}
	plan.Moved = e.movedSources(ctx, plan.Evidence)
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
	e.title(&plan)
	plan.UnfoundBuilds, plan.BuildsProblem = e.unfoundBuilds(ctx, plan.Commits)
	if plan.Upstream, err = e.revisionComparisons(ctx, branch, model.ObjectID(plan.Tree), true); err != nil {
		return plan, err
	}
	e.searchOthers(ctx, &plan)
	errorsFound := commitrules.Errors(plan.Findings)
	squashed := !slices.ContainsFunc(plan.Findings, func(f commitrules.Finding) bool { return f.Code == "follow-up" || f.Code == "merge" })
	plan.Moved = append(append(preparedSources(plan.Evidence, edits), plan.Moved...), assessedSources(plan.Evidence, plan.Upstream)...)
	facts := bodyFacts{Commits: plan.Commits, Evidence: plan.Evidence, NoCheck: request.NoCheck, Accepted: slices.Concat(accepted, request.Accept), Types: request.Types,
		Updated:     dockhandUpdate(plan.Commits, edits),
		RulesPassed: !errorsFound, Squashed: squashed, Searched: plan.SearchProblem == "", Others: plan.Others,
		TestedBinaries: request.TestedBinaries, TestedVariants: request.TestedVariants, SkipNotification: request.SkipNotification, NewPorts: newPorts,
		Note: plan.Note, UncoveredCI: plan.UncoveredCI}
	plan.facts = facts
	plan.Answer(request.TestedBinaries, request.TestedVariants)
	return plan, nil
}

// evidence finds the evidence for the commit's files and applies the
// publication rule, accepted being the failures already accepted at this
// commit.
func (e *Engine) evidence(ctx context.Context, plan *SubmitPlan, accepted []string) error {
	request := plan.Request
	for _, kind := range request.Types {
		if !prdescription.IsType(kind) {
			return fmt.Errorf("--type %q is not one of the template's: %s", kind, strings.Join(prdescription.Types(), ", "))
		}
	}
	if request.NoCheck {
		if len(request.Accept) > 0 {
			return errors.New("--accept acknowledges a check's failure, and --no-check has none")
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
		var pending []model.Run
		if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			var err error
			pending, err = runsOfTree(r, plan.Branch.ID, model.ObjectID(plan.Tree), model.RunQueued, model.RunRunning)
			return err
		}); err != nil {
			return err
		}
		if len(pending) > 0 {
			run := pending[0]
			plan.Blocking = append(plan.Blocking, fmt.Sprintf("%s, of this commit's files, is %s; dockhand wait %s, then submit, or submit a draft with --draft", run.Name(), run.State, run.Name()))
			return nil
		}
		plan.Blocking = append(plan.Blocking, "no check has finished for this commit's files; run dockhand check first, submit a draft with --draft, or submit without a check with --no-check, which the pull request states")
		return nil
	}
	plan.Evidence = &evidence
	plan.UncoveredCI = uncoveredCI(e.ciReleases(ctx, plan.Tree), evidence.Plan.Environments)
	if err := acceptanceProblem(evidence, request.Accept); err != nil {
		return err
	}
	if !request.Draft {
		plan.Blocking = append(plan.Blocking, publicationProblems(evidence, slices.Concat(accepted, request.Accept))...)
	}
	return nil
}

// destination finds where submit pushes: your fork, or for a pull request
// someone else opened, their branch, when GitHub lets you push to it; the
// pull request already open for the branch; and the fork's branch as it
// stands.
func (e *Engine) destination(ctx context.Context, worktree *git.Repository, plan *SubmitPlan) error {
	f := e.forge()
	login, err := f.AuthenticatedUser(ctx)
	if err != nil {
		return loginError("submit", err)
	}
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
		if err != nil {
			return err
		}
		plan.HeadRepository, remoteName, plan.PushURL = fork.Repository, fork.Remote, fork.PushURL
	}
	if plan.RemoteHead, err = worktree.RemoteHead(ctx, plan.PushURL, plan.RemoteBranch()); err != nil {
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
	if last := plan.Branch.PullRequest; last != nil && last.Pushed != "" && pr.RemoteHead != last.Pushed && string(pr.RemoteHead) != plan.Commit {
		plan.Blocking = append(plan.Blocking, fmt.Sprintf("someone else pushed to #%d: it is at %s, and dockhand last pushed %s. Fetch it (git fetch %s %s) and compare before submitting again; nothing will be pushed over it",
			pr.Ref.Number, short(pr.RemoteHead), short(last.Pushed), remoteName, plan.RemoteBranch()))
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
		plan.Blocking = append(plan.Blocking, fmt.Sprintf("@%s's #%d doesn't let maintainers push to %s; suggest your changes in a review (dockhand review %d), or ask them to allow edits", pr.Author, pr.Ref.Number, plan.Head(), pr.Ref.Number))
		return nil
	}
	permission, err := e.forge().Permission(ctx, e.PullRequestRepository(), login)
	if err != nil {
		return fmt.Errorf("reading your access to %s: %w", e.PullRequestRepository(), err)
	}
	if !slices.Contains([]string{"admin", "maintain", "write"}, permission) {
		plan.Blocking = append(plan.Blocking, fmt.Sprintf("pushing to @%s's %s needs write access to %s, and you have %s; suggest your changes in a review (dockhand review %d)", pr.Author, plan.Head(), e.PullRequestRepository(), permission, pr.Ref.Number))
	}
	return nil
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
		for _, commit := range plan.Commits {
			if strings.HasPrefix(commit.Subject(), plan.Ports[0]+":") {
				plan.Title = commit.Subject()
				break
			}
		}
	default:
		plan.Title = updateWithRebuilds(plan.Commits)
	}
	if plan.Title == "" {
		plan.Blocking = append(plan.Blocking, "the pull request needs a title, since the branch changes several ports: give one with --title")
	}
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
	plan.Others, plan.SearchProblem = e.openPullRequests(ctx, ports, except)
}

// openPullRequests are the open pull requests for any of the ports, but
// except, each once; or why they couldn't be looked for.
func (e *Engine) openPullRequests(ctx context.Context, ports []string, except int) ([]forge.PullRequestSummary, string) {
	var others []forge.PullRequestSummary
	for _, port := range ports {
		found, err := e.forge().OpenPullRequests(ctx, e.PullRequestRepository(), port)
		if err != nil {
			return nil, err.Error()
		}
		for _, pr := range found {
			if pr.Number != except && !slices.ContainsFunc(others, func(o forge.PullRequestSummary) bool { return o.Number == pr.Number }) {
				others = append(others, pr)
			}
		}
	}
	return others, ""
}

// Submitted is what submit did.
type Submitted struct {
	PullRequest forge.PullRequest
	Created     bool
	Pushed      bool
}

// ErrStaleSubmit reports a plan whose branch or fork moved since it was
// made.
var ErrStaleSubmit = errors.New("the branch changed since submit looked")

// ApplySubmit pushes the planned commit to your fork, only while the fork's
// branch is as the plan saw it, and opens or updates the pull request.
func (e *Engine) ApplySubmit(ctx context.Context, plan SubmitPlan) (Submitted, error) {
	if len(plan.Blocking) > 0 {
		return Submitted{}, fmt.Errorf("can't submit yet: %s", strings.Join(plan.Blocking, "; "))
	}
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return Submitted{}, err
	}
	if head, _, err := worktree.Branch(ctx, plan.Branch.Name); err != nil || head != plan.Commit {
		return Submitted{}, fmt.Errorf("%w: %s moved; run dockhand submit again", ErrStaleSubmit, plan.Branch.Name)
	}
	var result Submitted
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
		if !plan.Theirs && (plan.Title != plan.Existing.PullRequest.Title || plan.Body != plan.Existing.PullRequest.Body) {
			observed, err = e.forge().Update(ctx, input)
		}
	}
	if err != nil {
		return result, fmt.Errorf("pushed %s to %s, but the pull request was not written: %w; dockhand submit again finishes it", short(model.ObjectID(plan.Commit)), plan.Head(), err)
	}
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
		draft := plan.Request.Draft
		if branch.PullRequest != nil && plan.Existing != nil {
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
			if err := tx.AddAcceptance(model.Acceptance{Branch: branch.ID, Commit: model.ObjectID(plan.Commit), Port: port, At: e.now()}); err != nil {
				return err
			}
		}
		verb := "updated"
		if result.Created {
			verb = "opened"
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.submit", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s #%d with %s", verb, observed.PullRequest.Ref.Number, short(model.ObjectID(plan.Commit)))})
		return err
	})
	if err != nil {
		return result, fmt.Errorf("#%d is open with %s, but recording it failed: %w; dockhand submit again finds it and records it", observed.PullRequest.Ref.Number, short(model.ObjectID(plan.Commit)), err)
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

// readyByCLI marks a pull request ready with the GitHub CLI, when it's
// signed in as the account dockhand acts for, since the change is then
// made by the same person through another app; why it didn't, otherwise.
func (e *Engine) readyByCLI(ctx context.Context, ref forge.PullRequestRef) (bool, string) {
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
	if err := e.GitHubCLI.MarkReady(ctx, ref); err != nil {
		return false, fmt.Sprintf("The GitHub CLI couldn't mark it ready either: %v.", err)
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
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.ready", Level: model.LevelInfo, Message: message})
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
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.rerequest", Level: model.LevelInfo,
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
		return buildenv.Fork{}, fmt.Errorf("there is no remote %s that pushes to a GitHub repository other than %s", named, UpstreamRepository)
	case len(candidates) == 0 && sandbox:
		return buildenv.Fork{}, fmt.Errorf("no Git remote pushes to the sandbox %s that pull requests go to", e.PullRequestRepository())
	case len(candidates) == 0:
		return buildenv.Fork{}, fmt.Errorf("no Git remote pushes to a fork of %s that %s owns; fork it on GitHub, then git remote add fork https://github.com/%s/macports-ports.git", UpstreamRepository, login, login)
	case len(candidates) > 1:
		return buildenv.Fork{}, fmt.Errorf("several remotes push to your forks: %s; choose one with --remote", strings.Join(candidates, ", "))
	}
	if sandbox && !strings.EqualFold(fork.Repository, e.PullRequestRepository()) {
		return buildenv.Fork{}, fmt.Errorf("%s isn't the sandbox %s that pull requests go to", fork.Repository, e.PullRequestRepository())
	}
	info, err := f.RepositoryInfo(ctx, fork.Repository)
	if err != nil {
		return buildenv.Fork{}, err
	}
	if !strings.EqualFold(info.Parent, UpstreamRepository) {
		return buildenv.Fork{}, fmt.Errorf("%s is not a fork of %s; dockhand pushes only to your fork", fork.Repository, UpstreamRepository)
	}
	return fork, nil
}

// newPorts are the ports a branch adds, each a Portfile its base doesn't
// have, as the submitted files evaluate them. One that can't be evaluated
// is left out: the description says less, and nothing more is wrong.
func (e *Engine) newPorts(ctx context.Context, worktree *git.Repository, source model.Source, baseTree string, changed []string) []NewPort {
	reader, err := e.portReader()
	if err != nil {
		return nil
	}
	var ports []NewPort
	for _, path := range changed {
		directory, ok := macports.PortDirectoryOf(path)
		if !ok || path != directory+"/Portfile" {
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
