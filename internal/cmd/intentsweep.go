package cmd

// The write verbs at selector scale.
//
// One port is a conversation: the plan on stderr, the branch on stdout,
// a sentence about what happens next. Four hundred is a report, and a
// report has to be readable by a program — a sweep that said the same
// things four hundred times would be unreadable by either. So the
// plural road speaks a different language deliberately: one JSON object
// per port on stdout, prose kept to the few things that are true of the
// whole sweep, and a census at the end on stderr where prose belongs.
//
// Rows arrive in completion order and not in the selector's order. The
// evaluation runs on a pool, so which port finishes first is a fact
// about the machine; what is promised is that every target in produces
// exactly one row out, and that the row names the port. A reader who
// wants the tree's order sorts, and gets progress in the meantime
// instead of silence followed by a wall.
//
// The single-port road is untouched by all of it. Arity is the only
// switch, and it is thrown in intentAction.Execute.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/sweep"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// sweepRow is one port's whole answer, and the only thing the plural
// road writes to stdout.
//
// The twin is embedded rather than restated, for the reason
// declineExit embeds it: the code and its family are one fact derived
// one way, and a row that spelled its own family could disagree with
// the process's own exit contract. It also fixes the field order —
// port, outcome, code, family, reason, detail, remedy — so a reader
// scanning the raw stream sees the same shape on every line.
//
// outcome and code answer different questions on purpose. The code says
// what KIND of thing happened, in the vocabulary every other dockhand
// document uses; the outcome says what happened TO THIS PORT in the
// sweep's own words, which is what a script filtering a thousand rows
// actually branches on. "declined" with code 12 and "declined" with
// code 10 are the same decision about the port and different decisions
// about what to do next.
type sweepRow struct {
	Port    string `json:"port"`
	Outcome string `json:"outcome"`
	exitcode.Twin
	Detail string `json:"detail,omitempty"`
	Remedy string `json:"remedy,omitempty"`
}

// The outcome vocabulary, in the order the census reports it. Every row
// carries exactly one of these, and the set is closed: a sweep that
// invented an outcome would break the census's arithmetic, which is the
// only way a reader can tell a complete sweep from a short one.
const (
	// outcomeMinted is a branch that did not exist before.
	outcomeMinted = "minted"
	// outcomeSuperseded is a mint that set the port's older in-flight
	// branch aside. The older branch still exists and still holds what
	// it learned; nothing discards it.
	outcomeSuperseded = "superseded"
	// outcomeAdvanced is the branch this change would have minted
	// already standing. Nothing was written, and nothing is wrong — it
	// is what makes rerunning an interrupted sweep a resume.
	outcomeAdvanced = "advanced"
	// outcomePlanned is --plan: the port was planned and nothing was
	// realized.
	outcomePlanned = "planned"
	// outcomeApplied is --in-place. Unreachable at selector scale,
	// where --in-place is refused, and named anyway so the switch over
	// the engine's realizations stays exhaustive: a realization added
	// later meets a compile error rather than an empty outcome.
	outcomeApplied = "applied"
	// outcomeUnchanged is a plan with no edits.
	outcomeUnchanged = "unchanged"
	// outcomeDeclined is a planner refusing to make a plan it cannot
	// stand behind. The commonest outcome of any real sweep.
	outcomeDeclined = "declined"
	// outcomeExcluded is a port the selector filter kept out before any
	// work was done: retired, or pinned by a person.
	outcomeExcluded = "excluded"
	// outcomeAbandoned is a port the sweep never reached because the
	// evaluator pool died under it.
	outcomeAbandoned = "abandoned"
	// outcomeFailed is everything else: something broke.
	outcomeFailed = "failed"
)

// censusOrder is the order the tail reports outcomes in — fixed, so two
// runs of the same sweep read the same way, and roughly best to worst,
// so the line a reader needs is the last one.
var censusOrder = []string{
	outcomeMinted, outcomeSuperseded, outcomeAdvanced, outcomePlanned, outcomeApplied,
	outcomeUnchanged, outcomeDeclined, outcomeExcluded, outcomeAbandoned, outcomeFailed,
}

