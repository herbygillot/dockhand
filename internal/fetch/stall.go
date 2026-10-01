package fetch

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrStalled means a transfer went longer than its bound without a byte.
var ErrStalled = errors.New("fetch: no data arrived")

// Stall bounds a transfer by its silences rather than its length. A
// download's size is unbounded, at the person's word (2026-10-01), and a
// bound on its whole time would bound its size as surely: rustc's 566 MB
// source passed the two minutes it had. A transfer going on at any pace
// goes on; one that stops is given up.
type Stall struct {
	bound  time.Duration
	timer  *time.Timer
	cancel context.CancelCauseFunc
}

// NewStall gives a context that's cancelled, with ErrStalled as its cause,
// once bound passes with no byte read through the stall's Reader, the
// wait for a response included; Stop ends it.
func NewStall(ctx context.Context, bound time.Duration) (context.Context, *Stall) {
	ctx, cancel := context.WithCancelCause(ctx)
	s := &Stall{bound: bound, cancel: cancel}
	s.timer = time.AfterFunc(bound, func() { cancel(ErrStalled) })
	return ctx, s
}

// Reader reads through r, each byte that arrives putting the bound off.
func (s *Stall) Reader(r io.Reader) io.Reader { return stallReader{r: r, s: s} }

// Stop ends the bound, and the context it gave.
func (s *Stall) Stop() {
	s.timer.Stop()
	s.cancel(context.Canceled)
}

type stallReader struct {
	r io.Reader
	s *Stall
}

func (r stallReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.s.timer.Reset(r.s.bound)
	}
	return n, err
}
