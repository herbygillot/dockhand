package cmd

// outdated asks upstream what it has, for one port or for a thousand.
//
// It is a report and it writes nothing, which is what lets it be the
// verb that answers a selector over a whole category: no branch, no
// note, no fetch of a distfile. What it is not is free. Every port it
// answers about costs somebody else a round trip, and a selector's
// worth of them lands overwhelmingly on one host — so the pacing, the
// staging and the observation cache in internal/upstream are not
// performance work, they are the reason this verb is allowed to exist
// in this shape at all.
//
// The command's own job is small: resolve the selector, filter what
// nobody should ask about, drive the observer through the sweep, and
// render. The politeness lives under it, where it can be tested with a
// fake clock and no network.

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/sweep"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
)

// perTarget bounds one port's whole update check.
//
// Three minutes rather than the census's sixty seconds, because this
// verb's per-target work is not evaluation. A livecheck phase fetches
// somebody's web page and parses it, and a slow mirror takes as long
// as it takes; a minute would report a timeout as an answer about the
// port, which is the wrong reason on a row a person will act on.
//
// The pacer's queue does not enter into it, and that is worth saying
// because it looks as though it should. Only a worker running a target
// ever waits on the pacer, so at most one request per worker is ever
// queued — eight, not nine hundred — and a target's wait for its turn
// is a few seconds however long the sweep as a whole takes.
const perTarget = 3 * time.Minute

// defaultTTL is how long an observation stands before it is asked
// again.
//
// Six hours is chosen against what upstream actually does: a project
// cuts a release a few times a year, and a maintainer sweeping a
// category twice in an afternoon is asking the same question twice.
// It is the wrong knob for a maintainer chasing one specific release,
// which is what --no-cache is for.
const defaultTTL = 6 * time.Hour

// pruneAge is how long a cache file outlives its last use. Thirty
// times the TTL: long enough that a weekly sweep never re-fetches what
// it already had, short enough that a repository dockhand stopped
// asking about does not sit in the cache forever.
const pruneAge = 30 * defaultTTL

// deepCeiling is how many ports --deep will answer about.
//
// --deep pays the expensive witness on every port rather than on the
// candidates the forge flags, and the expensive witness is a whole
// MacPorts livecheck phase fetching whatever site the maintainer
// declared — serialized, because the fetch session is not safe to
// share. `--deep -a` on this tree is 4764 of them, six hours of
// continuous fetching from one machine against several thousand
// unrelated web sites, and it is currently the easiest invocation to
// type. Two hundred is a category or a maintainer's ports: a sweep
// somebody chose, not the whole world.
//
// The refusal, and not a clamp, for the reason --riders is refused at
// selector scale: a run that quietly did something narrower than what
// was asked for is worse than one that says nobody has ruled on this.
const deepCeiling = 200

// paceFloor is the fastest --pace a selector may ask for.
//
// The flag exists so a maintainer chasing one release need not wait,
// and `--pace 1ms -a --no-cache` would defeat the entire ruling from
// the command line. One port may be asked for as fast as the user
// likes; four thousand may not.
const paceFloor = 100 * time.Millisecond

// outdatedAction asks upstream about every port a selector names.
type outdatedAction struct {
	args    []string
	workers int
	all     bool
	deep    bool
	current bool
	asJSON  bool
	noCache bool
	ttl     time.Duration
	pace    time.Duration
}

var _ Action = outdatedAction{}

