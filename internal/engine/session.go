package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
)

// SessionProcess and Liveness are a session's process and what judges
// another's (coord), as the engine's options name them.
type (
	SessionProcess = coord.Process
	Liveness       = coord.Liveness
)

// Session records this process's session of kind and keeps its heartbeat
// until ctx ends: the one coordinator built for a command, where command
// built its own (the command package split's 7b row 2).
func (e *Engine) Session(ctx context.Context, kind model.SessionKind) (*coord.Session, error) {
	c := &coord.Coordinator{Store: e.Store, Repository: e.Repository, Liveness: e.options.Liveness, Heartbeat: e.options.Heartbeat}
	self := coord.Self
	if e.options.Process != nil {
		self = e.options.Process
	}
	process, err := self()
	if err != nil {
		return nil, err
	}
	session, err := c.StartFor(ctx, process, kind, buildinfo.Current().String())
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