// SweepFailedError is a sweep that finished with rows that were not
// declines.
//
// It is the census's exit, and it is a type rather than a count
// returned alongside because the whole contract of the plural road is
// that $? answers without reading anything: 0 when every port was
// either done or declined, 83 when something in there is broken. A
// sweep that exited 0 over four hundred environment failures would say
// the tree was examined when it was not.
type SweepFailedError struct {
	// Hard is how many rows were not declines.
	Hard int
	// Total is how many rows there were.
	Total int
	// First is the first hard row's port, so the message names
	// somewhere to start.
	First string
}

func (e *SweepFailedError) Error() string {
	return fmt.Sprintf("%d of %d ports ended with an error that was not a decline, starting at %s",
		e.Hard, e.Total, e.First)
}

// DockhandExit: the partial band. Ports were swept and some of them
// broke, which is neither success nor a failure of the whole run.
func (e *SweepFailedError) DockhandExit() int { return exitcode.SweepHardErrors }

// Code names the outcome for a machine.
func (e *SweepFailedError) Code() string { return "sweep-hard-errors" }

// hardBand reports whether a per-port exit code means something broke,
// as against a port dockhand looked at and declined to change.
//
// The bands do the deciding, so a code added to one later is already
// classified. Each is ruled on here and nowhere else:
//
//	success, declined  the port was handled. A decline is the
//	                   commonest outcome of a real sweep and is not a
//	                   fault of anything.
//	refused            the destination would not take the change. It
//	                   is about a branch or a pull request, not about
//	                   the sweep, and a mint cannot reach it anyway.
//	upstream           somebody else's problem, per port: a forge that
//	                   would not answer about one port says nothing
//	                   about the other nine hundred, and the remedy is
//	                   to run it again later.
//	pending            nothing failed. A verification deferred for
//	                   want of a slot is exactly what "submit to
//	                   capacity, queue the rest" produces, and it is
//	                   the normal ending for most of a large sweep.
//	verdict            a verification answered and the answer was not
//	                   a pass. No sweep road reaches it — nothing here
//	                   waits for a verdict — and it is a fact about
//	                   the port either way.
//	environment        the machine cannot do the work. It will not do
//	                   the next port either.
//	tree               dockhand was pointed at something that is not
//	                   what the work needs, or the Portfile moved
//	                   under the plan.
//	partial            the operation did half its work. The whole
//	                   point of the band is that a script must be able
//	                   to tell this from nothing having happened.
//	usage, failure     the invocation is wrong, or nothing said whose
//	                   problem it is.
func hardBand(code int) bool {
	switch exitcode.Family(code) {
	case "success", "declined", "refused", "upstream", "pending", "verdict":
		return false
	case "environment", "tree", "partial", "usage", "failure":
		return true
	}
	// A code outside the contract has no family and cannot be argued
	// to be benign.
	return true
}

// census counts a sweep's rows.
type census struct {
	counts map[string]int
	total  int
	hard   int
	first  string
}

func (c *census) add(r sweepRow) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[r.Outcome]++
	c.total++
	if hardBand(r.Code) {
		if c.hard++; c.first == "" {
			c.first = r.Port
		}
	}
}

// String is the tail: the count first, then one line per outcome that
// occurred, in the classify census's own shape because it is the same
// kind of report and a reader should not have to learn two.
//
// Outcomes that did not occur are left out. A tail of nine zeroes
// buries the two numbers that matter, and a sweep's reader is looking
// for the one line that says something went wrong.
func (c *census) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d ports swept\n", c.total)
	for _, name := range censusOrder {
		if n := c.counts[name]; n > 0 {
			fmt.Fprintf(&b, "  %-12s %5d  (%.1f%%)\n", name, n, 100*float64(n)/float64(c.total))
		}
	}
	return b.String()
}

// planned is what the sweep's evaluation half hands to its realization
// half: one target, and either a plan or the reason there is none.
//
// The split is the point. Planning is what the evaluator pool is for
// and runs on its workers; realizing writes to git and to stdout and
// runs on the caller's own goroutine, one port at a time, which is what
// lets a row be written with no mutex and a branch be minted with no
// lock contention over one repository's index.
type planned struct {
	target tree.Target
	plan   *plan.Plan
	err    error
	// abandoned marks a target the pool never reached, which is a row
	// about the sweep rather than about the port.
	abandoned bool
}

