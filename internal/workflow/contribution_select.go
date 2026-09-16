package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// ContributionSelector locates existing work without choosing a new source.
// Target constrains an explicit branch/change, or selects a unique open change.
type ContributionSelector struct {
	Target   string
	Branch   string
	ChangeID record.ChangeID
}

func (s ContributionSelector) Validate() error {
	if s.Target != "" && !macports.ValidName(s.Target) || s.Branch != "" && !git.ValidBranchName(s.Branch) || s.ChangeID != "" && !validToken(string(s.ChangeID)) {
		return fmt.Errorf("%w: invalid contribution selector", ErrInvalidRequest)
	}
	if s.Target == "" && s.Branch == "" && s.ChangeID == "" {
		return fmt.Errorf("%w: select a target, branch, or change", ErrInvalidRequest)
	}
	return nil
}

func selectContribution(ctx context.Context, reader state.Reader, selected ContributionSelector) (record.Change, error) {
	if err := selected.Validate(); err != nil {
		return record.Change{}, err
	}
	var change record.Change
	var err error
	switch {
	case selected.ChangeID != "":
		change, err = reader.Change(ctx, selected.ChangeID)
	case selected.Branch != "":
		change, err = reader.OpenChangeByBranch(ctx, selected.Branch)
	default:
		var matches []record.Change
		matches, err = reader.Changes(ctx, state.Query{Target: selected.Target, Pending: true, Limit: 16})
		if err == nil {
			if len(matches) == 0 {
				return change, fmt.Errorf("%w: no open contribution for %s; start a bump, or explicitly select --branch or --working-tree for manual verification", ErrNotFound, selected.Target)
			}
			if len(matches) > 1 {
				choices := make([]string, 0, len(matches))
				for _, candidate := range matches {
					branch := candidate.Branch
					if branch == "" {
						branch = "not prepared"
					}
					choices = append(choices, fmt.Sprintf("%s (%s)", candidate.ID, branch))
				}
				return change, fmt.Errorf("%w: multiple open contributions for %s: %s; select --change or --branch", ErrInvalidRequest, selected.Target, strings.Join(choices, ", "))
			}
			change = matches[0]
		}
	}
	if err != nil {
		return change, err
	}
	if change.Disposition != record.ChangeOpen {
		return change, fmt.Errorf("%w: contribution %s is %s", ErrInvalidRequest, change.ID, change.Disposition)
	}
	if selected.Branch != "" && selected.Branch != change.Branch || selected.Target != "" && !strings.EqualFold(selected.Target, change.InitiatingTarget) {
		return change, fmt.Errorf("%w: selector does not match contribution %s (%s)", ErrInvalidRequest, change.ID, change.InitiatingTarget)
	}
	return change, nil
}

func (e *Engine) SelectContribution(ctx context.Context, selected ContributionSelector) (record.Change, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return record.Change{}, ErrNoState
	}
	var change record.Change
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, reader state.Reader) error {
		var err error
		change, err = selectContribution(ctx, reader, selected)
		return err
	})
	return change, err
}

func contributionPrepared(change record.Change) error {
	if change.Branch == "" || change.CurrentRevision == "" {
		return fmt.Errorf("%w: %s has no prepared update branch; finish bump preparation before verifying or publishing (contribution %s)", ErrInvalidRequest, change.InitiatingTarget, change.ID)
	}
	return nil
}

// ContributionBuild returns the latest accepted verification settings for this
// contribution; it never chooses a successful historical revision as the source.
func (e *Engine) ContributionBuild(ctx context.Context, change record.Change) (record.JobSpec, error) {
	var spec record.JobSpec
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, reader state.Reader) error {
		current, err := reader.Change(ctx, change.ID)
		if err != nil {
			return err
		}
		if current.CurrentRevision != change.CurrentRevision {
			return ErrStaleRevision
		}
		jobs, err := reader.Jobs(ctx, state.Query{ChangeID: change.ID, Newest: true, WithBuild: true, Limit: 1})
		if err != nil {
			return err
		}
		if len(jobs) == 1 {
			spec = jobs[0].Spec
		}
		return nil
	})
	return spec, err
}
