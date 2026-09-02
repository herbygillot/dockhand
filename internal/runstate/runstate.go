// Package runstate is one dockhand run: what the user asked for
// through the global flags, and the run-scoped facilities every
// command draws from — the prefix, the repository, the temp root,
// evaluators, the fetch session — each resolved once and shut down
// together, so every part of one invocation agrees on which
// installation, which repository, and which tree it is in, and a
// failed run leaves nothing behind. The tool finder, the verify
// provider and the gh seam are fields, wired by the composition root
// and stood in by tests; a package-level seam would be mutable global
// state.
//
// The layering rule: a Context reaches the application layer — cmd's
// Actions and the lifecycle engine, which needs the run's streams,
// seams, and services throughout — and stops there. The domain
// packages (planners, styles, evaluation, vendored families) take only
// what they need — a Prefix, an Evaluator, a tempdir.Root — because a
// planner that accepted a Context would be a planner that could reach
// the command line.
package runstate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Context is one dockhand run's state: what the user asked for through
// the global flags, and the run-scoped facilities every command draws
// from — each resolved once, so every part of one invocation agrees
// on which installation, repository, and tree it is in. The root
// command builds it from the global flags and carries it on the
// context.Context (Into/From); the package comment says how far it
// reaches.
type Context struct {
	// TreeRoot and PrefixPath are as the user gave them; empty means
	// discover. Debug is the flag, already applied to the logger.
	TreeRoot   string
	PrefixPath string
	Debug      bool

	// Tools finds the external programs this run drives — git, tart,
	// gh, the block generators — one finder built at the composition
	// root and handed to every component that execs, so doctor's
	// answer and the working code's are the same lookup. A pointer:
	// the finder carries a lock and a memo, and status copies this
	// Context by value to reroute its prose.
	Tools *tool.Finder
	// Verifier resolves the machine's verify provider — wired by the
	// composition root (cmd's Root), stood in by tests. A package-level
	// seam would be mutable global state; a Context field is the same
	// injection every other service here gets.
	Verifier func(ctx context.Context) (verify.Verifier, error)
	// Gh runs one gh invocation and returns its stdout — the GitHub
	// seam, wired and stood in the same way.
	Gh func(ctx context.Context, args ...string) (string, error)

	// In, Out and Err are the run's streams. Structured output goes to
	// Out and prose to Err, so machine output can be piped while its
	// summary stays readable.
	In       io.Reader
	Out, Err io.Writer

	pfx     prefix.Prefix
	pfxErr  error
	pfxDone bool

	repo     *git.Repo
	repoErr  error
	repoDone bool

	tempRoot tempdir.Root
	tempErr  error
	tempDone bool

	ev *eval.Evaluator
	fe *portfetch.Fetcher

	closers []func()
}

// Init fills the context from what the flag layer parsed. It runs once
// per execution, before any command's own work. Flag extraction stays
// with the caller: this package knows runs, not command lines.
func (rc *Context) Init(treeRoot, prefixPath string, debug bool, in io.Reader, out, errOut io.Writer) {
	rc.TreeRoot, rc.PrefixPath, rc.Debug = treeRoot, prefixPath, debug
	rc.In, rc.Out, rc.Err = in, out, errOut

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
}

// Prefix is the MacPorts installation this run works against: the one
// the user named, or the one discovered. A stated prefix is never fallen
// back from.
func (rc *Context) Prefix() (prefix.Prefix, error) {
	if !rc.pfxDone {
		rc.pfxDone = true
		if rc.PrefixPath != "" {
			rc.pfx, rc.pfxErr = prefix.New(rc.PrefixPath)
		} else {
			rc.pfx, rc.pfxErr = prefix.Find(rc.Tools)
		}
	}
	return rc.pfx, rc.pfxErr
}

// Repo is the git repository this run works against: the one the
// user's tree names, or the one the working directory is in. Resolved
// once, so every part of one invocation agrees on which repository it
// is in — the door where symlinked-checkout identity gets to be right
// exactly once.
func (rc *Context) Repo(ctx context.Context) (*git.Repo, error) {
	if !rc.repoDone {
		rc.repoDone = true
		dir := rc.TreeRoot
		if dir == "" {
			dir = "."
		}
		rc.repo, rc.repoErr = git.Open(ctx, rc.Tools, dir)
		if rc.repoErr == nil {
			slog.Debug("repository", "root", rc.repo.Root)
		}
	}
	return rc.repo, rc.repoErr
}

// TempDir is the run's temporary root, created on first use. Everything
// temporary a run produces belongs under it, so what a killed run left
// behind is one identifiable tree.
func (rc *Context) TempDir() (tempdir.Root, error) {
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
func (rc *Context) Pool(ctx context.Context, n int) (*pool.Pool, error) {
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
func (rc *Context) Evaluator(ctx context.Context) (*eval.Evaluator, error) {
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
func (rc *Context) Fetcher(ctx context.Context) (*portfetch.Fetcher, error) {
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
func (rc *Context) Close() {
	for i := len(rc.closers) - 1; i >= 0; i-- {
		rc.closers[i]()
	}
	rc.closers = nil
	if err := rc.tempRoot.Remove(); err != nil {
		slog.Warn("temp root left behind", "dir", rc.tempRoot.Path(), "err", err)
	}
}

// key is the context key under which a run's Context travels. The run
// is ambient by the time an Action executes — the root command created
// and initialized it — so it rides the context.Context every execution
// already carries, and subcommand constructors stay pure grammar.
type key struct{}

// Into returns ctx carrying c.
func Into(ctx context.Context, c *Context) context.Context {
	return context.WithValue(ctx, key{}, c)
}

// From returns the run's Context. It panics when there is none: the
// root command is the single place that stores it, so absence is a
// wiring bug, not a runtime condition.
func From(ctx context.Context) *Context {
	c, ok := ctx.Value(key{}).(*Context)
	if !ok {
		panic("runstate: no Context in this context; the root command must store one")
	}
	return c
}

// VerifyProvider resolves the verify provider, refusing plainly when
// none was wired: a nil seam is a composition bug, not a machine
// state, and deserves its own message.
func (c *Context) VerifyProvider(ctx context.Context) (verify.Verifier, error) {
	if c.Verifier == nil {
		return nil, errors.New("no verify provider wired into this run")
	}
	return c.Verifier(ctx)
}

// RunGH runs one gh invocation through the wired seam.
func (c *Context) RunGH(ctx context.Context, args ...string) (string, error) {
	if c.Gh == nil {
		return "", errors.New("no gh runner wired into this run")
	}
	return c.Gh(ctx, args...)
}
