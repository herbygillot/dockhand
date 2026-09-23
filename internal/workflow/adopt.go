package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// AdoptRequest names a branch dockhand did not make, to be tracked as a
// contribution under its own name: one commit above a commit of master,
// changing one port directory. Target names the port when the directory's
// main port is not the one meant; Upstream is master's fetched head, which
// the branch's base must be on, and an empty Upstream skips that check.
type AdoptRequest struct {
	Branch   string
	Target   string
	Upstream record.ObjectID
	Platform record.Platform
	DryRun   bool
	// Squash folds a branch of several commits into one on its master base,
	// with the oldest commit's message, keeping the originals under
	// refs/dockhand/adopted/<branch>.
	Squash bool
	// PullRequest adopts an open pull request instead of a local branch: its
	// head is fetched into a local branch, named as the pull request names
	// it when the head is on the person's own fork and pr/<number> when it
	// is someone else's, and the contribution is recorded with the pull
	// request attached, so publish updates it and sync follows it. A head of
	// several commits is adopted as it is, with its merge base recorded, so
	// that amend --squash can fold it.
	PullRequest *record.PullRequestRef
	// KeepBody records that the pull request's body is its author's: no
	// publication rewrites or appends to it.
	KeepBody bool
}

// AdoptResult is the contribution adoption recorded, or with DryRun would record.

type AdoptResult struct {
	Change      record.Change
	Revision    record.Revision
	PullRequest *record.PullRequest `json:",omitempty"`
	Portfile    string
	// Commits is how many commits the adopted head carries above master;
	// more than one wants amend --squash before publishing.
	Commits int
	Detail  string
}

// AdoptContribution tracks a person's branch as a contribution. It is the way
// in for a Portfile dockhand cannot edit itself: once tracked, verify builds
// it, publish opens its pull request, and amend and rebase revise it.
func (e *Engine) AdoptContribution(ctx context.Context, input AdoptRequest) (AdoptResult, error) {
	var result AdoptResult
	// A dry run reads the records when there are any and writes nothing;
	// with no store there are no records, and nothing is tracked.
	if e == nil || e.State == nil && !input.DryRun || e.Repo == nil || e.Ports == nil || e.Publisher == nil {
		return result, errNoState
	}
	if input.Target != "" && !macports.ValidName(input.Target) {
		return result, fmt.Errorf("%w: %q is not a port name", ErrInvalidRequest, input.Target)
	}
	if e.State != nil {
		if err := e.requireRepository(""); err != nil {
			return result, err
		}
	}
	var pullRequest *record.PullRequest
	if input.PullRequest != nil {
		branch, pr, created, err := e.fetchPullRequestHead(ctx, *input.PullRequest)
		if err != nil {
			return result, err
		}
		input.Branch, pullRequest = branch, pr
		result, err = e.adoptBranch(ctx, input, pullRequest)
		if created && (err != nil || input.DryRun) {
			// A refused or dry-run adoption leaves no branch behind.
			head := git.RefValue{Exists: true, Object: string(pr.RemoteHead)}
			if cleanup := e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch, Expected: head}}); cleanup != nil {
				err = errors.Join(err, cleanup)
			}
		}
		return result, err
	}
	return e.adoptBranch(ctx, input, nil)
}

