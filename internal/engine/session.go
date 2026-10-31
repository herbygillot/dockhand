package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/store"
)

// Session records this process's session of kind and keeps its heartbeat
// until ctx ends: the one coordinator built for a command, where command
// built its own (the command package split's 7b row 2).
func (e *Engine) Session(ctx context.Context, kind model.SessionKind) (*coord.Session, error) {
	c := &coord.Coordinator{Store: e.Store, Repository: e.Repository}
	session, err := c.Start(ctx, kind, buildinfo.Current().String())
	if err != nil {
		return nil, err
	}
	go session.KeepAlive(ctx)
	return session, nil
}

type observerKey struct{}

// Observing gives a command one observer session, a session that only
// reads and judges who is alive, on ctx, which every judgment made under
// ctx shares. Each session is a row and two journal events, and status
// judged twice a render, which watch redrew every 30 seconds. A ctx that
// has one keeps it, and ending it is its opener's (the command package
// split's 7b row 2).
func (e *Engine) Observing(ctx context.Context) (context.Context, *coord.Session, func(), error) {
	if session, ok := ctx.Value(observerKey{}).(*coord.Session); ok {
		return ctx, session, func() {}, nil
	}
	session, err := e.Session(ctx, model.SessionObserver)
	if err != nil {
		return ctx, nil, func() {}, err
	}
	return context.WithValue(ctx, observerKey{}, session), session, func() { session.End(context.WithoutCancel(ctx)) }, nil
}

// Leader is the serve leading the queue, which runs what's queued; nil
// where none leads, and a check runs where it's asked.
func (e *Engine) Leader(ctx context.Context, session *coord.Session) (*model.Session, error) {
	return session.Holder(ctx, coord.LeaderResource)
}

// Lead is who sees a run through: the serve leading the queue, and the
// process holding the run, which may be that serve or another's; driven
// here where neither is.
type Lead struct {
	Serve, Holder *model.Session
}

// Here is a run nobody else sees through, which is driven here.
func (l Lead) Here() bool { return l.Serve == nil && l.Holder == nil }

// LeadOf is who sees the run through now.
func (e *Engine) LeadOf(ctx context.Context, session *coord.Session, id model.RunID) (Lead, error) {
	serve, err := e.Leader(ctx, session)
	if err != nil {
		return Lead{}, err
	}
	holder, err := session.Holder(ctx, RunResource(id))
	return Lead{Serve: serve, Holder: holder}, err
}

// SeeThrough sees a queued run to its end: followed as the serve leading
// the queue, or the process holding the run, drives it, else driven here,
// decided from who leads as it starts, which before, where it's set, is
// told first, for a command to say. A serve that started meanwhile and
// took the run is followed instead, and returned as taker. The engine's
// and the providers' reports are the work behind the scenes to a process
// driving only to see its own run through (progress.Quiet). Where ctx ends
// first, the run is as it was last read, with ctx's error.
func (e *Engine) SeeThrough(ctx context.Context, session *coord.Session, id model.RunID, before func(Lead)) (run model.Run, taker *model.Session, err error) {
	lead, err := e.LeadOf(ctx, session, id)
	if err != nil {
		return model.Run{}, nil, err
	}
	if before != nil {
		before(lead)
	}
	// A run that ended needs no slot: it's read, where it waited for a
	// slot a batch's other checks held (SeeAllThrough).
	if ended, err := e.Run(ctx, id); err != nil || ended.State.Terminal() {
		return ended, nil, err
	}
	if lead.Here() {
		if err := e.awaitSlot(ctx, session, id); err != nil {
			return model.Run{}, nil, err
		}
		run, err = e.Drive(progress.Quiet(ctx), session, id)
		held := new(coord.HeldError)
		if !errors.As(err, &held) {
			return run, nil, err
		}
		taker = &held.Holder
	}
	run, err = e.waitFor(ctx, id)
	return run, taker, err
}

// SeeAllThrough sees a batch's queued runs to their ends together, each as
// SeeThrough sees one, in a session of its own: driven here where nothing
// leads, at most as many at once on a provider as its capacity, which a
// run waiting for its slot couldn't see of its siblings still queued, or
// followed where serve leads or another process holds it. ended is told of
// each run as it ends, in the order they end, one at a time, so a batch
// takes about its longest check, not the sum of them (Codex's workflow
// review of 2ad896d2; the person's "every ask I add to Dockhand can
// increase the amount of time it takes to go through the bump process",
// 2026-10-29). Where ctx ends first, each run is as it was last read.
//
// waited, where given, hears each check's wait for a slot or a VM as its
// journal says it, once, which the batch says: G13's checks went one at a
// time behind a VM, and nothing said why (rc1's full stage).
func (e *Engine) SeeAllThrough(ctx context.Context, ids []model.RunID, waited func(model.Run, string), ended func(model.Run, error)) {
	type seen struct {
		run model.Run
		err error
	}
	slots := map[string]chan struct{}{}
	for provider := range e.Providers {
		slots[provider] = make(chan struct{}, e.capacity(provider))
	}
	results := make(chan seen, len(ids))
	for _, id := range ids {
		go func() {
			results <- func() seen {
				session, err := e.Session(ctx, model.SessionForeground)
				if err != nil {
					return seen{model.Run{ID: id}, err}
				}
				defer session.End(context.WithoutCancel(ctx))
				run, err := e.Run(ctx, id)
				if err != nil {
					return seen{run, err}
				}
				// The batch's own slots, taken in one order, so two runs
				// needing the same providers never hold one each.
				if lead, err := e.LeadOf(ctx, session, id); err == nil && lead.Here() {
					needs, err := e.RunProviders(ctx, run)
					if err != nil {
						return seen{run, err}
					}
					slices.Sort(needs)
					for _, provider := range needs {
						slot, ok := slots[provider]
						if !ok {
							continue
						}
						select {
						case slot <- struct{}{}:
							defer func() { <-slot }()
							continue
						default:
						}
						// It waits for a sibling's slot, which it says.
						if waited != nil {
							waited(run, fmt.Sprintf("%s waits for a slot on %s, which the batch's other checks are using (capacity %d)", run.Name(), provider, cap(slot)))
						}
						select {
						case slot <- struct{}{}:
							defer func() { <-slot }()
						case <-ctx.Done():
							return seen{run, ctx.Err()}
						}
					}
				}
				if waited != nil {
					watching, stop := context.WithCancel(ctx)
					defer stop()
					go e.sayWaits(watching, run, waited)
				}
				run, _, err = e.SeeThrough(ctx, session, id, nil)
				return seen{run, err}
			}()
		}()
	}
	for range ids {
		result := <-results
		ended(result.run, result.err)
	}
}

// sayWaits tells waited each new wait a run's journal records, until ctx
// ends: a batch's checks are driven quietly, so a wait for a slot or a VM
// was only in the journal.
func (e *Engine) sayWaits(ctx context.Context, run model.Run, waited func(model.Run, string)) {
	var last string
	for {
		var message string
		_ = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			event, found, err := r.LatestRunEvent(run.ID, model.EventWaiting)
			if found {
				message = event.Message
			}
			return err
		})
		if message != "" && message != last {
			last = message
			waited(run, message)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(waitPoll):
		}
	}
}

// waitPoll is how often a batch reads its checks' waits.
var waitPoll = 2 * time.Second

// waitFor reads a run until it ends or ctx does.
func (e *Engine) waitFor(ctx context.Context, id model.RunID) (model.Run, error) {
	for {
		run, err := e.Run(ctx, id)
		if err != nil || run.State.Terminal() {
			return run, err
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
