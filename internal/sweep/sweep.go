// Package sweep is how dockhand does one thing to many ports: it turns
// what a user typed into targets, says which of those targets nobody
// should touch and why, and drives the rest through a pool of
// evaluators.
//
// The three halves are deliberately separate. Resolve is the selector
// grammar and reads only directory structure and the tree's PortIndex.
// Exclusions is pure — it decides nothing by looking, only by reading
// material a caller already gathered — so the reasons it gives can be
// tested against quoted Portfiles with no tree and no MacPorts. Run is
// the dispatch loop, and it knows nothing about ports beyond how to
// build a handle: the row type is the verb's own, so the census, the
// write verbs' NDJSON and outdated's table each keep their shape
// without a shared struct nobody wants.
//
// A single port is a sweep of one. That is the internal truth and it
// must not become the observable one: a verb that renders a census tail
// or a JSON row for one named port has changed what every existing
// invocation prints. Arity is the caller's to branch on, which is why
// Resolve reports what it found rather than deciding what to do about
// it.
package sweep

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// DefaultPerTarget bounds one target's evaluation when a Config names
// no bound of its own. It is the census's sixty seconds, which is the
// right order for a Portfile that hangs its evaluator and the wrong one
// for a verb whose per-target work includes a distfile fetch — so a
// verb that fetches sets its own rather than inheriting this.
const DefaultPerTarget = 60 * time.Second

// strikes is how many consecutive broken rows condemn an evaluator. A
// Tcl interpreter that has lost its footing answers every question
// wrongly from then on, and three in a row is the count that tells that
// apart from a run of ports that genuinely decline.
const strikes = 3

// ErrAbandoned reports targets a sweep never attempted because every
// worker had stopped while work was still queued. It is the sentinel
// behind AbandonedError, so a caller can ask errors.Is without knowing
// the type.
var ErrAbandoned = errors.New("sweep: targets abandoned")

// AbandonedError is a sweep that could not cover its targets: the
// evaluators died and the pool would not replace them, so the queue
// still held work when the last worker gave up.
//
// It exists because the alternative is the failure this package was
// promoted to end. A sweep that finishes with a short total and says
// nothing is indistinguishable from one that examined everything, and
// on a write verb that means ports a user believes were bumped and were
// not. The band is SweepHardErrors: these are not declines.
type AbandonedError struct {
	// Targets are the ones no worker reached, in the sweep's own
	// order.
	Targets []tree.Target
	// Cause is the pool's replacement failure, when a worker recorded
	// one. It may be nil: a pool with no evaluators abandons every
	// target without any replacement having been attempted.
	Cause error
}

func (e *AbandonedError) Error() string {
	msg := fmt.Sprintf("sweep: %d target(s) abandoned, first %s",
		len(e.Targets), e.Targets[0].Portdir)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes both the sentinel and the pool's own failure, so
// errors.Is(err, ErrAbandoned) and errors.Is(err, eval.ErrStartup) both
// answer about the same error.
func (e *AbandonedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrAbandoned}
	}
	return []error{ErrAbandoned, e.Cause}
}

// DockhandExit: a sweep that finished with errors that were not
// declines.
func (e *AbandonedError) DockhandExit() int { return exitcode.SweepHardErrors }

// Pool is what Run needs of an evaluator pool: the set to run on, and
// a way to replace one that has stopped answering. It is transcribed
// from *pool.Pool's method set rather than designed beside it, so the
// real pool satisfies it without an adapter and the assertion below
// fails to compile if either drifts.
//
// It is an interface for the reason port.Oracle is one. What the
// dispatch loop promises — every target gets one row, an interrupt
// strands rather than fabricates, a dead pool is reported and not
// swallowed — are properties of the loop and not of MacPorts, and a
// test that had to start a Tcl interpreter to check them would run only
// on machines that have one. The loop never asks an evaluator anything;
// it hands it to the verb.
type Pool interface {
	Evaluators() []*eval.Evaluator
	Replace(*eval.Evaluator) (*eval.Evaluator, error)
}

// The pool is the pool, unchanged.
var _ Pool = (*pool.Pool)(nil)

// Config is what a verb tells the dispatch loop about its own rows.
//
// Broken and Abandon are both required by genericity rather than by
// taste. The loop cannot read R, so it cannot know that a row means the
// evaluator is sick — and guessing from a nil-error convention would
// count a port's own decline as an evaluator fault and churn the pool
// on a tree full of ordinary declines. Nor can it invent a row for a
// target it never reached.
type Config[R any] struct {
	// PerTarget bounds one target's evaluation. Zero means
	// DefaultPerTarget.
	PerTarget time.Duration
	// TempDir is where the handles the loop builds materialize their
	// shadows. The zero value is the system temporary directory; a run
	// supplies its own so what it leaves behind can be attributed to
	// it. It is applied here and not by each verb, because a verb that
	// rebuilt the handle would be free to hold an evaluator across
	// targets and the replacement below would silently stop applying.
	TempDir tempdir.Root
	// Broken reports whether a row means the evaluator that produced
	// it is sick. Nil never replaces an evaluator.
	Broken func(R) bool
	// Abandon states a row for a target the sweep never reached. Nil
	// emits no row, which leaves AbandonedError as the only account of
	// the loss — honest, but a verb whose output is one row per port
	// should supply this so the arithmetic still adds up.
	Abandon func(tree.Target, error) R
}

