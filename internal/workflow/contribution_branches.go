package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// A merged pull request ends a contribution, and its branches are residue.
// retireBranches deletes the local branch and the fork's head branch, each
// only while it still holds the published commit, so nothing unpublished is
// lost. Every outcome is reported; none fails the retirement.
func (e *Engine) retireBranches(ctx context.Context, change record.Change, pr record.PullRequest, published record.ObjectID) []string {
	var notes []string
	if change.Branch != "" {
		notes = append(notes, e.deleteLocalBranch(ctx, change.Branch, published))
	}
	if pr.HeadBranch != "" && pr.HeadRepository != "" {
		notes = append(notes, e.deleteForkBranch(ctx, pr))
	}
	return notes
}

func (e *Engine) deleteLocalBranch(ctx context.Context, branch string, published record.ObjectID) string {
	checkouts, err := e.Repo.Checkouts(ctx, branch)
	if err != nil {
		return "local branch " + branch + " kept: " + err.Error()
	}
	if len(checkouts) > 0 {
		return "local branch " + branch + " kept; it is checked out at " + strings.Join(checkouts, ", ")
	}
	expected := git.RefValue{Exists: true, Object: string(published)}
	err = e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch, Expected: expected}})
	var conflict *git.RefConflict
	switch {
	case err == nil:
		return "local branch " + branch + " deleted"
	case errors.As(err, &conflict) && !conflict.Actual.Exists:
		return "local branch " + branch + " was already gone"
	case errors.As(err, &conflict):
		return "local branch " + branch + " kept; it no longer holds the published commit"
	default:
		return "local branch " + branch + " kept: " + err.Error()
	}
}

func (e *Engine) deleteForkBranch(ctx context.Context, pr record.PullRequest) string {
	label := pr.HeadRepository + ":" + pr.HeadBranch
	if e.Publisher == nil || e.Publisher.Forge == nil {
		return "fork branch " + label + " kept; no forge is configured"
	}
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return "fork branch " + label + " kept: " + err.Error()
	}
	pushURL := ""
	for _, remote := range remotes {
		name, err := e.Publisher.Forge.NameFromRemote(remote.PushURL)
		if err == nil && strings.EqualFold(name, pr.HeadRepository) {
			pushURL = remote.PushURL
			break
		}
	}
	if pushURL == "" {
		return "fork branch " + label + " kept; no local remote pushes to " + pr.HeadRepository
	}
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return "fork branch " + label + " kept: " + err.Error()
	}
	call, cancel := context.WithTimeout(ctx, timeouts.Publish)
	defer cancel()
	err = e.Repo.DeleteRemoteBranch(call, pushURL, pr.HeadBranch, git.RefValue{Exists: true, Object: string(pr.RemoteHead)})
	var conflict *git.RefConflict
	switch {
	case err == nil:
		return "fork branch " + label + " deleted"
	case errors.As(err, &conflict):
		return "fork branch " + label + " kept; it no longer holds the merged commit"
	default:
		return fmt.Sprintf("fork branch %s kept: %v", label, err)
	}
}
