package ghactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/remotewrite"
)

var _ buildenv.Settler = (*Provider)(nil)
var _ buildenv.AttemptLeftovers = (*Provider)(nil)

// branchNote reads only a check's own launch: an attach or rerun also
// names its branch, while a rejected push made nothing. Its full check ID
// and commit must name the branch, so neither a prefix nor another check's
// launch authorizes a deletion (#241).
func branchNote(execution model.GuestExecution) (launchNote, bool) {
	var note launchNote
	if json.Unmarshal([]byte(execution.Launch), &note) != nil ||
		(note.Action != launchPush && note.Action != launchAttach && note.Action != launchRerun) || note.Nothing != "" ||
		note.Check != string(execution.Run) || !git.ValidObjectID(note.Commit) ||
		note.Branch != buildenv.CheckBranch(note.Commit, execution.Run) {
		return note, false
	}
	return note, true
}

// Settle removes only branches the check noted pushing, under the last
// attempt's launch lock. It neither reads nor stops workflow runs: the
// runner has already recorded that no attempt follows (#241).
func (p *Provider) Settle(ctx context.Context, request buildenv.SettleRequest) ([]string, error) {
	// The caller keeps settlement alive after an interrupt. Bound the
	// whole operation, including fork lookup, the lock and remote writes,
	// so that WithoutCancel cannot hang the check's exit (#241).
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var notes []launchNote
	seen := map[string]bool{}
	for _, attempt := range request.Attempts {
		if attempt.Run != request.Execution.Run || attempt.Environment.Provider != p.Name() {
			continue
		}
		note, ok := branchNote(attempt)
		if !ok || seen[note.Repository+"\n"+note.Branch] {
			continue
		}
		seen[note.Repository+"\n"+note.Branch] = true
		notes = append(notes, note)
	}
	if len(notes) == 0 {
		return nil, nil
	}
	release, err := request.Lock.Hold(ctx, stopWait)
	if err != nil {
		var said []string
		for _, note := range notes {
			said = append(said, settleFailure(note, err))
		}
		return said, err
	}
	defer release()
	var said []string
	var problems []error
	for _, note := range notes {
		fork, err := p.notedFork(ctx, note)
		if err != nil {
			said = append(said, settleFailure(note, err))
			problems = append(problems, err)
			continue
		}
		// Read first so a branch Execute already removed is quiet. The
		// deletion still compares the noted commit at the remote (#241).
		head, err := p.Repo.RemoteHead(ctx, fork.PushURL, note.Branch)
		if err == nil && !head.Exists {
			continue
		}
		if err == nil {
			err = remotewrite.Delete(ctx, p.Repo, remotewrite.Made, fork.Repository, fork.PushURL, note.Branch, git.RefValue{Exists: true, Object: note.Commit})
		}
		var moved *git.RefConflict
		switch {
		case errors.As(err, &moved):
			said = append(said, fmt.Sprintf("left %s on %s, since it has moved since the check", note.Branch, fork.Repository))
		case err != nil:
			said = append(said, settleFailure(note, err))
			problems = append(problems, err)
		default:
			said = append(said, fmt.Sprintf("removed %s from %s", note.Branch, fork.Repository))
		}
	}
	return said, errors.Join(problems...)
}

// settleFailure keeps even a multiline Git refusal to one progress line
// naming the branch whose removal clean may retry (#241).
func settleFailure(note launchNote, err error) string {
	return fmt.Sprintf("couldn't remove %s from %s; dockhand clean can try again: %s", note.Branch, note.Repository, strings.Join(strings.Fields(err.Error()), " "))
}

func (p *Provider) notedFork(ctx context.Context, note launchNote) (buildenv.Fork, error) {
	fork, err := p.Fork(ctx)
	if err == nil && !strings.EqualFold(note.Repository, fork.Repository) {
		err = fmt.Errorf("the launch named %s, not your fork %s", note.Repository, fork.Repository)
	}
	return fork, err
}

