package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// Services is THE COMPOSITION ROOT. It holds what the global flags said
// and the run-scoped facilities an invocation actually uses — the
// prefix, the repository, the temporary root, an evaluator, a fetch
// session, the verifier, the forge — and it closes every one of them in
// REVERSE ORDER OF ACQUISITION whether the operation succeeded or not.
//
// IT REPLACES runstate.Context, AND THE DIFFERENCE IS THE WHOLE OF THIS
// STEP'S DONE-CRITERION. runstate memoized lazily: every service was a
// method that resolved on first use, so which services an invocation
// acquired was decided deep inside whatever code path happened to ask,
// and a `--plan` that never verified still built a verify provider if
// some branch on the way there touched one. Here the set is DECLARED —
// app.ChangeRequest.Needs() computes it from the whole parsed request,
// and every other verb states its own — and Acquire opens exactly that
// and nothing else. "No dependency is resolved that the invocation will
// not use" is then a property of the code rather than an aspiration
// about it: there is no lazy path left to resolve one by accident.
//
// The tool finder, the verifier resolver and the forge runner are
// FIELDS wired here and stood in by tests, because a package-level seam
// would be mutable global state.
type Services struct {
	// What the global flags said. The persistent set is exactly three —
	// --prefix, --tree and --debug — since the dispatch ruling retired
	// --auto: the invoker stops being something an invocation asserts
	// and becomes a property of WHICH PROCESS acted.
	TreeRoot   string
	PrefixPath string
	Debug      bool

	// Version is the running binary's, as the linker knows it. It rides
	// into publish.Env because a published sentence found to be wrong
	// must be traceable to the build that wrote it.
	Version string
	// Agent is $AI_AGENT, read ONCE here: a service that read its own
	// environment would be deciding provenance rather than being told
	// it. It is recorded beside the invoker and READ BY NO GATE, so
	// setting it can neither grant nor withhold anything.
	Agent string
	// Grant is what THIS BUILD permits a machine to spend. It is spent
	// from a build-time constant at exactly one point (see grant.go) and
	// travels as a value, so no invocation, no environment variable and
	// no configuration file can be the thing that changed it.
	Grant publish.Grant

	Out, Err io.Writer

	// Tools finds the external programs this run drives — git, tart, gh,
	// the block generators — one finder, so doctor's answer and the
	// working code's are the same lookup.
	Tools *tool.Finder
	// Verifier and Lister resolve the machine's verify backend. TWO
	// RESOLVERS AND NOT ONE, because they answer two different questions:
	// verifying needs a base image to run on and listing or releasing a
	// guest does not, and a machine whose bases are gone is exactly where
	// a cloned worker outlives them and pins one of two slots. See
	// VerifyProvider for how the two are composed.
	Verifier func(ctx context.Context) (verify.Verifier, error)
	Lister   func(ctx context.Context) (verify.Verifier, error)
	// Forge runs one gh invocation and returns its stdout.
	Forge gh.Runner

	// base is the memo behind BaseRef: the commit a mint cuts from,
	// resolved ONCE per process.
	//
	// It is a memo and not a field because resolving it goes to the
	// NETWORK — upstream's primary branch is fetched so a change is based
	// on the newest tip rather than on whatever this checkout last pulled
	// — and a sweep plans hundreds of ports through a worker pool. One
	// fetch per port would be hundreds of round trips to say the same
	// thing, and worse, the ports at the end of a long sweep would be
	// based on a different commit from the ones at the start: one
	// invocation must mint one base.
	base     baseMemo
	baseOnce sync.Once

	// Now is the clock. A field so a test may pin it and so a pass and
	// the report of it agree about when the pass was.
	Now func() time.Time

	// born is THIS PROCESS'S START, read once when the root command is
	// built and never again. It is record.OwnerID.Since for every
	// identity this process produces, which is what makes the (PID,
	// start-time) liveness pair an identity rather than a coincidence —
	// see Me. Unexported and written once, so no verb can move it; a
	// test that needs a fixed one builds its Services through newRoot's
	// own path or sets it there.
	born time.Time

	// The resolved handles. Every one is filled by Acquire and by
	// nothing else: there is no lazy accessor, which is the point.
	repo  *git.Repo
	tr    *tree.Tree
	pfx   prefix.Prefix
	temp  tempdir.Root
	ev    *eval.Evaluator
	fetch *portfetch.Fetcher
	state *statestore.Store
	led   *ledger.Ledger

	// have records what was actually opened, so a road that asks for a
	// service it did not declare gets a wiring error rather than a nil
	// pointer three frames down.
	have app.Needs

	fetchMu sync.Mutex

	closers []func()
}