// Run drives targets through the pool, invoking each for every row from
// a single goroutine, in completion order. The caller owns the pool's
// lifetime; one worker runs per pooled evaluator. A worker whose
// evaluator looks broken — three consecutive rows Broken accepts —
// replaces it through the pool and continues.
//
// Targets carry no evaluator: a handle is built at dispatch, when a
// worker's evaluator meets the next target, because which evaluator
// serves which target is not known before then.
//
// Rows are drained on the CALLER's goroutine. That is a promise and not
// an implementation detail: it is what lets a verb write one NDJSON row
// per port to stdout with no mutex, lets a census stay a plain struct,
// and makes row order deterministic-per-completion rather than
// interleaved. An edit that spawned the drain would hand every caller a
// data race at once.
//
// Concurrency here is evaluator parallelism and nothing else. It is
// CPU-bound, sized by the pool, and it is not the gate that bounds how
// many verifications are in flight or how fast a host is asked
// questions: submission capacity is VM-bound and a per-host budget
// cannot be expressed as a worker count, so both live inside the
// verb's own eval func. Sizing this by NumCPU and calling it politeness
// gives eight unpaced concurrent requests at one forge.
//
// Two failures return an error rather than passing unremarked:
//
// An interrupted sweep stops. Targets it never attempted produce no row
// at all, and neither does a target that was in flight when the
// cancellation landed — a result computed under a dead context
// describes the interruption, not the port. Run returns the context's
// error. This is what makes resume-by-rerun possible: rerunning is only
// a resume if the kill left the untouched ports unrecorded.
//
// A sweep whose workers all stopped with work still queued returns
// AbandonedError, having first emitted Abandon's row for each target it
// lost, so that every target in gets exactly one row out.
func Run[R any](
	ctx context.Context,
	cfg Config[R],
	p Pool,
	targets []tree.Target,
	evalOne func(context.Context, port.Handle) R,
	each func(R),
) error {
	if len(targets) == 0 {
		return nil
	}
	perTarget := cfg.PerTarget
	if perTarget <= 0 {
		perTarget = DefaultPerTarget
	}

	jobs := make(chan tree.Target, len(targets))
	for _, t := range targets {
		jobs <- t
	}
	close(jobs)

	evs := p.Evaluators()
	results := make(chan R, 2*len(evs))

	// stranded and replaceErr are the two things a worker learns on its
	// way out: a target it took but did not judge, and why the pool
	// would not give it a fresh evaluator.
	var mu sync.Mutex
	var stranded []tree.Target
	var replaceErr error
	strand := func(t tree.Target) {
		mu.Lock()
		stranded = append(stranded, t)
		mu.Unlock()
	}
	failed := func(err error) {
		mu.Lock()
		if replaceErr == nil {
			replaceErr = err
		}
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for _, ev := range evs {
		wg.Add(1)
		go func(ev *eval.Evaluator) {
			defer wg.Done()
			consecutive := 0
			for target := range jobs {
				if ctx.Err() != nil {
					strand(target)
					return
				}
				cctx, cancel := context.WithTimeout(ctx, perTarget)
				r := evalOne(cctx, port.New(target, ev).WithTempDir(cfg.TempDir))
				cancel()
				if ctx.Err() != nil {
					strand(target)
					return
				}
				results <- r
				if cfg.Broken == nil || !cfg.Broken(r) {
					consecutive = 0
					continue
				}
				if consecutive++; consecutive < strikes {
					continue
				}
				replacement, err := p.Replace(ev)
				if err != nil {
					// The pool closed and dropped the broken
					// evaluator before failing to spawn, so this
					// worker has nothing left to work with. It stops
					// without draining the queue: its siblings are
					// still running and the work is theirs to take.
					failed(err)
					return
				}
				ev = replacement
				consecutive = 0
			}
		}(ev)
	}

	// leftover is what the queue still held after the last worker
	// stopped. Reading it needs no lock: the drain writes it before
	// closing results, and the caller reads it after the range below
	// has seen that close.
	var leftover []tree.Target
	go func() {
		wg.Wait()
		for t := range jobs {
			leftover = append(leftover, t)
		}
		close(results)
	}()
	for r := range results {
		each(r)
	}

	mu.Lock()
	leftover = append(leftover, stranded...)
	cause := replaceErr
	mu.Unlock()
	if len(leftover) == 0 {
		return nil
	}
	sortTargets(leftover)

	// An interruption outranks a dead pool: the user asked for the
	// sweep to stop, and the ports it did not reach are not a fault
	// anybody needs a row about.
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Abandon != nil {
		for _, t := range leftover {
			each(cfg.Abandon(t, cause))
		}
	}
	return &AbandonedError{Targets: leftover, Cause: cause}
}

// sortTargets orders targets by portdir, then subport — the order every
// selector form resolves in, so a rerun of the same command reports the
// same ports in the same sequence.
func sortTargets(ts []tree.Target) {
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].Portdir != ts[j].Portdir {
			return ts[i].Portdir < ts[j].Portdir
		}
		return ts[i].Subport < ts[j].Subport
	})
}
