package cmd

import (
	"context"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/classify"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/sweep"
)

// classifyAction surveys ports for version-style tractability.
type classifyAction struct {
	args     []string
	workers  int
	all      bool
	declines bool
}

var _ Action = classifyAction{}

func (a classifyAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(ctx, rs, a.all, a.args)
	if err != nil {
		return err
	}
	p, err := rs.Pool(ctx, a.workers)
	if err != nil {
		return err
	}

	// A survey redirected to a file must not report success on a
	// partial write: exiting 0 over a truncated census would misreport
	// the tree. Run drains its rows on this goroutine, so the first
	// failure is captured without a lock.
	var census classify.Census
	var writeErr error
	cfg := sweep.Config[classify.Result]{
		PerTarget: sweep.DefaultPerTarget,
		// The census's own vocabulary for a sick evaluator, which is
		// the judgment Run cannot make for itself.
		Broken: func(r classify.Result) bool { return r.Outcome == classify.EvalFailed },
		// A port the pool could not reach is still a port the survey
		// was asked about. Counting it keeps the census total equal to
		// the number of targets, which is the only way a reader can
		// tell a complete survey from a short one.
		Abandon: func(t tree.Target, cause error) classify.Result {
			detail := "the evaluator pool died before this port was reached"
			if cause != nil {
				detail += ": " + cause.Error()
			}
			return classify.Result{Target: t, Outcome: classify.EvalFailed, Detail: detail}
		},
	}
	runErr := sweep.Run(ctx, cfg, p, targets,
		func(cctx context.Context, h port.Handle) classify.Result {
			return classify.Port(cctx, h)
		},
		func(r classify.Result) {
			census.Add(r)
			if a.declines && r.Outcome != classify.Located {
				if _, err := fmt.Fprintf(rs.Out, "%-14s %s\t%s\n",
					r.Outcome, r.Target.Portdir, r.Detail); err != nil && writeErr == nil {
					writeErr = err
				}
			}
		})
	if writeErr != nil {
		return writeErr
	}
	if _, err := fmt.Fprint(rs.Out, census.String()); err != nil {
		return err
	}
	// The census is printed either way — a survey that reached 19000 of
	// 20000 ports still measured 19000 — and the error is what says the
	// number is not the whole tree.
	return runErr
}

// resolveTargets turns a command's arguments into Targets through the
// sweep grammar. The resolution itself lives in internal/sweep now —
// this is what remains here: the flag, the usage voice, and the
// deferred tree.
//
// --all and the grammar's own `all` token are the same expansion, so
// the flag hands the token over rather than keeping a second code path
// beside it. The tree is still opened before --all's arity is checked,
// because that is the order the two errors have always come in and a
// user who typed `--all foo` at a directory that is not a ports tree
// should keep hearing about the tree first.
//
// The single-target verbs still call this, which is why it keeps its
// shape: what a selector means to a verb that mints branches is that
// verb's ruling to make, not a side effect of the grammar growing.
func resolveTargets(ctx context.Context, rs *runstate.Context, all bool, args []string) ([]tree.Target, error) {
	src := sweepSources(rs)
	if all {
		if _, err := src.Tree(); err != nil {
			return nil, err
		}
		if len(args) > 0 {
			return nil, usagef("--all takes no arguments")
		}
		args = []string{"all"}
	}
	if len(args) == 0 {
		return nil, usagef("specify ports, categories, or portdirs (or --all for the whole tree)")
	}
	res, err := sweep.Resolve(ctx, src, args)
	if err != nil {
		return nil, err
	}
	// What the grammar decided goes to stderr, where prose belongs, so
	// a maintainer sweep says which keys it used and how many ports
	// each names. Only the maintainer forms produce notes, so nothing
	// an existing invocation prints moves.
	for _, n := range res.Notes {
		fmt.Fprintln(rs.Err, "selector: "+n)
	}
	// The census surveys what it is pointed at, obsolete ports
	// included: what fraction of the tree dockhand can locate is the
	// measurement, and a filtered denominator would not be it. Nothing
	// here calls sweep.Select.
	return res.Targets, nil
}

// sweepSources is every lookup the selector grammar needs, wired to
// the run.
//
// One assembler for every verb that resolves a selector, because a
// verb that wired half of them does not fail: it advertises a form it
// cannot answer. `maintainer:me` needs the forge handle and the git
// identity, and reaching this with only a tree makes "me" report "no
// forge lookup is wired" — a sentence describing a bug in dockhand,
// handed to a user who typed something perfectly correct.
func sweepSources(rs *runstate.Context) sweep.Sources {
	return sweep.Sources{
		Tree:  treeSource(rs),
		Login: forgeLogin(rs),
		Email: gitIdentity(rs),
	}
}

// treeSource defers opening the tree until a selector form needs one,
// and keeps the message a user gets when there is none: a portdir path
// resolves without a tree, so `classify ./devel/foo` must still work
// outside one.
func treeSource(rs *runstate.Context) func() (*tree.Tree, error) {
	return func() (*tree.Tree, error) {
		if rs.TreeRoot == "" {
			return nil, usagef("a ports tree is needed: run inside one, pass --tree, or set DOCKHAND_TREE")
		}
		return rs.Tree()
	}
}

// Classify builds the classify subcommand.
func Classify() *cobra.Command {
	var (
		workers  int
		all      bool
		declines bool
	)
	c := &cobra.Command{
		Use:   "classify [port|category|portdir ...]",
		Short: "Survey ports for version-style tractability",
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return classifyAction{
				args:     args,
				workers:  workers,
				all:      all,
				declines: declines,
			}, nil
		}),
	}
	c.Flags().IntVarP(&workers, "workers", "j", min(8, runtime.NumCPU()),
		"evaluator pool size")
	c.Flags().BoolVarP(&all, "all", "a", false,
		"classify the entire tree")
	c.Flags().BoolVarP(&declines, "declines", "d", false,
		"list each port that was not located, as classified")
	return c
}
