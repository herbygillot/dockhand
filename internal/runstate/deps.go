package runstate

import (
	"context"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/eval"
)

// Deps builds the run's engine. It is the ONE place a Context becomes
// engine.Deps, which is what keeps the layering honest in both
// directions: the engine names no runstate type, and the run is still
// the single owner of every memo — each field below is a closure over
// this Context, so two engines built from one run share its
// repository, its provider, its temporary root.
//
// A method rather than a package function taking a context.Context:
// the accessors take their own, and a run that had to be fetched off a
// context before it could hand out its own services would make the
// ambient Context part of the engine's contract.
func (rc *Context) Deps() *engine.Engine {
	return engine.New(engine.Deps{
		Repo:    rc.Repo,
		RepoFor: rc.RepoFor,
		Ledger:  ledger.Open,
		// Named rather than passed by value: VerifyProvider and RunGH
		// refuse plainly when their seam was never wired, and that
		// refusal is a composition bug worth its own message.
		Verifier: rc.VerifyProvider,
		Lister:   rc.WorkerLister,
		Gh:       rc.RunGH,
		Eval:     rc.Evaluator,
		Session:  rc.session,
		Fetch:    rc.Fetcher,
		Tree:     rc.Tree,
		Temp:     rc.TempDir,
		Tools:    rc.Tools,
		TreeRoot: rc.TreeRoot,
		Version:  rc.Version,
		Out:      rc.Out,
		Err:      rc.Err,
	})
}

// session starts a fresh evaluator against the run's installation, for
// the readings the run's own single evaluator cannot serve: one framed
// on a target platform rather than the host, and one that must be
// closed before the directories it evaluated are removed. The caller
// closes it — that is the difference from Evaluator, which the run
// closes.
func (rc *Context) session(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error) {
	pfx, err := rc.Prefix()
	if err != nil {
		return nil, err
	}
	return eval.Start(ctx, pfx, opts...)
}