// ErrNotAcquired is a road asking for a service its Needs did not
// declare. It is a sentinel and a WIRING GAP, not a fact about the
// machine: the composition root opens what a request declares, so a
// road that reaches past its own declaration has a bug in the
// declaration and not in the environment. Rule 7 in the other
// direction — a nil repository must never be readable as "no
// repository here".
var ErrNotAcquired = errNotAcquired{}

type errNotAcquired struct{ what string }

func (e errNotAcquired) Error() string {
	if e.what == "" {
		return "cli: a service was used that this invocation did not declare"
	}
	return "cli: " + e.what + " was used and this invocation did not declare it"
}

// Is makes every specific refusal match the bare sentinel, so a caller
// may test errors.Is(err, ErrNotAcquired) without knowing which service
// was missed.
func (e errNotAcquired) Is(target error) bool {
	_, ok := target.(errNotAcquired)
	return ok
}

// Acquire opens exactly the services a request declares, in dependency
// order, and registers each one's shutdown. It is called ONCE per
// invocation, before the operation is built, so the operation is handed
// values rather than resolvers.
//
// The order is the order the dependencies actually run: the prefix
// before anything that evaluates, the temporary root before the fetcher
// that writes into it, the repository before the store and the ledger
// that are opened over it. Close runs it backwards.
//
// A REPOSITORY IMPLIES THE STORE AND THE LEDGER, and that is not a
// fourth flag on Needs. Both are constructed over an open *git.Repo and
// neither opens anything of its own — statestore.Open and ledger.Open
// are struct literals — so a Needs.Repo that did not carry them would
// be a distinction with no cost on either side of it.
func (s *Services) Acquire(ctx context.Context, n app.Needs) error {
	if n.Evaluator || n.Fetcher {
		p, err := s.Prefix()
		if err != nil {
			return err
		}
		s.pfx = p
	}
	if n.Tree {
		t, err := tree.Open(s.TreeRoot)
		if err != nil {
			return err
		}
		s.tr = t
	}
	if n.Repo {
		dir := s.TreeRoot
		if dir == "" {
			dir = "."
		}
		r, err := git.Open(ctx, s.Tools, dir)
		if err != nil {
			return err
		}
		s.repo, s.state, s.led = r, statestore.Open(r), ledger.Open(r)
		slog.Debug("repository", "root", r.Root)
	}
	// THE TEMPORARY ROOT BELONGS TO ANY ROAD THAT MATERIALIZES ANYTHING,
	// which is a wider set than the two that evaluate: a road that may
	// START a build stages the attempt's portdirs out of git through
	// run.Stager, and a stage issued from the ZERO Root lands straight in
	// the system temporary directory owned by nothing — so a killed run
	// would leave an anonymous tree instead of one identifiable one, which
	// is the whole reason tempdir exists. Hence Verifier here beside
	// Evaluator and Fetcher.
	if n.Evaluator || n.Fetcher || n.Verifier {
		root, err := tempdir.New()
		if err != nil {
			return err
		}
		s.temp = root
		s.closers = append(s.closers, func() {
			if err := root.Remove(); err != nil {
				fmt.Fprintf(s.Err, "warning: removing the run's temporary root: %v\n", err)
			}
		})
		slog.Debug("temp root", "dir", root.Path())
	}
	if n.Evaluator {
		p, err := pool.New(ctx, s.pfx, 1)
		if err != nil {
			return err
		}
		s.closers = append(s.closers, p.Close)
		s.ev = p.Evaluators()[0]
	}
	if n.Fetcher {
		f, err := portfetch.New(ctx, s.pfx, s.temp)
		if err != nil {
			return err
		}
		s.closers = append(s.closers, f.Close)
		s.fetch = f
	}
	// The verifier and the forge are RESOLVERS and not handles: a
	// machine with no tart is not an error, and the roads that may start
	// a build ask for one when they are ready to. What Needs decides
	// here is whether the road may ask at all — Verifier() refuses a
	// road that did not declare one — so a --plan can never boot a
	// provider by touching a branch that asks.
	s.have = n
	return nil
}

