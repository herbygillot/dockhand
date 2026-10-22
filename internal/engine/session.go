package engine

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
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
