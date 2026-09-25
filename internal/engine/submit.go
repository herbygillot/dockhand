package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
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
}

// SubmitPlan is exactly what submit would do, bound to the branch head
// and the fork's branch head it saw.
type SubmitPlan struct {
	Request  SubmitRequest
	Branch   model.Branch
	Commit   string
	Tree     string
	Commits  []git.HistoryCommit
	Ports    []string
	Findings []commitrules.Finding
	// LeftOut are uncommitted files --head leaves out.
	LeftOut []string

	Repository, HeadRepository, PushURL string
	// RemoteHead is the fork's branch as it was seen.
	RemoteHead git.RefValue
	// Replaces is true when the push replaces history rather than adding
	// to it.
	Replaces bool
	// Existing is the pull request already open for the branch.
	Existing *forge.PullRequestObservation

	Title string
	Body  string
	// BodyKept is true when a person's edits to the description are kept
	// as they are.
	BodyKept bool
	Evidence *Evidence
	Others   []forge.PullRequestSummary
	// SearchProblem says why other pull requests could not be looked for.
	SearchProblem string
	// Blocking is what stops the submission.
	Blocking []string
	// CheckNeeded is true when a PendingCheck plan has no check for its
	// files yet.
	CheckNeeded bool

	facts bodyFacts
}

// Answer records the person's statements for the template's last two
// items, which only they can make, and writes the description again.
func (p *SubmitPlan) Answer(testedBinaries, testedVariants bool) {
	p.Request.TestedBinaries, p.Request.TestedVariants = testedBinaries, testedVariants
	p.facts.TestedBinaries, p.facts.TestedVariants = testedBinaries, testedVariants
	p.Body = pullRequestBody(p.facts)
	if p.Existing != nil {
		last := ""
		if p.Branch.PullRequest != nil {
			last = p.Branch.PullRequest.Body
		}
		var updated bool
		p.Body, updated = mergeBody(p.Existing.PullRequest.Body, last, p.Body)
		p.BodyKept = !updated
	}
}

// Describe replaces the description with one the person wrote.
func (p *SubmitPlan) Describe(body string) {
	p.Body, p.BodyKept = body, true
}

// Head is the fork's head as "owner/repo:branch".
func (p SubmitPlan) Head() string { return p.HeadRepository + ":" + p.Branch.Name }

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
	plan := SubmitPlan{Request: request, Branch: branch, Repository: UpstreamRepository}
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
	changed, err := worktree.ChangedPaths(ctx, trees[string(branch.Base)], plan.Tree)
	if err != nil {
		return plan, err
	}
	scope := ScopeOf(changed)
	plan.Ports = scope.PortNames()
	plan.Findings = commitrules.CheckCommits(ruleCommits(plan.Commits))
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

	if err := e.evidence(ctx, &plan); err != nil {
		return plan, err
	}
	if err := e.destination(ctx, worktree, &plan); err != nil {
		return plan, err
	}
	e.title(&plan)
	e.searchOthers(ctx, &plan)
	errorsFound := commitrules.Errors(plan.Findings)
	squashed := !slices.ContainsFunc(plan.Findings, func(f commitrules.Finding) bool { return f.Code == "follow-up" || f.Code == "merge" })
	var accepted []string
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		list, err := r.Acceptances(branch.ID, model.ObjectID(head))
		for _, a := range list {
			accepted = append(accepted, a.Port)
		}
		return err
	}); err != nil {
		return plan, err
	}
	facts := bodyFacts{Commits: plan.Commits, Evidence: plan.Evidence, NoCheck: request.NoCheck, Accepted: slices.Concat(accepted, request.Accept), Types: request.Types,
		RulesPassed: !errorsFound, Squashed: squashed, Searched: plan.SearchProblem == "", Others: plan.Others,
		TestedBinaries: request.TestedBinaries, TestedVariants: request.TestedVariants, SkipNotification: request.SkipNotification}
	plan.facts = facts
	plan.Answer(request.TestedBinaries, request.TestedVariants)
	return plan, nil
}