// Leftovers has nothing to list without the check's launch notes. The
// engine asks AttemptLeftovers with its recorded attempts instead (#241).
func (p *Provider) Leftovers(context.Context) ([]buildenv.Leftover, error) {
	return nil, nil
}

// AttemptLeftovers finds branches from recorded pushes, even one that never got
// a run URL. The note keeps the expected commit through clean's plan and
// removal; a moved branch is still listed, and its deletion refuses it.
// The engine keeps these while another attempt may resume (#241).
func (p *Provider) AttemptLeftovers(ctx context.Context, attempts []model.GuestExecution) ([]buildenv.Leftover, error) {
	var recorded []model.GuestExecution
	for _, attempt := range attempts {
		if _, ok := branchNote(attempt); ok && attempt.Environment.Provider == p.Name() {
			recorded = append(recorded, attempt)
		}
	}
	if len(recorded) == 0 || p.Fork == nil {
		return nil, nil
	}
	// This is optional cleanup. A missing setup is quiet, and an outage
	// is one informational row, never a failure that stops Tart's cleanup.
	// Resolve the current fork once; notes for an earlier account or fork
	// belong to neither this listing nor its removals (#241).
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fork, err := p.Fork(ctx)
	if errors.Is(err, buildenv.ErrNoFork) || errors.Is(err, github.ErrNoCredentials) {
		return nil, nil
	}
	if err != nil {
		return listingProblem(err), nil
	}
	if fork.Repository == "" || fork.PushURL == "" {
		return nil, nil
	}
	recorded = slices.DeleteFunc(recorded, func(attempt model.GuestExecution) bool {
		note, _ := branchNote(attempt)
		return !strings.EqualFold(note.Repository, fork.Repository)
	})
	if len(recorded) == 0 {
		return nil, nil
	}
	refs, err := p.Repo.ReadRemoteRefs(ctx, fork.PushURL, "refs/heads/"+BranchPrefix)
	if err != nil {
		return listingProblem(err), nil
	}
	var found []buildenv.Leftover
	seen := map[string]bool{}
	for _, attempt := range recorded {
		note, _ := branchNote(attempt)
		if seen[note.Branch] {
			continue
		}
		seen[note.Branch] = true
		head := refs["refs/heads/"+note.Branch]
		if head.Exists {
			leftover := buildenv.Leftover{Ref: attempt.Launch, What: "GitHub check branch " + note.Branch + " on " + fork.Repository, Execution: attempt.ID, SettledOnly: true}
			if head.Object != note.Commit {
				leftover.Kept = "it moved since the check"
			}
			found = append(found, leftover)
		}
	}
	return found, nil
}

// listingProblem is one line however many recorded checks the failed
// listing couldn't look at. Clean and daily cleanup both say it (#241).
func listingProblem(err error) []buildenv.Leftover {
	return []buildenv.Leftover{{What: "GitHub check branches", Kept: "couldn't list them: " + strings.Join(strings.Fields(err.Error()), " ")}}
}

// RemoveLeftover uses the launch note clean listed, never the branch's
// current head as its expected value (#241).
func (p *Provider) RemoveLeftover(ctx context.Context, ref string) error {
	var note launchNote
	if err := json.Unmarshal([]byte(ref), &note); err != nil {
		return err
	}
	if _, ok := branchNote(model.GuestExecution{Run: model.RunID(note.Check), Launch: ref}); !ok {
		return fmt.Errorf("not a check's recorded push")
	}
	fork, err := p.notedFork(ctx, note)
	if err != nil {
		return err
	}
	head, err := p.Repo.RemoteHead(ctx, fork.PushURL, note.Branch)
	if err != nil {
		return err
	}
	if !head.Exists {
		return buildenv.ErrLeftoverGone
	}
	return remotewrite.Delete(ctx, p.Repo, remotewrite.Made, fork.Repository, fork.PushURL, note.Branch, git.RefValue{Exists: true, Object: note.Commit})
}
