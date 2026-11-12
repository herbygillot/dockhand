package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// RequestCancel records that a run should stop. A queued run is canceled
// at once; a running one is stopped by whoever holds it, or by this
// session when no one does. A cancel already recorded and not finished is
// asked again, which is how a person looks again at work that wasn't
// known to have stopped.
func (c *Service) RequestCancel(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	var run model.Run
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil {
			return err
		}
		if run.State.Terminal() || run.CancelRequested != nil {
			return nil
		}
		now := c.Now()
		run.CancelRequested = &now
		if run.State == model.RunQueued {
			run.State, run.FinishedAt, run.Detail = model.RunCanceled, &now, "canceled before it started"
		}
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: now, Session: session.ID(), Branch: run.Branch, Run: run.ID, Kind: model.EventRunCancel, Level: model.LevelInfo, Message: run.Name() + ": cancel requested"})
		return err
	})
	if err != nil || run.State.Terminal() {
		return run, err
	}
	// With no one attending the run, this session applies the cancel.
	lease, holder, err := session.TakeIfUnattended(ctx, RunResource(id))
	if err != nil || holder != nil {
		return run, err
	}
	defer session.Release(context.WithoutCancel(ctx), lease)
	return c.settleCancel(ctx, session, lease, id)
}

// settleCancel is the one rule every cancel keeps, whoever applies it: the
// canceller where no driver holds the check, a driver that takes a check
// whose cancel is recorded, and the live driver once its providers
// return. It asks every provider to stop what the check launched
// (stopUnfinished), and the check is canceled only when each answered
// that its work ended. Until then it stays unfinished with its cancel
// recorded and the providers' words on it, which status shows, and the
// error says them with the command that asks again (the lifecycle
// design's "Stopping").
func (c *Service) settleCancel(ctx context.Context, session *coord.Session, lease model.Lease, id model.RunID) (model.Run, error) {
	c.asked(id)
	left, unknown, name, err := c.stopUnfinished(ctx, id)
	if err != nil {
		return model.Run{}, err
	}
	if len(unknown) == 0 {
		return c.finishCanceled(ctx, session, lease, id, left)
	}
	words := "a cancel was asked, and " + prose.And(unknown)
	var run model.Run
	if err := session.Fenced(context.WithoutCancel(ctx), lease, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil || run.State.Terminal() {
			return err
		}
		if run.CancelRequested != nil && run.Detail == words {
			return nil
		}
		now := c.Now()
		if run.CancelRequested == nil {
			run.CancelRequested = &now
		}
		run.Detail = words
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventRunCancel, Level: model.LevelInfo, Message: name + ": " + words})
		return err
	}); err != nil {
		return run, err
	}
	if run.State.Terminal() {
		return run, nil
	}
	return run, refusal.New(refusal.Infrastructure, "%s: %s; dockhand cancel %s asks again", name, words, name)
}

// stopUnfinished asks the provider of each attempt of a run to end the
// work it launched, where that work can outlive its driver
// (buildenv.Stopper): a GitHub check whose driver stopped kept its
// workflow running, and canceling it said it stopped (#115). It asks once
// for each provider and reference, so a matrix's cells, which share one,
// are asked once, and finished attempts too, since one a stopped driver
// interrupted is recorded as failed while its work runs: the provider
// tells its unfinished work from what it keeps. It goes on past a stop
// that fails. left is what ended work leaves, each named with what
// removes it, and unknown says each stop that couldn't be seen through,
// in the provider's words.
func (c *Service) stopUnfinished(ctx context.Context, id model.RunID) (left, unknown []string, name string, err error) {
	var executions []model.GuestExecution
	if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		run, err := r.Run(id)
		if err != nil {
			return err
		}
		name = run.Name()
		executions, err = r.Executions(id)
		return err
	}); err != nil {
		return nil, nil, "", err
	}
	asked := map[[2]string]bool{}
	for _, execution := range executions {
		provider := execution.Environment.Provider
		stopper, ok := c.Providers[provider].(buildenv.Stopper)
		if !ok {
			continue
		}
		// An attempt that recorded nothing is asked by itself: the
		// provider says whether anything of it can still be running.
		key := [2]string{provider, execution.ProviderRef}
		if execution.ProviderRef == "" {
			key[1] = "execution " + string(execution.ID)
		}
		if asked[key] {
			continue
		}
		asked[key] = true
		gone, err := stopper.Stop(ctx, execution)
		if err != nil {
			what := execution.ProviderRef
			if what == "" {
				what = "its work"
			}
			unknown = append(unknown, fmt.Sprintf("%s couldn't see %s stop: %v", provider, what, err))
			continue
		}
		left = append(left, gone...)
	}
	return left, unknown, name, nil
}

// finishCanceled settles a run as canceled, once every provider said its
// work ended, naming what that work left.
func (c *Service) finishCanceled(ctx context.Context, session *coord.Session, lease model.Lease, id model.RunID, left []string) (model.Run, error) {
	var run model.Run
	err := session.Fenced(ctx, lease, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil || run.State.Terminal() {
			return err
		}
		executions, err := tx.Executions(id)
		if err != nil {
			return err
		}
		now := c.Now()
		for _, execution := range executions {
			if !execution.State.Terminal() {
				execution.State, execution.FinishedAt, execution.Detail = model.ExecutionCanceled, &now, "canceled"
				if err := tx.UpdateExecution(execution); err != nil {
					return err
				}
			}
		}
		if run.State == model.RunQueued {
			run.State = model.RunRunning
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
		}
		detail := "canceled; finished results are kept"
		if len(left) > 0 {
			detail += "; left: " + strings.Join(left, "; ")
		}
		run.State, run.FinishedAt, run.Detail = model.RunCanceled, &now, detail
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventRunState, Level: model.LevelInfo, Message: run.Name() + " canceled"})
		return err
	})
	return run, err
}

// stopInterval is how often serve's loop and a worker ask again of a
// check whose cancel isn't finished: no more often than a provider polls
// (the GitHub provider's poll).
const stopInterval = 30 * time.Second

// asked notes that this service asked a check's providers to stop now.
func (c *Service) asked(id model.RunID) {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()
	if c.stopAsked == nil {
		c.stopAsked = map[model.RunID]time.Time{}
	}
	c.stopAsked[id] = c.Now()
}

// askedLately reports whether this service asked a check's providers to
// stop within stopInterval, which serve's loop leaves until it passes.
func (c *Service) askedLately(id model.RunID) bool {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()
	at, ok := c.stopAsked[id]
	return ok && c.Now().Sub(at) < stopInterval
}