func (a outdatedAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(ctx, rs, a.all, a.args)
	if err != nil {
		return err
	}

	// The exclusion filter is a selector-time filter and belongs to
	// selector-shaped invocations. A user who names one port has
	// already made this decision; a sweep of a category has not, and
	// `outdated category:perl` without it would ask upstream about
	// roughly two thousand perl5 stubs that were retired years ago.
	// That is the politeness ruling's own worked example, and the
	// signal is already in the index.
	var excluded []sweep.Excluded
	if len(targets) > 1 {
		tr, terr := rs.Tree()
		if terr != nil {
			return terr
		}
		sel, serr := sweep.Select(tr, targets)
		if serr != nil {
			return serr
		}
		targets, excluded = sel.Keep, sel.Excluded
	}
	// Nothing is collapsed by portdir. A branch edits a file, so the
	// write verbs collapse; a report writes nothing, and two subports
	// of one Portfile are two ports with two answers. They cost one
	// round trip between them anyway — the forge observation is keyed
	// by repository, so the second subport is served from the cache,
	// or, when the two land on two workers at once, from the first's
	// own round trip.

	if err := a.refuseAtScale(len(targets)); err != nil {
		return err
	}

	obs, cache, err := a.observer(ctx, rs)
	if err != nil {
		return err
	}

	var census upstream.Census
	var writeErr error
	rows := json.NewEncoder(rs.Out)
	emit := func(r upstream.Row) {
		census.Add(r)
		var err error
		switch {
		case a.asJSON:
			err = rows.Encode(r)
		case a.show(r):
			_, err = fmt.Fprintln(rs.Out, textRow(r))
		}
		if err != nil && writeErr == nil {
			writeErr = err
		}
	}
	// The excluded go first and in the order Select returned them,
	// which is the resolution's own. They are rows like any other: a
	// port left out with no line saying so is a port a reader has to
	// notice is missing.
	for _, ex := range excluded {
		emit(outdatedExcluded(ex))
	}

	var runErr error
	if len(targets) > 0 {
		p, perr := rs.Pool(ctx, a.workers)
		if perr != nil {
			return perr
		}
		runErr = sweep.Run(ctx, sweep.Config[upstream.Row]{
			PerTarget: perTarget,
			// Nothing here condemns an evaluator. This verb's failures
			// are upstream's — a forge that would not answer, a
			// livecheck whose site is down — and replacing a Tcl
			// interpreter over them would churn the pool for a reason
			// that has nothing to do with the pool. The three-strikes
			// rule exists for an interpreter that has lost its footing,
			// and a sweep that never evaluates anything but a port's
			// options rarely meets one.
			Abandon: outdatedAbandoned,
		}, p, targets, obs.Observe, emit)
	}

	// The tail goes to stderr so that a report piped to a file or to
	// jq stays a report, and the prose about it stays readable.
	fmt.Fprint(rs.Err, census.String())
	if n := cache.Prune(pruneAge); n > 0 {
		fmt.Fprintf(rs.Err, "  %-12s %5d removed\n", "cache", n)
	}
	if writeErr != nil {
		// A report redirected to a file must not exit 0 over a
		// truncated one.
		return writeErr
	}
	if runErr != nil {
		return runErr
	}
	return census.Err()
}

// refuseAtScale turns away the two invocations that are fine for a
// handful of ports and are an abuse of somebody else's server for
// thousands.
//
// Both refusals are at the boundary and both are about arity, for the
// reason the write verbs refuse --riders there: what a flag costs is
// not knowable until the selector has been resolved, and the cost is
// the whole of what is being ruled on. Neither clamps silently. A run
// that quietly asked a narrower question than the one typed would be
// answering something nobody asked.
func (a outdatedAction) refuseAtScale(n int) error {
	if a.deep && n > deepCeiling {
		return usagef("--deep runs every port's livecheck — a whole MacPorts fetch phase each, one at a time — and %d ports of that has no batching strategy anybody has ruled on; name a category or a maintainer (at most %d ports)",
			n, deepCeiling)
	}
	if a.pace > 0 && a.pace < paceFloor && n > 1 {
		return usagef("--pace %s over %d ports asks one host %.0f times a second; %s is the floor for a selector, and any pace is allowed for a single port",
			a.pace, n, float64(time.Second)/float64(a.pace), paceFloor)
	}
	return nil
}

// show decides whether a row belongs in the text report.
//
// The report is named for what it is looking for, so what it prints by
// default is everything that is not a port sitting where it should be.
// A thousand lines of "current" is a report nobody reads, and the
// census tail keeps the arithmetic honest for the reader who wants to
// know how many there were. --json is unfiltered on purpose: a person
// wants the exceptions and a program wants the census.
func (a outdatedAction) show(r upstream.Row) bool {
	return a.current || r.Outcome != upstream.OutcomeCurrent
}

