package engine

import (
	"context"
	"io"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Deps is what one run hands the engine: its streams, and each
// run-scoped service as the function that resolves it. They are funcs
// rather than values because the engine's roads want different
// subsets — a --plan never opens a repository, a tart-less status never
// composes a provider — and because every one of them is memoized by
// the run behind the func, so asking twice costs nothing and two Deps
// values built from the same run share one answer.
//
// Out and Err are plain writers, not accessors: diffFromPlan asks
// whether Out is a *os.File to decide whether to page, and a wrapper
// would disable the pager forever with nothing able to see it.
type Deps struct {
	// Repo is the run's repository, anchored on the tree (or the
	// working directory); RepoFor anchors it on a portdir an intent
	// named. They share one memo in the run, where the first asker
	// anchors, which is why both are here rather than one derived from
	// the other.
	Repo    func(ctx context.Context) (*git.Repo, error)
	RepoFor func(ctx context.Context, dir string) (*git.Repo, error)

	// Ledger opens custody of one repository's notes. A func so the
	// engine never names the constructor, and a test can hand it a
	// ledger over a repository of its own.
	Ledger func(repo *git.Repo) *ledger.Ledger

	// Verifier resolves the machine's verify provider, answer and
	// refusal alike, once per run.
	Verifier func(ctx context.Context) (verify.Verifier, error)

	// Lister resolves who can say what this machine is running, for the
	// worker audit. Deliberately not Verifier, because the two questions
	// have different preconditions: verifying needs a base image and
	// listing needs only the backend. The audit matters most on exactly
	// the machine where that gap opens — bases deleted or half
	// reprovisioned while their workers survive — and asking Verifier
	// there answers "no provider" about a backend that is holding two
	// slots. A verify.Verifier rather than a verify.WorkerLister because
	// a backend that cannot list is an answer the audit has to render
	// (as silence), and the type assertion is where that is decided.
	Lister func(ctx context.Context) (verify.Verifier, error)

	// Gh runs one gh invocation and returns its stdout.
	Gh func(ctx context.Context, args ...string) (string, error)

	// Eval is the run's single evaluator, for the one road that edits
	// the tree in place. Session is its opposite number: a fresh
	// short-lived evaluator the caller closes, which is what the
	// pre-flight (a target release's platform frame) and the
	// changed-context read (a session that must not outlive the temp
	// dirs it evaluated) each need and cannot get from the run's.
	Eval    func(ctx context.Context) (*eval.Evaluator, error)
	Session func(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error)

	// Fetch is the run's fetch session; Tree the ports tree it works
	// against; Temp the run's temporary root.
	Fetch func(ctx context.Context) (*portfetch.Fetcher, error)
	Tree  func() (*tree.Tree, error)
	Temp  func() (tempdir.Root, error)

	// Tools finds the external programs this run drives. The engine
	// asks it exactly one question — whether this machine can verify at
	// all — and asks the machine rather than a composed provider,
	// because composing one lists base images and a provider stood in
	// by a test is not evidence about the machine.
	Tools *tool.Finder

	// TreeRoot is the ports tree as the user named it, which is the
	// owner a gate's verification is attributed to.
	TreeRoot string

	// MachinePublish is whether THIS BUILD permits a machine to spend
	// ring 3 — to push a branch and open or edit a pull request with an
	// invoker of record.Machine. It is false on every build today; the
	// composition root spends a build-time constant into it, and the
	// engine cannot read that constant itself because it may not import
	// cmd.
	//
	// It is named for the permission GRANTED and never for one withheld,
	// and that is the whole of the guarantee: the zero value of a bool is
	// false, so every engine built by every test, every future
	// composition and every caller who has not heard of this field
	// refuses unattended publication by default. A NoMachinePublish that
	// defaulted to permissive would invert the argument and nobody would
	// notice until a pass had opened pull requests.
	MachinePublish bool

	// Version is the running binary's, for the words that name it.
	Version string

	// Out and Err are the run's streams. Structured output goes to Out
	// and prose to Err, so machine output can be piped while its
	// summary stays readable.
	Out, Err io.Writer
}

// Engine carries out one run's write and lifecycle work over Deps.
// Copying one is how a verb reroutes its prose — the accessors close
// over the run, so a copy shares its memos rather than resolving a
// second provider or a second temporary root.
type Engine struct{ Deps }

// New builds an engine over deps. The composition of deps is the run's;
// this exists so the engine's own tests need not restate the literal.
func New(d Deps) *Engine { return &Engine{Deps: d} }