// Close shuts down everything Acquire started, in reverse order of
// acquisition, and it runs WHETHER THE OPERATION SUCCEEDED OR NOT. The
// failures are what used to leave directories behind.
func (s *Services) Close() {
	for i := len(s.closers) - 1; i >= 0; i-- {
		s.closers[i]()
	}
	s.closers = nil
}

// ProposeTree is the tree the COHORT PROPOSAL needs, opened
// best-effort, and it is deliberately not app.Needs.Tree.
//
// Needs.Tree means "this road resolves a selector against the tree and
// cannot proceed without one" — every intent verb, and the two report
// verbs. The roads that SETTLE hold a tree for one reason only:
// run.Finish's propose step reads the reverse index and the
// maintainer's Portfile cues through run.Local, and run.Local's own
// contract says a backend that cannot answer returns an error and the
// propose step RECORDS NOTHING, "which is rule 7's answer: no finding
// is not a finding of no dependents". An unbuilt index is exactly that
// case and it is the ordinary one on a fresh checkout.
//
// So a `status` or a `cancel` in a git checkout that is not a ports
// tree settles what it settles and proposes nothing, where declaring
// Needs.Tree would have refused the whole verb in the tree band over a
// finding nobody asked for.
func (s *Services) ProposeTree() local {
	if s.tr != nil {
		return local{tr: s.tr, repo: s.repo}
	}
	t, err := tree.Open(s.TreeRoot)
	if err != nil {
		slog.Debug("no ports tree for the propose step; cohort proposals will record nothing", "err", err)
		return local{repo: s.repo}
	}
	s.tr = t
	return local{tr: t, repo: s.repo}
}

// Pool starts n evaluators against this run's installation and
// registers their shutdown, so the survey verbs — which want N
// evaluators where every other road wants one — do not have to hold
// their own lifetime.
//
// It is a second acquisition beside Needs.Evaluator rather than a knob
// on it, because the two answer different questions: Needs says WHETHER
// an evaluation happens on this road, and n says how wide the survey
// is. A verb that asks for a pool has already declared a tree.
func (s *Services) Pool(ctx context.Context, n int) (*pool.Pool, error) {
	pfx, err := s.Prefix()
	if err != nil {
		return nil, err
	}
	p, err := pool.New(ctx, pfx, n)
	if err != nil {
		return nil, err
	}
	s.closers = append(s.closers, p.Close)
	return p, nil
}

// Prefix is the MacPorts installation this run works against: the one
// the user named, or the one discovered. A stated prefix is never
// fallen back from.
func (s *Services) Prefix() (prefix.Prefix, error) {
	if s.PrefixPath != "" {
		return prefix.New(s.PrefixPath)
	}
	return prefix.Find(s.Tools)
}

// Repo, Tree, State, Ledger, Temp and Eval hand back what Acquire
// opened, and refuse a road that did not declare them. The refusal is
// what makes the declaration load-bearing rather than documentation.
// baseMemo is what BaseRef resolved, kept so the answer and its failure
// are both remembered.
type baseMemo struct {
	ref string
	err error
}

// BaseRef is the rev a mint cuts its change from: upstream's primary
// branch, fetched, so the branch dockhand hands a maintainer is based on
// the newest tip the project has rather than on the one their checkout
// happens to hold.
//
// ONE FETCH PER INVOCATION, whatever it is planning. See the memo's own
// doc: a sweep would otherwise make one round trip per port and, worse,
// spread one sweep's changes across several bases.
//
// fetch false is the caller declining the network — the `--no-fetch`
// road — and it answers with the local primary without asking anything.
// The two are memoized together on purpose: an invocation that says
// --no-fetch says it once.
func (s *Services) BaseRef(ctx context.Context, fetch bool) (string, error) {
	s.baseOnce.Do(func() { s.base.ref, s.base.err = s.resolveBase(ctx, fetch) })
	return s.base.ref, s.base.err
}