func (e *Engine) adoptBranch(ctx context.Context, input AdoptRequest, pullRequest *record.PullRequest) (AdoptResult, error) {
	var result AdoptResult
	if !git.ValidBranchName(input.Branch) {
		return result, fmt.Errorf("%w: adopt needs a literal local branch", ErrInvalidRequest)
	}
	var tracked record.Change
	if e.State != nil {
		err := e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
			change, err := r.OpenChangeByBranch(ctx, input.Branch)
			if errors.Is(err, state.ErrNotFound) {
				return nil
			}
			tracked = change
			return err
		})
		if err != nil {
			return result, err
		}
	}
	if tracked.ID != "" {
		return result, fmt.Errorf("%w: branch %s is already tracked as the contribution for %s; verify, publish, and amend select it by that name", ErrInvalidRequest, input.Branch, initiatingNameOf(tracked))
	}
	snapshot, err := changeset.CaptureBranch(ctx, e.Repo, input.Branch)
	if err != nil {
		return result, err
	}
	stacked := false
	parent, err := e.Repo.SingleParent(ctx, string(snapshot.Commit))
	if err != nil {
		return result, fmt.Errorf("%w: %v; dockhand adopts one commit above a commit of master", ErrInvalidRequest, err)
	}
	if input.Upstream != "" {
		onMaster, err := e.Repo.IsAncestor(ctx, parent, string(input.Upstream))
		if err != nil {
			return result, err
		}
		if !onMaster {
			count, err := e.Repo.CountCommits(ctx, string(input.Upstream), string(snapshot.Commit))
			if err != nil {
				return result, err
			}
			if count <= 1 {
				return result, fmt.Errorf("%w: the commit under branch %s is not on master; rebase the branch onto master first", ErrInvalidRequest, input.Branch)
			}
			switch {
			case input.Squash:
				if input.DryRun {
					result.Detail = fmt.Sprintf("Would squash the %d commits of %s into one and track it; nothing recorded", count, input.Branch)
				}
				if snapshot, err = e.squash(ctx, input.Branch, snapshot, string(input.Upstream), count, input.DryRun); err != nil {
					return result, err
				}
			case pullRequest != nil:
				// A pull request's stack is adopted as it stands, on its merge
				// base, so that amend --squash can fold it into the one commit
				// the port wants without anyone touching Git by hand.
				result.Commits = count
				stacked = true
			default:
				return result, fmt.Errorf("%w: branch %s is %d commits above master; dockhand adopts one commit above a commit of master, so squash them first, or adopt --squash", ErrInvalidRequest, input.Branch, count)
			}
		}
	}
	var source record.Source
	var portfile string
	if stacked {
		base, err := e.Repo.MergeBase(ctx, string(input.Upstream), string(snapshot.Commit))
		if err != nil {
			return result, err
		}
		source = snapshot.Source(record.ObjectID(base))
		if portfile, err = e.contributionPortfile(ctx, source); err != nil {
			return result, err
		}
	} else if source, portfile, err = e.Publisher.UntrackedSource(ctx, snapshot.Source("")); err != nil {
		return result, fmt.Errorf("%w; dockhand adopts one commit changing one port directory", err)
	}
	selection := macports.Selection{Selector: portfile}
	if input.Target != "" {
		selection.Selector = input.Target
	}
	targets, _, err := e.bindSnapshot(ctx, source, selection, input.Platform, nil)
	if err != nil {
		return result, err
	}
	if len(targets) != 1 {
		return result, fmt.Errorf("%w: %s resolves to %d targets; name one port", ErrInvalidRequest, selection.Selector, len(targets))
	}
	target := targets[0]
	if path.Dir(target.Portfile) != path.Dir(portfile) {
		return result, fmt.Errorf("%w: %s lives in %s, but the branch changes %s", ErrInvalidRequest, target.Name, path.Dir(target.Portfile), path.Dir(portfile))
	}
	if existing, err := e.SelectContribution(ctx, ContributionSelector{Target: target.Name}); err == nil {
		return result, fmt.Errorf("%w: %s already has an open contribution on branch %s; amend that one, or abandon it first", ErrInvalidRequest, target.Name, existing.Branch)
	} else if !errors.Is(err, state.ErrNotFound) && !errors.Is(err, ErrInvalidRequest) && !(e.State == nil && errors.Is(err, errNoState)) {
		return result, err
	}
	scope, err := e.rebindReleaseScope(ctx, nil, source, input.Platform)
	if err != nil {
		return result, err
	}
	now := e.now()
	change := record.Change{InitiatingTarget: target.Name, ID: record.ChangeID("change_" + rand.Text()), Branch: input.Branch, Targets: []record.Target{target}, Disposition: record.ChangeOpen, CreatedAt: now, KeepBody: input.KeepBody && pullRequest != nil}
	revision := record.Revision{Scope: scope, ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Source: source, CreatedAt: now, Shared: e.sharedFiles(ctx, source)}
	change.CurrentRevision = revision.ID
	if pullRequest != nil {
		pullRequest.ID, pullRequest.ChangeID = record.PullRequestID("pr_"+rand.Text()), change.ID
		result.PullRequest = pullRequest
	}
	result.Change, result.Revision, result.Portfile = change, revision, portfile
	next := fmt.Sprintf("verify %s builds it, publish %s opens its pull request", target.Name, target.Name)
	if pullRequest != nil {
		next = fmt.Sprintf("verify %s builds it, amend %s revises it and updates the pull request", target.Name, target.Name)
		if result.Commits > 1 {
			next = fmt.Sprintf("its %d commits become one with amend %s --squash, which verifies and updates the pull request", result.Commits, target.Name)
		}
	}
	if input.DryRun {
		if result.Detail == "" {
			result.Detail = fmt.Sprintf("Would track %s as the contribution for %s; nothing recorded", input.Branch, target.Name)
			if result.Commits > 1 {
				result.Detail = fmt.Sprintf("Would track %s as the contribution for %s, %d commits above master that amend %s --squash would fold into one; nothing recorded", input.Branch, target.Name, result.Commits, target.Name)
			}
		}
		return result, nil
	}
	err = e.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
		if _, err := tx.OpenChangeByBranch(ctx, input.Branch); err == nil {
			return fmt.Errorf("%w: branch %s was tracked while adopting", ErrStaleRevision, input.Branch)
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if err := tx.PutChange(ctx, change); err != nil {
			return err
		}
		if err := tx.PutRevision(ctx, revision); err != nil {
			return err
		}
		if pullRequest == nil {
			return nil
		}
		if err := tx.PutPullRequest(ctx, *pullRequest); err != nil {
			return err
		}
		change.PullRequestID, change.PublishedRevision = pullRequest.ID, revision.ID
		return tx.PutChange(ctx, change)
	})
	if err != nil {
		return result, err
	}
	result.Change = change
	result.Detail = fmt.Sprintf("Tracking %s as the contribution for %s; %s", input.Branch, target.Name, next)
	return result, nil
}