// many is the plural road: refuse what only makes sense for one port,
// filter what nobody should touch, then plan and realize the rest.
func (a intentAction) many(ctx context.Context, rs *runstate.Context, res sweep.Resolution) error {
	if err := a.refusePlural(res); err != nil {
		return err
	}
	tr, err := rs.Tree()
	if err != nil {
		return err
	}
	sel, err := sweep.Select(tr, res.Targets)
	if err != nil {
		return err
	}
	targets, named := sweep.CollapseByPortdir(sel.Keep)
	if named != len(targets) {
		// Said, never silent: a user who selected 1072 ports and counts
		// 1000 rows is owed the sentence that says where the other 72
		// went. Two subports of one Portfile are two names and one
		// edit.
		fmt.Fprintf(rs.Err, "%d names in %d portdirs: a branch edits a file, so the sweep works on portdirs\n",
			named, len(targets))
	}
	// The caution belongs to the change and not to the invocation, so
	// it is said once for the sweep rather than once per port: four
	// hundred copies of a supply-chain warning is a warning nobody
	// reads.
	if a.def.Caution != "" {
		fmt.Fprint(rs.Err, a.def.Caution)
	}

	var c census
	var writeErr error
	rows := json.NewEncoder(rs.Out)
	emit := func(r sweepRow) {
		c.add(r)
		if err := rows.Encode(r); err != nil && writeErr == nil {
			writeErr = err
		}
	}
	// The excluded go first and in the order Select returned them,
	// which is the resolution's own order. They are rows like any
	// other: a port left out of a sweep with no line saying so is a
	// port a reader has to notice is missing.
	for _, ex := range sel.Excluded {
		emit(excludedRow(ex))
	}

	// Nothing left to work on is a complete answer, and the excluded
	// rows above are it. No evaluator pool is started and no fetch
	// session is opened for a sweep with no targets.
	var runErr error
	if len(targets) > 0 {
		runErr = a.sweepTargets(ctx, rs, targets, emit)
	}
	fmt.Fprint(rs.Err, c.String())
	if writeErr != nil {
		// A report redirected to a file must not exit 0 over a
		// truncated one.
		return writeErr
	}
	if runErr != nil {
		return runErr
	}
	if c.hard > 0 {
		return &SweepFailedError{Hard: c.hard, Total: c.total, First: c.first}
	}
	return nil
}

// refusePlural turns away the invocations that mean one port and
// cannot mean many.
//
// Every one of them is a flag whose whole content is singular — one
// document, one working tree, one build, one log, one ticket, one
// version — and the refusal is at the boundary because arity is the
// thing that decides it, and arity is not known until the selector has
// been resolved.
func (a intentAction) refusePlural(res sweep.Resolution) error {
	n := len(res.Targets)
	if n == 0 {
		// The arity answer comes first, and to a caller who selected
		// nothing it is the nearer one: a selector that named no port
		// has nothing for any flag to be wrong about, and being told
		// about --replace instead would answer a question behind the
		// one that was asked.
		return usagef("%s takes exactly one port; %q names %d", a.def.Name, a.params.Target, n)
	}
	if a.opts.OnInFlight == engine.Replace {
		// --replace says the same thing in its own terms, because with
		// --replace the arity is not a formality: a category name that
		// expanded to nine ports is nine in-flight branches the user did
		// not picture demolishing.
		return usagef("--replace replaces one port's in-flight branch; %q names %d",
			a.params.Target, n)
	}
	if len(res.Ambiguous) > 0 {
		amb := res.Ambiguous[0]
		return usagef("%q names both the category (%d ports) and the port at %s; say `category:%s` for the sweep or the portdir for the port",
			amb.Token, amb.Category, amb.Port.Portdir, amb.Token)
	}
	switch {
	case a.params.Riders == intent.RidersOnly:
		return usagef("--riders makes housekeeping the whole change, and a housekeeping sweep over %d ports needs a batching strategy nobody has ruled on; name one port", n)
	case a.opts.Diff:
		return usagef("--diff is an output mode of its own; over %d ports the rows on stdout are the report", n)
	case a.opts.InPlace:
		return usagef("--in-place would leave %d uncommitted edits with nothing recording which; a selector mints branches", n)
	case a.verify:
		return usagef("--verify gates one mint on one build; over %d ports it would run them one after another — the minted branches submit their own verification", n)
	case a.opts.Trace:
		return usagef("--trace follows one submitted verification; over %d ports there is no single log to follow — `dockhand status` collects them", n)
	case a.params.ClosesTicket != "":
		return usagef("--closes writes one ticket's trailer onto every commit it makes; %d ports do not close one ticket", n)
	case a.params.Version != "":
		return usagef("--to names one version, and over %d ports the version is each port's own; drop it and the newest release is resolved per port", n)
	}
	return nil
}

