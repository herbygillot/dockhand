package classify

import (
	"context"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
)

// perPortTimeout bounds one port's evaluation: a Portfile that hangs its
// evaluator becomes an EvalFailed result, not a stuck sweep.
const perPortTimeout = 60 * time.Second

// Sweep classifies portdirs through the given pool, invoking each for
// every result from a single goroutine, in completion order. The caller
// owns the pool's lifetime; one worker runs per pooled evaluator. A
// worker whose evaluator looks broken (three consecutive evaluation
// failures) replaces it through the pool and continues.
func Sweep(ctx context.Context, p *pool.Pool, portdirs []string, each func(Result)) {
	jobs := make(chan string, len(portdirs))
	for _, d := range portdirs {
		jobs <- d
	}
	close(jobs)

	evs := p.Evaluators()
	results := make(chan Result, 2*len(evs))
	var wg sync.WaitGroup
	for _, ev := range evs {
		wg.Add(1)
		go func(ev *eval.Evaluator) {
			defer wg.Done()
			consecutiveFails := 0
			for dir := range jobs {
				cctx, cancel := context.WithTimeout(ctx, perPortTimeout)
				r := Port(cctx, ev, dir)
				cancel()
				results <- r
				if r.Outcome == EvalFailed {
					if consecutiveFails++; consecutiveFails >= 3 {
						replacement, err := p.Replace(ev)
						if err != nil {
							return
						}
						ev = replacement
						consecutiveFails = 0
					}
				} else {
					consecutiveFails = 0
				}
			}
		}(ev)
	}
	go func() { wg.Wait(); close(results) }()
	for r := range results {
		each(r)
	}
}
