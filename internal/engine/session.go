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

// Leader is the serve leading the queue, which runs what's queued; nil
// where none leads, and a check runs where it's asked.
func (e *Engine) Leader(ctx context.Context, session *coord.Session) (*model.Session, error) {
	return session.Holder(ctx, coord.LeaderResource)
}

// SeeThrough sees a queued run to its end: driven here, where drive says
// so, else followed as serve drives it. A serve that started meanwhile and
// took the run is followed instead, and returned as taker. The engine's
// and the providers' reports are the work behind the scenes to a process
// driving only to see its own run through (progress.Quiet). Where ctx ends
// first, the run is as it was last read, with ctx's error.
func (e *Engine) SeeThrough(ctx context.Context, session *coord.Session, id model.RunID, drive bool) (run model.Run, taker *model.Session, err error) {
	if drive {
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