// sweepTargets plans every target through the pool and realizes each
// plan as its row arrives.
func (a intentAction) sweepTargets(ctx context.Context, rs *runstate.Context,
	targets []tree.Target, emit func(sweepRow)) error {
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	// The fetcher is one session over one Tcl interpreter and is not
	// safe to share, which is the whole reason a fetching verb sweeps
	// on a single evaluator. It is also the reason to want that: a
	// sweep pulling eight distfiles at once from one host is the abuse
	// nobody asked for, and one at a time is polite by construction.
	// The evaluation-only verb keeps the pool, because its cost is Tcl
	// and Tcl is CPU.
	workers := min(8, runtime.NumCPU())
	var pf *portfetch.Fetcher
	var df distfile.Fetcher
	if a.def.Fetches {
		workers = 1
		if pf, err = rs.Fetcher(ctx); err != nil {
			return err
		}
		df = pf
	}
	p, err := rs.Pool(ctx, workers)
	if err != nil {
		return err
	}
	roster := sweepRoster(rs)
	standing, err := standingBranches(ctx, rs)
	if err != nil {
		return err
	}
	eng := rs.Deps()
	// The politeness the resolution asks upstream under. It is not
	// decoration on this road: `bump maintainer:me` resolves latest for
	// every port it names, which on this tree is a thousand livecheck
	// phases against a thousand unrelated web sites and the best part of
	// a thousand calls to one metered API. Unpaced and uncached that is
	// the abuse the whole S13 ruling exists to prevent, and a report
	// that paced itself while the write verb beside it did not would be
	// politeness in name only.
	manners, cache := sweepManners(rs)

	cfg := sweep.Config[planned]{
		// A version bump fetches a distfile and regenerates its
		// checksums; a minute is the census's bound for a Portfile that
		// hangs, and it is the wrong one here. What must still hold is
		// that one port cannot stall the sweep forever.
		PerTarget: sweepPerTarget(a.def),
		TempDir:   root,
		// A sick evaluator answers everything wrongly from then on, and
		// three in a row is what tells that apart from a run of ports
		// that genuinely decline. The test is the band and not the
		// presence of an error, because two whole classes of failure
		// must never churn the pool: a decline is the port's own
		// judgment, and an upstream refusal is a forge's — replacing a
		// perfectly good interpreter answers neither.
		//
		// What is left over is broader than "the interpreter died": a
		// Portfile that will not parse counts too. That is deliberate
		// and it is classify's own rule, which condemns an evaluator on
		// three consecutive evaluation failures whatever caused them. A
		// false positive costs one respawn; a false negative is a sweep
		// of garbage.
		Broken: func(pl planned) bool { return pl.err != nil && hardBand(ExitCode(pl.err)) },
		Abandon: func(t tree.Target, cause error) planned {
			return planned{target: t, abandoned: true, err: abandonedCause(cause)}
		},
	}
	defer cache.Prune(pruneAge) //nolint:errcheck // Prune returns a count, and housekeeping that could fail a sweep would be worse than housekeeping that does not happen
	return sweep.Run(ctx, cfg, p, targets,
		func(cctx context.Context, h port.Handle) planned {
			return a.planOne(cctx, rs, h, pf, df, roster, manners)
		},
		func(pl planned) {
			// The loop strands a target whose PLAN came back under a
			// dead context, because a result computed there describes
			// the interruption and not the port. Realization is the
			// caller's half of that promise and owes it just as much:
			// the results already buffered when a Ctrl-C lands would
			// otherwise each be minted against a cancelled context,
			// fail, and be counted — a census reading "failed 23" over
			// ports that were interrupted rather than broken, and 23
			// rows the rerun immediately contradicts by minting them.
			if ctx.Err() != nil {
				return
			}
			emit(a.realize(ctx, rs, eng, standing, pl))
		})
}