// evidence applies the publication rule.
func (e *Engine) evidence(ctx context.Context, plan *SubmitPlan) error {
	request := plan.Request
	for _, kind := range request.Types {
		if !slices.Contains(PullRequestTypes, kind) {
			return fmt.Errorf("--type %q is not one of the template's: %s", kind, strings.Join(PullRequestTypes, ", "))
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
		if !request.Draft {
			plan.Blocking = append(plan.Blocking, "no check has finished for this commit's files; run dockhand check first, submit a draft with --draft, or submit without a check with --no-check, which the pull request states")
		}
		return nil
	}
	plan.Evidence = &evidence
	for _, port := range request.Accept {
		i := slices.IndexFunc(evidence.Targets, func(t TargetEvidence) bool { return t.Target.Target.Name == port })
		switch {
		case i < 0:
			return fmt.Errorf("--accept %s: %s checked no port %s", port, evidence.Run.Name(), port)
		case evidence.Targets[i].Passed:
			return fmt.Errorf("--accept %s: it passed in %s; there is nothing to accept", port, evidence.Run.Name())
		case !Acceptable(evidence.Targets[i].Target):
			return fmt.Errorf("--accept %s: %s is a changed port, and a changed port that fails is shared as a draft (--draft), never accepted", port, port)
		}
	}
	if !request.Draft {
		var accepted []string
		if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			list, err := r.Acceptances(plan.Branch.ID, model.ObjectID(plan.Commit))
			for _, a := range list {
				accepted = append(accepted, a.Port)
			}
			return err
		}); err != nil {
			return err
		}
		plan.Blocking = append(plan.Blocking, publicationProblems(evidence, slices.Concat(accepted, request.Accept))...)
	}
	return nil
}

// destination finds your fork, the pull request already open for the
// branch, and the fork's branch as it stands.
func (e *Engine) destination(ctx context.Context, worktree *git.Repository, plan *SubmitPlan) error {
	f := e.forge()
	login, err := f.AuthenticatedUser(ctx)
	if err != nil {
		return fmt.Errorf("submit needs your GitHub login: %w", err)
	}
	remotes, err := worktree.Remotes(ctx)
	if err != nil {
		return err
	}
	var candidates []string
	var chosen git.Remote
	for _, remote := range remotes {
		name, err := f.NameFromRemote(remote.PushURL)
		if err != nil || strings.EqualFold(name, UpstreamRepository) {
			continue
		}
		owner, _, _ := strings.Cut(name, "/")
		if plan.Request.Remote != "" && remote.Name == plan.Request.Remote || plan.Request.Remote == "" && strings.EqualFold(owner, login) {
			candidates = append(candidates, remote.Name+" ("+name+")")
			chosen, plan.HeadRepository = remote, name
		}
	}
	switch {
	case len(candidates) == 0 && plan.Request.Remote != "":
		return fmt.Errorf("there is no remote %s that pushes to a GitHub repository other than %s", plan.Request.Remote, UpstreamRepository)
	case len(candidates) == 0:
		return fmt.Errorf("no Git remote pushes to a fork of %s that %s owns; fork it on GitHub, then git remote add fork https://github.com/%s/macports-ports.git", UpstreamRepository, login, login)
	case len(candidates) > 1:
		return fmt.Errorf("several remotes push to your forks: %s; choose one with --remote", strings.Join(candidates, ", "))
	}
	info, err := f.RepositoryInfo(ctx, plan.HeadRepository)
	if err != nil {
		return err
	}
	if !strings.EqualFold(info.Parent, UpstreamRepository) {
		return fmt.Errorf("%s is not a fork of %s; dockhand pushes only to your fork", plan.HeadRepository, UpstreamRepository)
	}
	plan.PushURL = chosen.PushURL
	if plan.RemoteHead, err = worktree.RemoteHead(ctx, plan.PushURL, plan.Branch.Name); err != nil {
		return err
	}
	if plan.RemoteHead.Exists && plan.RemoteHead.Object != plan.Commit {
		above, err := worktree.IsAncestor(ctx, plan.RemoteHead.Object, plan.Commit)
		plan.Replaces = err != nil || !above
	}

	var observed forge.PullRequestObservation
	if pr := plan.Branch.PullRequest; pr != nil {
		observed, err = f.Observe(ctx, record.PullRequestRef{Forge: forge.GitHub, Repository: pr.Repository, Number: pr.Number})
	} else {
		observed, err = f.Find(ctx, forge.PullRequestQuery{Repository: UpstreamRepository, HeadRepository: plan.HeadRepository, HeadBranch: plan.Branch.Name, BaseBranch: UpstreamBranch})
	}
	if err != nil {
		return err
	}
	if !observed.Found {
		return nil
	}
	pr := observed.PullRequest
	if pr.State != record.PullRequestOpen {
		return fmt.Errorf("#%d is %s; start a new branch for further work (dockhand start)", pr.Ref.Number, pr.State)
	}
	plan.Existing = &observed
	if last := plan.Branch.PullRequest; last != nil && last.Pushed != "" && pr.RemoteHead != last.Pushed && string(pr.RemoteHead) != plan.Commit {
		plan.Blocking = append(plan.Blocking, fmt.Sprintf("someone else pushed to #%d: it is at %s, and dockhand last pushed %s. Fetch it (git fetch %s %s) and compare before submitting again; nothing will be pushed over it",
			pr.Ref.Number, short(pr.RemoteHead), short(last.Pushed), chosen.Name, plan.Branch.Name))
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
	}
	if plan.Title == "" {
		plan.Blocking = append(plan.Blocking, "the pull request needs a title, since the branch changes several ports: give one with --title")
	}
}