// lazyLivecheck opens the run's fetch session on the first port that
// actually needs the expensive witness.
//
// Most of a staged sweep never does: a category whose ports are all
// current is answered by ls-remote alone, and a category the exclusion
// filter emptied is answered by nothing at all. Opening a MacPorts
// fetch session for either is the eager cost this verb's whole design
// is about not paying — and on a machine where the session will not
// start, it would fail a report that stage one could have finished.
//
// The lock is not decoration. runstate's facilities are memoized
// without synchronization because a run resolves them before its work
// begins, and this resolves one during it, from whichever sweep worker
// arrives first.
//
// run is the whole invocation's context and target is one port's, and
// keeping them apart is the entire reason this type carries a context
// field — which otherwise reads as the mistake it exists to prevent.
// The fetch session is a tclsh, and shell.Start kills it when the
// context it was started under is cancelled. Started under the target
// context a sweep worker hands in, the session would die the moment
// that port's three minutes were up or its work was done, and every
// port after it would fail with "session broken" — measured, on a
// sweep of one category that reported ten ports broken and the tree
// fine. The session belongs to the run; the call belongs to the
// target.
type lazyLivecheck struct {
	mu  sync.Mutex
	run context.Context //nolint:containedctx // the run's lifetime is the session's, and the target's is not
	rs  *runstate.Context
	f   upstream.Livechecker
	err error
}

func (l *lazyLivecheck) Livecheck(ctx context.Context, portdir, subport string) (portfetch.LivecheckResult, error) {
	l.mu.Lock()
	if l.f == nil && l.err == nil {
		l.f, l.err = l.rs.Fetcher(l.run)
	}
	f, err := l.f, l.err
	l.mu.Unlock()
	if err != nil {
		return portfetch.LivecheckResult{}, err
	}
	return f.Livecheck(ctx, portdir, subport)
}

// observer assembles the staged observer and the politeness under it.
func (a outdatedAction) observer(ctx context.Context, rs *runstate.Context) (*upstream.Observer, *courtesy.Cache, error) {
	m, cache := manners(rs, a.noCache, a.ttl, a.pace)
	return &upstream.Observer{
		Tools:   rs.Tools,
		Gh:      rs.Gh,
		Fetcher: &lazyLivecheck{run: ctx, rs: rs},
		Pacer:   m.Pacer,
		Cache:   m.Cache,
		Agent:   m.Agent,
		Deep:    a.deep,
	}, cache, nil
}

// manners assembles the politeness a selector-scale run asks upstream
// under: the pacer, the observation cache and the user agent.
//
// One assembler, because there is one ruling and two roads that have to
// obey it. The report asks a forge about a thousand ports; the write
// verbs resolve "latest" for a thousand ports, which is the same
// thousand questions of the same forge. Two constructors would be two
// chances for one of them to be built without a pacer, and the one
// without it is the one that gets the address blocked.
func manners(rs *runstate.Context, noCache bool, ttl, pace time.Duration) (upstream.Manners, *courtesy.Cache) {
	dir := ""
	if !noCache {
		// A machine with no usable cache directory is not a reason to
		// refuse the report; it is a reason to pay for every
		// observation, which is what an empty directory means here.
		if d, derr := courtesy.Dir(); derr == nil {
			dir = d
		}
	}
	cache := courtesy.NewCache(dir, ttl, nil)
	pol := courtesy.Default
	if pace > 0 {
		pol.Interval = pace
	}
	return upstream.Manners{
		Pacer: courtesy.NewPacer(pol, nil),
		Cache: cache,
		Agent: courtesy.UserAgent(rs.Version),
	}, cache
}

// sweepManners is the write verbs' politeness: the shared one, with the
// defaults, because a sweep that mints branches has no flags for any of
// this and needs it more than the report does — every port it names
// costs a livecheck phase and a call to a metered API before a single
// branch exists.
func sweepManners(rs *runstate.Context) (upstream.Manners, *courtesy.Cache) {
	return manners(rs, false, defaultTTL, 0)
}