func (s *Services) resolveBase(ctx context.Context, fetch bool) (string, error) {
	repo, err := s.Repo()
	if err != nil {
		return "", err
	}
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	if !fetch {
		return primary, nil
	}
	ref, err := freshPrimary(ctx, s.Forge, repo, s.Err, primary)
	if err != nil {
		// freshPrimary said why on stderr. A fetch that failed must not
		// stop a bump — offline is not a planning error — and it must not
		// pass silently either, which is what the sentence is for.
		return primary, nil
	}
	return ref, nil
}

func (s *Services) Repo() (*git.Repo, error) {
	if s.repo == nil {
		return nil, errNotAcquired{"a repository"}
	}
	return s.repo, nil
}

func (s *Services) State() (*statestore.Store, error) {
	if s.state == nil {
		return nil, errNotAcquired{"the state ref"}
	}
	return s.state, nil
}

func (s *Services) Ledger() (*ledger.Ledger, error) {
	if s.led == nil {
		return nil, errNotAcquired{"the ledger"}
	}
	return s.led, nil
}

func (s *Services) Tree() (*tree.Tree, error) {
	if s.tr == nil {
		return nil, errNotAcquired{"a ports tree"}
	}
	return s.tr, nil
}

func (s *Services) Temp() tempdir.Root { return s.temp }

func (s *Services) Eval() (*eval.Evaluator, error) {
	if s.ev == nil {
		return nil, errNotAcquired{"an evaluator"}
	}
	return s.ev, nil
}

// Fetch is the distfile fetcher as the SEAM planning takes, and nil
// stays a nil interface rather than a typed nil in disguise: planning
// tests the value for nil, and a non-nil interface holding a nil
// pointer would pass that test and panic in the planner.
func (s *Services) Fetch() distfile.Fetcher {
	if s.fetch == nil {
		return nil
	}
	return s.fetch
}

// VerifyProvider is the resolver an operation is handed for its
// Verifier field. A road whose Needs said no verifier gets nil, which
// app reads as verify.ErrNoProvider — "this road wired none" — and that
// is a FACT rather than a wiring gap: cli acquires a verifier exactly
// where Needs says the delivery may start one, and a host with no tart
// wires none either. Both mean the same thing to every road below.
func (s *Services) VerifyProvider() func(context.Context) (verify.Verifier, error) {
	if !s.have.Verifier {
		return nil
	}
	return func(ctx context.Context) (verify.Verifier, error) {
		prov, err := s.Verifier(ctx)
		if err == nil || !errors.Is(err, verify.ErrNoEnvironment) {
			return prov, err
		}
		// A MACHINE WITH NO BASE IMAGES CAN STILL ACCOUNT FOR ITS
		// GUESTS, and that is the whole reason there are two resolvers.
		// ErrNoEnvironment means the backend is installed and has nothing
		// to CLONE FROM; it does not mean the backend cannot list, poll or
		// release what is already running — and a machine whose bases were
		// deleted is precisely where a cloned worker outlives them and
		// pins a slot nobody can see. So the roads that settle, discharge
		// and report fall back to the listing backend, where a Submit
		// would fail with the same sentinel it would have failed with
		// anyway.
		//
		// The fallback is HERE and not inside an operation, because this
		// is the layer that is allowed to name a backend at all: an
		// operation receives one verify.Verifier and must not know that
		// two ways of getting one exist.
		if s.Lister == nil {
			return prov, err
		}
		lister, lerr := s.Lister(ctx)
		if lerr != nil {
			return prov, err // the original refusal is the more useful one
		}
		return lister, nil
	}
}