// searchOthers looks for other open pull requests for the same ports.
func (e *Engine) searchOthers(ctx context.Context, plan *SubmitPlan) {
	for _, port := range plan.Ports {
		found, err := e.forge().OpenPullRequests(ctx, UpstreamRepository, port)
		if err != nil {
			plan.SearchProblem = err.Error()
			return
		}
		for _, pr := range found {
			if plan.Existing != nil && pr.Number == plan.Existing.PullRequest.Ref.Number {
				continue
			}
			if !slices.ContainsFunc(plan.Others, func(o forge.PullRequestSummary) bool { return o.Number == pr.Number }) {
				plan.Others = append(plan.Others, pr)
			}
		}
	}
}

// Submitted is what submit did.
type Submitted struct {
	PullRequest record.PullRequest
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
		if err := worktree.Push(ctx, git.Push{Remote: plan.PushURL, Branch: plan.Branch.Name, Commit: plan.Commit, ExpectedRemote: plan.RemoteHead}); err != nil {
			var conflict *git.RefConflict
			if errors.As(err, &conflict) {
				return Submitted{}, fmt.Errorf("%w: %s changed on %s since submit looked; nothing was pushed", ErrStaleSubmit, plan.Branch.Name, plan.HeadRepository)
			}
			return Submitted{}, err
		}
		result.Pushed = true
	}
	input := forge.PullRequestInput{Repository: plan.Repository, BaseBranch: UpstreamBranch, HeadBranch: plan.Branch.Name, HeadRepository: plan.HeadRepository,
		Desired: record.PublicationContent{Head: record.ObjectID(plan.Commit), Title: plan.Title, Body: plan.Body}, Draft: plan.Request.Draft}
	var observed forge.PullRequestObservation
	if plan.Existing == nil {
		observed, err = e.forge().Create(ctx, input)
		result.Created = true
	} else {
		ref := plan.Existing.PullRequest.Ref
		input.ExistingPR = &ref
		observed = *plan.Existing
		if plan.Title != plan.Existing.PullRequest.Title || plan.Body != plan.Existing.PullRequest.Body {
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
		draft := plan.Request.Draft
		if branch.PullRequest != nil && plan.Existing != nil {
			draft = branch.PullRequest.Draft
		}
		var seen *model.PullRequestObservation
		if branch.PullRequest != nil && branch.PullRequest.Number == observed.PullRequest.Ref.Number {
			seen = branch.PullRequest.Observed
		}
		branch.PullRequest = &model.PullRequest{Repository: plan.Repository, Number: observed.PullRequest.Ref.Number, Head: plan.Head(),
			Pushed: model.ObjectID(plan.Commit), Body: plan.Body, Draft: draft, Observed: seen}
		if plan.Request.Title != "" {
			branch.Title = plan.Request.Title
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
	return result, err
}

// Ready takes a branch's draft pull request out of draft, so it is ready
// for review (Design v3 §9's submit --ready).
func (e *Engine) Ready(ctx context.Context, branch model.Branch) (model.Branch, error) {
	branch, err := e.Branch(ctx, branch.ID)
	if err != nil {
		return branch, err
	}
	pr := branch.PullRequest
	if pr == nil {
		return branch, fmt.Errorf("%s has no pull request yet; dockhand submit opens one", branch.Name)
	}
	if _, err := e.forge().MarkReady(ctx, record.PullRequestRef{Forge: forge.GitHub, Repository: pr.Repository, Number: pr.Number}); err != nil {
		return branch, fmt.Errorf("marking #%d ready for review: %w", pr.Number, err)
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
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.ready", Level: model.LevelInfo,
			Message: fmt.Sprintf("marked #%d ready for review", pr.Number)})
		return err
	})
	return branch, err
}
