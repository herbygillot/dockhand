package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

// Refreshed is what reading one pull request found.
type Refreshed struct {
	Branch model.Branch
	// Changes say what is new since the last reading, for people.
	Changes []string
	Err     error
}

// RefreshPullRequests reads every open branch's pull request from the
// forge (Design v3 §6.13): its state, reviews, and checks. A merged pull
// request marks its branch merged, and a closed one closed; what changed
// is recorded as events. A pull request that can't be read is reported and
// the rest are read.
func (e *Engine) RefreshPullRequests(ctx context.Context) ([]Refreshed, error) {
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen, model.BranchClosed}})
		return err
	}); err != nil {
		return nil, err
	}
	var all []Refreshed
	for _, branch := range branches {
		if branch.PullRequest == nil {
			continue
		}
		refreshed, err := e.refresh(ctx, branch)
		refreshed.Err = err
		all = append(all, refreshed)
	}
	return all, nil
}

func (e *Engine) refresh(ctx context.Context, branch model.Branch) (Refreshed, error) {
	refreshed := Refreshed{Branch: branch}
	pr := branch.PullRequest
	ref := record.PullRequestRef{Forge: forge.GitHub, Repository: pr.Repository, Number: pr.Number}
	observed, err := e.forge().Observe(ctx, ref)
	if err != nil {
		return refreshed, err
	}
	if !observed.Found {
		return refreshed, fmt.Errorf("#%d was not found", pr.Number)
	}
	now := observed.PullRequest
	next := model.PullRequestObservation{State: string(now.State), Head: model.ObjectID(now.RemoteHead), Review: "none", Checks: "none", At: e.now()}
	if now.State == record.PullRequestOpen {
		status, err := e.forge().Inspect(ctx, ref)
		if err != nil {
			return refreshed, err
		}
		next.Draft, next.Review, next.Failing = status.Draft, status.Review, status.Checks.Failing
		switch checks := status.Checks; {
		case checks.Failed > 0:
			next.Checks = "failing"
		case checks.Pending > 0:
			next.Checks = "pending"
		case checks.Total > 0:
			next.Checks = "passing"
		}
	}
	previous := pr.Observed
	if previous == nil {
		previous = &model.PullRequestObservation{}
	}
	if previous.State != next.State {
		refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d is %s", pr.Number, next.State))
	}
	if next.State == string(record.PullRequestOpen) {
		if previous.Review != next.Review && next.Review != "none" {
			refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d: %s", pr.Number, reviewWords(next.Review)))
		}
		if previous.Checks != next.Checks && (next.Checks == "failing" || next.Checks == "passing") {
			refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d: CI %s", pr.Number, next.Checks))
		}
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		current, err := tx.Branch(branch.ID)
		if err != nil || current.PullRequest == nil || current.PullRequest.Number != pr.Number {
			return err
		}
		current.PullRequest.Observed = &next
		current.PullRequest.Draft = next.Draft
		switch {
		case next.State == string(record.PullRequestMerged) && current.State.CanBecome(model.BranchMerged):
			current.State = model.BranchMerged
		case next.State == string(record.PullRequestClosed) && current.State == model.BranchOpen:
			current.State = model.BranchClosed
		case next.State == string(record.PullRequestOpen) && current.State == model.BranchClosed:
			current.State = model.BranchOpen
		}
		if err := tx.UpdateBranch(current); err != nil {
			return err
		}
		refreshed.Branch = current
		for _, change := range refreshed.Changes {
			if _, err := tx.AppendEvent(model.Event{At: next.At, Branch: branch.ID, Kind: "pr.observed", Level: model.LevelInfo, Message: branch.ShortName() + ": " + change}); err != nil {
				return err
			}
		}
		return nil
	})
	return refreshed, err
}

func reviewWords(review string) string {
	switch review {
	case "changes-requested":
		return "changes requested"
	case "approved":
		return "approved"
	}
	return "no review yet"
}

// SomeoneElsePushed reports whether the pull request holds a commit that
// dockhand did not push and the branch does not have.
func (s BranchStatus) SomeoneElsePushed() bool {
	pr := s.Branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.Head == "" || pr.Pushed == "" {
		return false
	}
	return pr.Observed.Head != pr.Pushed && string(pr.Observed.Head) != s.Head
}

// Failing lists the pull request's failing checks, sorted.
func (s BranchStatus) Failing() []string {
	if s.Branch.PullRequest == nil || s.Branch.PullRequest.Observed == nil {
		return nil
	}
	failing := slices.Clone(s.Branch.PullRequest.Observed.Failing)
	slices.Sort(failing)
	return failing
}