// sweepPerTarget bounds one port's planning.
//
// A verb that goes to the network is bounded by what it has to
// download, and a large distfile on a slow mirror is minutes; one that
// only evaluates is bounded by Tcl, where the census's minute is
// exactly right and anything longer is a Portfile that has hung.
func sweepPerTarget(def intent.Definition) time.Duration {
	if def.Fetches {
		return 20 * time.Minute
	}
	return sweep.DefaultPerTarget
}

// planOne is the evaluation half, and it runs on a pool worker: it may
// touch nothing the run memoizes and may write to no stream.
//
// The resolution's own sentence — bump's "latest: 1.2.3 (github
// releases)" — goes to a buffer that is dropped. It is one port's news
// on a road that is reporting hundreds, and the version it resolved is
// in the row's detail where a reader can act on it.
func (a intentAction) planOne(ctx context.Context, rs *runstate.Context, h port.Handle,
	pf *portfetch.Fetcher, df distfile.Fetcher, roster func(tree.Target) []string,
	manners upstream.Manners) planned {
	params := a.params
	params.Tools = rs.Tools
	params.Dependents = roster(h.Target)
	if a.resolve != nil {
		var prose bytes.Buffer
		if err := a.resolve(ctx, rs, &prose, h, pf, &params, manners); err != nil {
			return planned{target: h.Target, err: err}
		}
	}
	planner, err := a.def.New(params)
	if err != nil {
		return planned{target: h.Target, err: err}
	}
	p, err := planner.Plan(ctx, h, df)
	return planned{target: h.Target, plan: p, err: err}
}

// realize is the realization half, and it runs on the caller's own
// goroutine, one port at a time: it mints, it submits, and it turns
// what happened into the row.
//
// The engine's prose is caught in a buffer rather than sent to the
// run's streams. On a row that came out clean it is dropped — the row
// says everything — and on a row that broke it is replayed under the
// port's name, because a warning about the one port in four hundred
// that went wrong is exactly the prose worth keeping.
func (a intentAction) realize(ctx context.Context, rs *runstate.Context, eng *engine.Engine,
	standing map[string]bool, pl planned) sweepRow {
	name := targetName(pl.target)
	switch {
	case pl.abandoned:
		// The machine's band, always: the pool died, and it will not
		// have got better for the next port. It is what makes a sweep
		// that quietly skipped four hundred ports exit 83 rather than
		// look like one that examined them.
		return sweepRow{Port: name, Outcome: outcomeAbandoned,
			Twin:   exitcode.Of(exitcode.EvalStartup, "sweep-abandoned"),
			Detail: pl.err.Error()}
	case pl.err != nil:
		return declineOrFailure(name, pl.err)
	case pl.plan == nil:
		return sweepRow{Port: name, Outcome: outcomeFailed,
			Twin:   exitcode.Of(exitcode.Failure, ""),
			Detail: "the planner returned neither a plan nor a reason"}
	}

	opts := a.opts
	// The two answers a sweep gives a standing branch, chosen by which
	// encounter this is. The branch name is the change's own identity
	// — a port's name and what it is moving to — so a branch already
	// carrying it means this exact work is done, and any other branch
	// for the port is older work that this supersedes.
	opts.OnInFlight = engine.Supersede
	if standing[git.MintBranchName(pl.plan.Slug)] {
		opts.OnInFlight = engine.Advance
	}

	var prose bytes.Buffer
	quiet := eng.Deps
	quiet.Out, quiet.Err = &prose, &prose
	done, err := engine.New(quiet).Run(ctx, pl.plan, opts)
	if done.Realization == engine.NotRealized {
		// Nothing was realized and the error is why. It reads exactly
		// like a planning error, because from a row's point of view it
		// is one: the port was not changed, and this is the reason.
		row := declineOrFailure(name, err)
		if hardBand(row.Code) {
			replayProse(rs.Err, name, prose.String())
		}
		return row
	}

	row := sweepRow{Port: name, Twin: TwinOf(err), Detail: pl.plan.Summary}
	switch done.Realization {
	case engine.NotRealized:
		// Answered above; named so the switch stays exhaustive.
	case engine.BranchMinted:
		// The sweep's own minting is the only thing that changes the
		// standing set while it runs, and this is where it changes.
		standing[done.Branch] = true
		row.Outcome = outcomeMinted
		if len(done.Superseded) > 0 {
			row.Outcome = outcomeSuperseded
			row.Detail += " (superseding " + strings.Join(done.Superseded, ", ") + ")"
		}
	case engine.BranchStood:
		row.Outcome = outcomeAdvanced
		row.Detail = done.Branch + " already carries this change"
	case engine.PlanShown:
		row.Outcome = outcomePlanned
	case engine.EditApplied:
		row.Outcome = outcomeApplied
	case engine.NothingRealized:
		row.Outcome = outcomeUnchanged
		row.Detail = "no edits; no branch minted"
	}
	if err != nil {
		if _, remedy, _, ok := declineFacts(err); ok {
			row.Remedy = remedy
		}
		// The error is what happened next, not instead: a branch minted
		// whose verification deferred is a minted branch, and the row
		// says both.
		row.Detail = joinDetail(row.Detail, err.Error())
	}
	if hardBand(row.Code) {
		replayProse(rs.Err, name, prose.String())
	}
	return row
}