// fetchPullRequestHead reads an open pull request and brings its head into a
// local branch: the pull request's own branch name when the head is on the
// person's fork, pr/<number> when it is someone else's. A local branch of
// that name already at the head is reused; one at another commit is refused
// rather than moved, since it may be the person's.
func (e *Engine) fetchPullRequestHead(ctx context.Context, ref record.PullRequestRef) (branch string, pr *record.PullRequest, created bool, err error) {
	if e.Publisher == nil || e.Publisher.Forge == nil {
		return "", nil, false, fmt.Errorf("%w: adopting a pull request needs the forge", ErrInvalidRequest)
	}
	forge := e.Publisher.Forge
	observed, err := forge.Observe(ctx, ref)
	if err != nil {
		return "", nil, false, err
	}
	if !observed.Found {
		return "", nil, false, fmt.Errorf("%w: pull request %d on %s was not found", ErrInvalidRequest, ref.Number, ref.Repository)
	}
	observedPR := observed.PullRequest
	pr = &observedPR
	if pr.State != record.PullRequestOpen {
		return "", nil, false, fmt.Errorf("%w: pull request %d on %s is %s", ErrInvalidRequest, ref.Number, ref.Repository, pr.State)
	}
	if !git.ValidBranchName(pr.HeadBranch) || !git.ValidObjectID(string(pr.RemoteHead)) {
		return "", nil, false, fmt.Errorf("%w: pull request %d has no usable head", ErrInvalidRequest, ref.Number)
	}
	login, _ := forge.AuthenticatedUser(ctx)
	owner, _, _ := strings.Cut(pr.HeadRepository, "/")
	branch = fmt.Sprintf("pr/%d", ref.Number)
	if login != "" && strings.EqualFold(owner, login) {
		branch = pr.HeadBranch
	}
	head, err := forge.RepositoryInfo(ctx, pr.HeadRepository)
	if err != nil {
		return "", nil, false, err
	}
	commit, _, err := e.Repo.FetchBranch(ctx, head.CloneURL, pr.HeadBranch)
	if err != nil {
		return "", nil, false, fmt.Errorf("fetching the pull request's head from %s: %w", pr.HeadRepository, err)
	}
	if record.ObjectID(commit) != pr.RemoteHead {
		return "", nil, false, fmt.Errorf("%w: the pull request's head moved while adopting; try again", ErrStaleRevision)
	}
	existing, err := e.Repo.ReadRef(ctx, "refs/heads/"+branch)
	if err != nil {
		return "", nil, false, err
	}
	if existing.Exists && existing.Object != commit {
		return "", nil, false, fmt.Errorf("%w: local branch %s is at %s, not at the pull request's head %s; move or rename it first", ErrInvalidRequest, branch, existing.Object[:12], commit[:12])
	}
	if !existing.Exists {
		if err := e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch, Desired: git.RefValue{Exists: true, Object: commit}}}); err != nil {
			return "", nil, false, err
		}
		created = true
		progress.Report(ctx, "Fetched pull request %d's head into local branch %s", ref.Number, branch)
	}
	return branch, pr, created, nil
}

