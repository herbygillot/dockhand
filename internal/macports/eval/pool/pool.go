// Package pool maintains a set of evaluators over one MacPorts
// installation for callers sweeping many evaluations across workers. It
// owns evaluator lifecycle — batch start tolerating partial failure,
// replacement of evaluators a caller judges broken, closing whatever is
// current — while job scheduling and the judgment of brokenness stay
// with the caller.
package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
)

// Pool is the evaluator set. Replace may be called concurrently from
// workers; each *eval.Evaluator is still single-conversation and must
// be used by one worker at a time.
type Pool struct {
	ctx  context.Context
	pfx  prefix.Prefix
	opts []eval.Option

	mu  sync.Mutex
	evs map[*eval.Evaluator]bool
}

// New starts up to size evaluators against one installation. Failing to
// start some is tolerated — a sweep on a loaded machine should run
// degraded rather than not at all — but zero is an error that always
// carries eval.ErrStartup, whatever the underlying cause. ctx governs
// the evaluators' lifetime, replacements included.
func New(ctx context.Context, pfx prefix.Prefix, size int, opts ...eval.Option) (*Pool, error) {
	if size < 1 {
		size = 1
	}
	p := &Pool{ctx: ctx, pfx: pfx, opts: opts, evs: make(map[*eval.Evaluator]bool)}
	var lastErr error
	for i := 0; i < size; i++ {
		ev, err := p.spawn()
		if err != nil {
			lastErr = err
			continue
		}
		p.evs[ev] = true
	}
	if len(p.evs) == 0 {
		if !errors.Is(lastErr, eval.ErrStartup) {
			lastErr = fmt.Errorf("%w: %w", eval.ErrStartup, lastErr)
		}
		return nil, fmt.Errorf("pool: no evaluator could be started (prefix: %s): %w", pfx, lastErr)
	}
	return p, nil
}

func (p *Pool) spawn() (*eval.Evaluator, error) {
	proc, err := shell.Start(p.ctx, p.pfx.PortTclsh())
	if err != nil {
		return nil, err
	}
	return eval.New(p.ctx, proc, p.opts...)
}

// Evaluators returns the pool's current set, one entry per started
// evaluator. Callers typically take this once, before workers start,
// and hand one evaluator to each worker.
func (p *Pool) Evaluators() []*eval.Evaluator {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*eval.Evaluator, 0, len(p.evs))
	for ev := range p.evs {
		out = append(out, ev)
	}
	return out
}

// Replace closes a broken evaluator and starts a fresh one in its
// place. On failure the broken evaluator is still closed and dropped
// from the pool; the worker that held it has nothing left to work with.
func (p *Pool) Replace(ev *eval.Evaluator) (*eval.Evaluator, error) {
	ev.Close()
	p.mu.Lock()
	delete(p.evs, ev)
	p.mu.Unlock()

	fresh, err := p.spawn()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.evs[fresh] = true
	p.mu.Unlock()
	return fresh, nil
}

// Close shuts down every evaluator the pool currently holds.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for ev := range p.evs {
		ev.Close()
	}
	p.evs = make(map[*eval.Evaluator]bool)
}