// PublishEnv is the whole of the forge an operation may hold. A road
// whose Needs said no forge gets a zero Env, which publish refuses
// plainly rather than reaching for a runner nobody wired.
func (s *Services) PublishEnv() publish.Env {
	if !s.have.Forge {
		return publish.Env{}
	}
	return publish.Env{
		Repo: s.repo, Forge: s.Forge, State: s.state,
		Eval: s.publishEval(), Version: s.Version,
	}
}

// publishEval is publish's narrow evaluator seam, and nil where this
// invocation opened none. A nil Evaluator is not a failure there: it
// rides back as Direction.Err with Compared false, which the machine
// road refuses on and the human road is unaffected by, since a person
// may publish a downgrade on "the operator typed it".
func (s *Services) publishEval() publish.Evaluator {
	if s.ev == nil {
		return nil
	}
	return identityAt{ev: s.ev, repo: s.repo, temp: s.temp}
}

// Me is who THIS PROCESS is, and this function is the ONE POINT where
// record.OwnerID.Root is canonicalized: EvalSymlinks, then Abs, then
// Clean, as record/lease.go requires and as D20 is about. Two spellings
// of one checkout make this checkout's own leases read as foreign, and
// a foreign obligation is reported and never seized — so the
// environment is never released, the obligation is never Done, and the
// record is therefore never closed and never compacted.
//
// The root is the REPOSITORY's when there is one and the tree's
// otherwise, because `dockhand exec` runs with no repository at all and
// still takes an environment that has to be attributable.
//
// SINCE IS THE PROCESS'S BIRTH AND IT NEVER MOVES. It used to be a
// parameter, so that a resident dispatcher could restamp it on every
// pass; that made the identity useless and the liveness check actively
// wrong, and record.Claim.Pass already says so in the durable
// vocabulary ("Since is the process's start time and half of the
// liveness pair that decides whether a PID is the same process, so
// moving it per pass would make a resident dispatcher's own leases read
// as a stranger's between passes").
//
// The arithmetic that made it certain rather than merely risky:
// lease.sameProcess admits a gap of at most one minute between a
// recorded Since and the kernel's process start, sized on a measurement
// of Go's own startup (291ms and 479ms on two runs). A dispatcher
// restamping per pass writes a Since hours after its birth, so from its
// second minute of life EVERY peer asking about it got `gone` —
// DeadElsewhere, which Standing.Seizable admits — and the
// LiveElsewhere protection that exists precisely for "a person's verb
// running beside a live dispatcher" was unreachable. EarlierPass was
// unreachable too, since reaching it needs sameOwner and sameOwner
// compares Since exactly.
//
// What a pass needs instead is a PASS token, and it already has one:
// Claimant.Pass, stamped onto record.Claim.Pass, which is a report
// field by construction and never a seize condition.
func (s *Services) Me() record.OwnerID {
	root := s.TreeRoot
	if s.repo != nil {
		root = s.repo.Root
	}
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	return record.OwnerID{Root: canonical(root), Host: host, PID: os.Getpid(), Since: s.born}
}