// contributionPortfile is the one port directory a stacked head changes
// above its base, as the Portfile path; shared files under _resources may
// change beside it, and several port directories are refused.
func (e *Engine) contributionPortfile(ctx context.Context, source record.Source) (string, error) {
	delta, err := changeset.Between(ctx, e.Repo, source.Base, source.Tree)
	if err != nil {
		return "", err
	}
	scope, err := changeset.ScopeOf(delta.Paths)
	if err != nil {
		return "", fmt.Errorf("%w: the pull request %s above its base; dockhand adopts one port directory", ErrInvalidRequest, scopeWords(err))
	}
	return scope.Portfile(), nil
}

// initiatingNameOf is the port a contribution is selected by.
func initiatingNameOf(change record.Change) string {
	if change.InitiatingTarget != "" {
		return change.InitiatingTarget
	}
	if len(change.Targets) > 0 {
		return change.Targets[0].Name
	}
	return string(change.ID)
}

// squash folds a branch's commits above master into one commit on their
// merge base, carrying the branch's tree and the oldest commit's message,
// and moves the branch to it. The originals stay reachable under
// refs/dockhand/adopted/<branch>. A dry run computes the commit without
// moving anything.
func (e *Engine) squash(ctx context.Context, branch string, snapshot changeset.Snapshot, upstream string, count int, dryRun bool) (changeset.Snapshot, error) {
	base, err := e.Repo.MergeBase(ctx, upstream, string(snapshot.Commit))
	if err != nil {
		return snapshot, err
	}
	first, err := e.Repo.FirstCommitAbove(ctx, base, string(snapshot.Commit))
	if err != nil {
		return snapshot, err
	}
	message, err := e.Repo.CommitMessage(ctx, first)
	if err != nil {
		return snapshot, err
	}
	author, err := e.Repo.Author(ctx)
	if err != nil {
		return snapshot, err
	}
	author.When = e.now()
	commit, err := e.Repo.WriteCommit(ctx, git.Commit{Tree: string(snapshot.Tree), Parents: []string{base}, Message: message, Author: author, Committer: author})
	if err != nil {
		return snapshot, err
	}
	if !dryRun {
		previous := git.RefValue{Exists: true, Object: string(snapshot.Commit)}
		backup, err := e.Repo.ReadRef(ctx, "refs/dockhand/adopted/"+branch)
		if err != nil {
			return snapshot, err
		}
		if err := e.Repo.UpdateRefs(ctx, []git.RefChange{
			{Name: "refs/dockhand/adopted/" + branch, Expected: backup, Desired: previous},
			{Name: "refs/heads/" + branch, Expected: previous, Desired: git.RefValue{Exists: true, Object: commit}},
		}); err != nil {
			return snapshot, err
		}
		progress.Report(ctx, "Squashed the %d commits of %s into one; the originals stay under refs/dockhand/adopted/%s", count, branch, branch)
	}
	snapshot.Commit, snapshot.Head = record.ObjectID(commit), record.ObjectID(commit)
	return snapshot, nil
}