// declineOrFailure is one port's planning error as a row: a decline
// when the planner judged, a failure when something broke.
//
// Both tests have to agree before it is called a decline. A decline
// type carries the two things a bare status does not — what was found
// and what to do about it — and the band says whose problem it is; a
// decline whose band was the machine's would be a planner refusing
// because MacPorts is broken, which is not a judgment about the port.
func declineOrFailure(name string, err error) sweepRow {
	detail, remedy, _, ok := declineFacts(err)
	row := sweepRow{Port: name, Outcome: outcomeFailed, Twin: TwinOf(err), Detail: err.Error()}
	if ok && !hardBand(row.Code) {
		row.Outcome, row.Detail, row.Remedy = outcomeDeclined, detail, remedy
	}
	return row
}

// excludedRow is a target the filter kept out, with the evidence.
//
// A retired port is finished and nothing is owed about it. A pinned
// version or a revbump hub is the opposite: a person decided something
// a sweep cannot re-decide, so the row carries the Portfile's own words
// and a remedy that says a human has to look.
func excludedRow(ex sweep.Excluded) sweepRow {
	row := sweepRow{
		Port:    targetName(ex.Target),
		Outcome: outcomeExcluded,
		Twin:    exitcode.Of(exitcode.PlanDeclined, "excluded-"+ex.Reason.String()),
		Detail:  ex.Detail,
		Remedy:  "nothing to do here; the port is retired",
	}
	if ex.Reason.Human() {
		row.Remedy = "a person pinned this; read the quoted comment and name the port on its own to act anyway"
		row.Detail = joinDetail(row.Detail, ex.Quote)
	}
	return row
}

// targetName is the row's identity for a target, and it is the target's
// own and never the plan's.
//
// A row must name the same port whatever happened to it: a port that
// declined before it was ever evaluated has no evaluated name, and a
// column that held the Portfile's `name` for the ports that got that
// far and the directory's for the rest would be two identities in one
// column. The directory is what was pointed at, and a subport is what
// was pointed at when the selector named one.
func targetName(t tree.Target) string {
	if t.Subport != "" {
		return t.Subport
	}
	return filepath.Base(t.Portdir)
}