// canonical is Root's one spelling. A path that cannot be resolved
// comes back as cleaned as it can be made rather than empty: an
// unresolvable root is still this checkout's root, and an empty one
// would make every lease in the store foreign at once.
func canonical(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// realVerifier builds the resolver of the machine's verify provider —
// the tart provider assembled from the base images actually present.
// The two ways of having no environment are told apart, because their
// remedies are: no tart at all is ErrNoProvider, and the roads narrow
// their contract around it (a bump mints and says unverified); tart
// with no base images is ErrNoEnvironment, and the remedy is
// provisioning.
//
// It lives at the composition root because it NAMES THE PROVIDER: an
// operation may speak verify's vocabulary and never the tart that
// implements it.
// noBases is the refusal a machine with no base images gets, and it
// NAMES THE CHEAP REMEDY WHEN THERE IS ONE.
//
// A golden is a base's reference copy, and restoring from it is a
// copy-on-write clone: seconds, no download. That has always been true
// and this message used to send every caller down the full provisioning
// road anyway — a fetch, a MacPorts install and a toolchain — because
// nothing enumerated goldens and so nothing could tell the two
// situations apart. A person who had just removed their bases was told
// to rebuild from scratch while the copy that would have taken seconds
// sat on the disk beside them.
//
// The listing is asked ONLY HERE, on the road that has already
// established there are no bases, so the ordinary path pays nothing for
// it. A listing that fails answers the plain refusal rather than
// inventing a remedy: rule 7, on a message rather than on an act.
func noBases(ctx context.Context, tools *tool.Finder) error {
	const plain = "%w: no base images; run `dockhand provision tart --macos <release>` first"
	goldens, err := (provision.Tart{Tools: tools}).Restorable(ctx)
	if err != nil || len(goldens) == 0 {
		return fmt.Errorf(plain, verify.ErrNoEnvironment)
	}
	names := make([]string, 0, len(goldens))
	for _, r := range goldens {
		names = append(names, strings.ToLower(r.Name))
	}
	return fmt.Errorf(
		"%w: no base images, but a golden copy stands for %s; "+
			"`dockhand provision tart --macos <release> --restore` clones one back in seconds, "+
			"or `--macos <release>` builds a new one from scratch",
		verify.ErrNoEnvironment, strings.Join(names, ", "))
}

func realVerifier(tools *tool.Finder) func(ctx context.Context) (verify.Verifier, error) {
	return func(ctx context.Context) (verify.Verifier, error) {
		if _, err := tools.Find(tool.Tart); err != nil {
			return nil, verify.NoProvider(
				"tart is not installed (`port install tart`); --no-verify skips verification")
		}
		releases, err := (provision.Tart{Tools: tools}).Provisioned(ctx)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			return nil, noBases(ctx, tools)
		}
		// Newest first: the provider's default is its first base, and the
		// default a quick bump wants is the current OS — the mundane-build
		// check — not the oldest.
		bases := make([]tart.Base, 0, len(releases))
		for i := len(releases) - 1; i >= 0; i-- {
			bases = append(bases, tart.Base{VM: tart.BaseName(releases[i]), Release: releases[i]})
		}
		return tart.Provider{Bases: bases, Tools: tools}, nil
	}
}

// realLister builds the resolver of the backend the obligation report
// asks. tart on PATH is the whole gate, because listing guests needs no
// base image — which is the point of it being a separate resolver:
// realVerifier refuses on a machine whose bases are gone, and a machine
// whose bases are gone is exactly where a cloned worker outlives them
// and pins one of two slots.
func realLister(tools *tool.Finder) func(ctx context.Context) (verify.Verifier, error) {
	return func(context.Context) (verify.Verifier, error) {
		if _, err := tools.Find(tool.Tart); err != nil {
			return nil, verify.NoProvider("tart is not installed")
		}
		return tart.Provider{Tools: tools}, nil
	}
}

// fetchSession opens the run's MacPorts fetch session, LAZILY and at
// most once, for the one caller that cannot declare it up front.
//
// Every other road states whether it fetches through app.Needs, before
// anything runs. `outdated` cannot: whether a livecheck is reached at
// all depends on what the cheap witnesses said about each port, and
// most of a staged sweep never reaches one — a category whose ports are
// all current is answered by ls-remote alone, and a category the
// exclusion filter emptied is answered by nothing. Opening a tclsh for
// either is the eager cost that verb's whole design is about not
// paying, and on a machine where the session will not start it would
// fail a report the cheap stage could have finished.
//
// The lock is not decoration: this is the one acquisition that happens
// DURING the work, from whichever sweep worker arrives first, where
// every other one happens in Acquire before a goroutine exists.
func (s *Services) fetchSession(ctx context.Context) (*portfetch.Fetcher, error) {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	if s.fetch != nil {
		return s.fetch, nil
	}
	pfx, err := s.Prefix()
	if err != nil {
		return nil, err
	}
	if s.temp.Path() == "" {
		root, terr := tempdir.New()
		if terr != nil {
			return nil, terr
		}
		s.temp = root
		s.closers = append(s.closers, func() { _ = root.Remove() })
	}
	f, err := portfetch.New(ctx, pfx, s.temp)
	if err != nil {
		return nil, err
	}
	s.closers, s.fetch = append(s.closers, f.Close), f
	return f, nil
}