// outdatedAbandoned is a port the pool never reached, as a row.
//
// It carries the machine's band, spelled exactly as the write verbs'
// own abandoned row spells it. A row built without a twin publishes
// code 0 and an empty family — success, on a port nothing looked at —
// and the zero twin is not a twin any constructor produces, so a
// consumer filtering on the family would find a value outside the
// vocabulary.
func outdatedAbandoned(t tree.Target, cause error) upstream.Row {
	detail := "the evaluator pool died before this port was reached"
	if cause != nil {
		detail += ": " + cause.Error()
	}
	return upstream.Row{
		Portdir: t.Portdir,
		Port:    upstream.PortName(t),
		Outcome: upstream.OutcomeAbandoned,
		Twin:    exitcode.Of(exitcode.EvalStartup, "sweep-abandoned"),
		Detail:  detail,
	}
}

// outdatedExcluded is a port the selector filter kept out, as a row.
//
// The name comes from the target rather than from an evaluation,
// because the whole point of excluding a port is that no evaluator
// ever meets it: a retired stub costs nothing to leave out and would
// cost a Tcl round trip to name properly.
func outdatedExcluded(ex sweep.Excluded) upstream.Row {
	detail := ex.Detail
	if ex.Quote != "" {
		detail += "\n" + ex.Quote
	}
	return upstream.Row{
		Portdir: ex.Target.Portdir,
		Port:    upstream.PortName(ex.Target),
		Outcome: upstream.OutcomeExcluded,
		// The declined band, spelled exactly as the write verbs' own
		// excluded row spells it: a port kept out by the filter was
		// judged and not examined, and a row that published code 0
		// would say the opposite in the one field a script reads.
		Twin:   exitcode.Of(exitcode.PlanDeclined, "excluded-"+ex.Reason.String()),
		Detail: detail,
	}
}

// textRow is one line of the human report.
//
// Fixed columns rather than a table computed over the whole run,
// because a paced sweep of a category takes minutes and a reader
// watching it wants to see the answers arrive. Buffering to align them
// would trade the one thing a slow report has going for it — progress
// — for column widths nobody asked about.
func textRow(r upstream.Row) string {
	move := r.Current
	if r.Outcome == upstream.OutcomeOutdated && r.Latest != "" {
		move = r.Current + " -> " + r.Latest
	}
	sha := r.Sha
	if len(sha) > 12 {
		// git's own abbreviation length, so a sha printed here can be
		// pasted at a git that will resolve it.
		sha = sha[:12]
	}
	detail := strings.ReplaceAll(r.Detail, "\n", "\n"+strings.Repeat(" ", 4))
	return strings.TrimRight(
		fmt.Sprintf("%-11s %-30s %-26s %-13s %s", r.Outcome, r.Port, move, sha, detail), " ")
}

// Outdated builds the outdated subcommand.
func Outdated() *cobra.Command {
	var a outdatedAction
	c := &cobra.Command{
		Use:   "outdated [port|category|portdir|maintainer:me|all ...]",
		Short: "Ask upstream what it has, for one port or a whole selector",
		Long: `Ask upstream what it has.

Witnesses are staged by cost: one git ls-remote for every port that
names a forge, and the port's own livecheck and the forge's releases
feed only for the ports whose tags show something newer. Observations
are cached with a TTL, requests to one host are spaced and capped, and
a host that refuses dockhand is left alone for a while — a selector's
worth of update checks lands on one forge, and the report is not worth
having at the price of being blocked by it.

The report goes to stdout and the census to stderr. Exit is 0 whether
or not anything is outdated; 83 says some ports could not be examined
at all.`,
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			a.args = args
			return a, nil
		}),
	}
	c.Flags().IntVarP(&a.workers, "workers", "j", min(8, runtime.NumCPU()),
		"evaluator pool size")
	c.Flags().BoolVarP(&a.all, "all", "a", false,
		"ask about the entire tree")
	c.Flags().BoolVar(&a.deep, "deep", false,
		"run every port's livecheck, not only the candidates the forge flags")
	c.Flags().BoolVar(&a.current, "current", false,
		"list the ports that are up to date as well")
	c.Flags().BoolVar(&a.asJSON, "json", false,
		"one JSON object per port on stdout")
	c.Flags().BoolVar(&a.noCache, "no-cache", false,
		"ask upstream even where a recent observation is held")
	c.Flags().DurationVar(&a.ttl, "ttl", defaultTTL,
		"how long a cached observation stands")
	c.Flags().DurationVar(&a.pace, "pace", 0,
		"minimum gap between two requests to one host (default 500ms)")
	return c
}
