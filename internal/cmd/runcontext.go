package cmd

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// RunContext is one dockhand run: what the user asked for through the
// global flags, and the run-scoped facilities every command draws from.
// Commands took these from *cobra.Command directly, which meant each
// re-derived the prefix and the tree for itself and acquired evaluators
// two different ways. One run, resolved once.
//
// It deliberately lives in this package and goes no further. Everything
// below takes what it needs — a Prefix, an Evaluator, a tempdir.Root —
// because a planner that accepted a RunContext would be a planner that
// could reach the command line, and the domain packages stay testable
// precisely because they cannot. That rule is not a convention here: cmd
// imports bump, classify and plan, so any of them importing this would
// be an import cycle.
//
// Configuration is held; resources are not. Prefix, evaluators, the
// fetcher and the temporary root are built on first use and remembered, so
// a run that needs none of them — version, help, a usage error — starts
// none of them, and a test can construct a RunContext without a MacPorts
// installation anywhere in sight.
//
// Not safe for concurrent use: a run is one command on one goroutine.
type RunContext struct {
	// TreeRoot and PrefixPath are as the user gave them; empty means
	// discover. Debug is the flag, already applied to the logger.
	TreeRoot   string
	PrefixPath string
	Debug      bool

	// Out and Err are the run's streams. Structured output goes to Out
	// and prose to Err, so a plan can be piped while its summary stays
	// readable.
	Out, Err io.Writer

	pfx     prefix.Prefix
	pfxErr  error
	pfxDone bool

	tempRoot tempdir.Root
	tempErr  error
	tempDone bool

	ev *eval.Evaluator
	fe *portfetch.Fetcher

	closers []func()
}

// init fills the context from the parsed global flags. It runs once per
// execution, before any command's own work.
func (rc *RunContext) init(c *cobra.Command) error {
	treeRoot, err := c.Flags().GetString("tree")
	if err != nil {
		return err
	}
	prefixPath, err := c.Flags().GetString("prefix")
	if err != nil {
		return err
	}
	debug, err := c.Flags().GetBool("debug")
	if err != nil {
		return err
	}
	rc.TreeRoot, rc.PrefixPath, rc.Debug = treeRoot, prefixPath, debug
	rc.Out, rc.Err = c.OutOrStdout(), c.ErrOrStderr()

	level := slog.LevelWarn
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// With no tree named, the one the user is standing in is the one
	// they mean. Best-effort on purpose: a command that needs no tree
	// must not fail because the working directory is not in one, so a
	// fruitless search leaves TreeRoot empty and the commands that do
	// need a tree report it themselves. This runs after the logger is
	// configured so that --debug can say which tree was found.
	if rc.TreeRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			if root, err := tree.Find(wd); err == nil {
				slog.Debug("ports tree discovered from the working directory", "tree", root)
				rc.TreeRoot = root
			}
		}
	}
	return nil
}

// Prefix is the MacPorts installation this run works against: the one
// the user named, or the one discovered. A stated prefix is never fallen
// back from.
func (rc *RunContext) Prefix() (prefix.Prefix, error) {
	if !rc.pfxDone {
		rc.pfxDone = true
		if rc.PrefixPath != "" {
			rc.pfx, rc.pfxErr = prefix.New(rc.PrefixPath)
		} else {
			rc.pfx, rc.pfxErr = prefix.Find()
		}
	}
	return rc.pfx, rc.pfxErr
}

// TempDir is the run's temporary root, created on first use. Everything
// temporary a run produces belongs under it, so what a killed run left
// behind is one identifiable tree.
func (rc *RunContext) TempDir() (tempdir.Root, error) {
	if !rc.tempDone {
		rc.tempDone = true
		rc.tempRoot, rc.tempErr = tempdir.New()
		if rc.tempErr == nil {
			slog.Debug("temp root", "dir", rc.tempRoot.Path())
		}
	}
	return rc.tempRoot, rc.tempErr
}

// Pool starts n evaluators against the run's installation and registers
// their shutdown. Callers do not close it; the run does.
func (rc *RunContext) Pool(ctx context.Context, n int) (*pool.Pool, error) {
	pfx, err := rc.Prefix()
	if err != nil {
		return nil, err
	}
	p, err := pool.New(ctx, pfx, n)
	if err != nil {
		return nil, err
	}
	rc.closers = append(rc.closers, func() { p.Close() })
	return p, nil
}

// Evaluator is the run's single evaluator, for the commands that work
// one port at a time. It replaces two separate acquisition paths with
// one.
func (rc *RunContext) Evaluator(ctx context.Context) (*eval.Evaluator, error) {
	if rc.ev == nil {
		p, err := rc.Pool(ctx, 1)
		if err != nil {
			return nil, err
		}
		rc.ev = p.Evaluators()[0]
	}
	return rc.ev, nil
}

// Fetcher is the run's fetch session, over MacPorts' own curl.
func (rc *RunContext) Fetcher(ctx context.Context) (*portfetch.Fetcher, error) {
	if rc.fe == nil {
		pfx, err := rc.Prefix()
		if err != nil {
			return nil, err
		}
		root, err := rc.TempDir()
		if err != nil {
			return nil, err
		}
		f, err := portfetch.New(ctx, pfx, root)
		if err != nil {
			return nil, err
		}
		rc.closers, rc.fe = append(rc.closers, f.Close), f
	}
	return rc.fe, nil
}

// Close shuts down everything the run started and removes its temporary
// root, in reverse order of acquisition. It runs whether the command
// succeeded or failed, which is the point: the failures are what used to
// leave directories behind.
func (rc *RunContext) Close() {
	for i := len(rc.closers) - 1; i >= 0; i-- {
		rc.closers[i]()
	}
	rc.closers = nil
	if err := rc.tempRoot.Remove(); err != nil {
		slog.Warn("temp root left behind", "dir", rc.tempRoot.Path(), "err", err)
	}
}