// joinDetail puts two facts in one detail field without either being
// lost or run together.
func joinDetail(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

// abandonedCause is what an abandoned row says. The pool may have died
// without any replacement having been attempted, which is a nil cause
// and still a target nobody reached.
func abandonedCause(cause error) error {
	const what = "the evaluator pool died before this port was reached"
	if cause == nil {
		return errors.New(what)
	}
	return fmt.Errorf("%s: %w", what, cause)
}

// replayProse writes what the engine said about one port, under that
// port's name, when the row says something went wrong.
func replayProse(w io.Writer, name, prose string) {
	for _, line := range strings.Split(strings.TrimRight(prose, "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(w, "%s: %s\n", name, line)
		}
	}
}

// standingBranches is every dockhand branch in the repository, read
// once for the whole sweep.
//
// It answers one question per port — is the branch this change would
// mint already there — and the answer chooses between the two policies
// a sweep has for a standing branch. Reading it per port would be one
// git invocation per port to learn something nothing but the sweep
// itself changes, and what the sweep mints is added as it goes.
//
// A tree that is not a repository is not an error here: the mint that
// follows will say so per port, in the tree band, which is where that
// answer belongs.
func standingBranches(ctx context.Context, rs *runstate.Context) (map[string]bool, error) {
	repo, err := rs.Repo(ctx)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return nil, err
	}
	standing := make(map[string]bool, len(branches))
	for _, b := range branches {
		standing[b] = true
	}
	return standing, nil
}

// sweepRoster is dependentRoster for a road whose readers are
// goroutines: the reverse index is built at most once, behind a
// sync.Once, rather than by whichever worker asks first. The run's
// memos are plain fields guarded by nothing, and eight workers
// resolving one at the same moment is a data race on the index that
// decides what a finding may say.
//
// It is built lazily for the same reason dependentRoster reads it
// lazily: a full sequential pass over a 41630-entry PortIndex is not
// worth spending on a sweep of ports that carry no revbump comment,
// which is nearly all of them.
func sweepRoster(rs *runstate.Context) func(tree.Target) []string {
	build := sync.OnceValues(func() (portindex.Reverse, error) {
		t, err := rs.Tree()
		if err != nil {
			return portindex.Reverse{}, err
		}
		return t.Dependents()
	})
	return func(target tree.Target) []string {
		path, err := target.Portfile()
		if err != nil {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !intent.MentionsRevbump(src) {
			return nil
		}
		rev, err := build()
		if err != nil {
			// The rule's own fallback is the word list it uses for
			// every ordinary port, and its refusal — stop at the first
			// token you cannot justify — is what a reader finishes by
			// hand. Silent here rather than a line per port: the
			// single-port road says it because there is one port to say
			// it about.
			return nil
		}
		var out []string
		for _, d := range rev.ByPort[strings.ToLower(targetName(target))] {
			out = append(out, d.Name)
		}
		return out
	}
}

// resolveSelector expands the verb's one argument through the sweep
// grammar and says what the grammar decided.
//
// The notes are the maintainer forms' own — which keys were used, how
// many ports each names, near-miss spellings that were left out — and
// they go to stderr because they are prose. Nothing an invocation could
// type before this existed produces one, so no transcript that predates
// selectors moves.
func resolveSelector(ctx context.Context, rs *runstate.Context, arg string) (sweep.Resolution, error) {
	res, err := sweep.Resolve(ctx, sweepSources(rs), []string{arg})
	for _, n := range res.Notes {
		fmt.Fprintln(rs.Err, "selector: "+n)
	}
	return res, err
}

// forgeLogin answers the gh: half of `maintainer:me` through the run's
// own gh seam.
//
// It is wired here rather than reached for inside the grammar because
// this is the composition root: who you are on the forge is a fact
// about the machine, and a selector package that ran gh for itself
// could not be tested without one.
func forgeLogin(rs *runstate.Context) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		login, err := rs.RunGH(ctx, "api", "user", "-q", ".login")
		if err != nil {
			return "", fmt.Errorf("finding your forge handle needs gh: %w (or spell it out)", err)
		}
		return strings.TrimSpace(login), nil
	}
}

// gitIdentity answers the mail: half of `maintainer:me`.
//
// Both halves are asked because neither is complete: on this tree the
// forge handle names 1070 of the maintainer's ports and the mail key
// names 1072, and the two stragglers are ports whose maintainers list
// spells the handle a third way. The mail key is the git identity and
// not the forge's, so it has its own lookup and its own failure.
func gitIdentity(rs *runstate.Context) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		bin, err := rs.Tools.Find(tool.Git)
		if err != nil {
			return "", err
		}
		out, err := tool.Run(ctx, bin, tool.Opts{Args: []string{"config", "--get", "user.email"}})
		if err != nil {
			return "", fmt.Errorf("git config user.email: %w", err)
		}
		return strings.TrimSpace(out), nil
	}
}
