package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/staging"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/bump"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/intent/refresh"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/report"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/sweep"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// The three write intents, built from a catalogue so the shared flags
// are declared once and each verb contributes only its own. A fourth
// intent is a fourth entry in intentCatalogue and a package under
// internal/intent — not a fourth hand-written cobra constructor, whose
// flag validation, caution and fetch behaviour would be three more
// places to get subtly wrong.
//
// EVERY INTENT TAKES EXACTLY ONE SELECTOR. `all` is a grammar token and
// not a flag, and a caller wanting two categories runs the verb twice —
// which is also the only way the two get their own exit statuses. Arity
// is not known until the selector resolves, so two OPERATIONS live
// behind one command line: one port is app.Change and more than one is
// app.Survey, with its own exit partition.

// intentVerb is one row of the catalogue: the kit's own Definition, and
// the three things a cobra command needs that a Definition has no
// business carrying — the one-line help, the verb's own flags, and the
// resolution only a live run can perform.
type intentVerb struct {
	intent.Definition
	Short string
	// Flags declares the verb's own flags, binding them straight to the
	// parameters they become, and returns the check for the combinations
	// only this verb can judge.
	Flags func(c *cobra.Command, p *intent.Params) func() error
	// Resolve fills in what the command line could not, with a planner
	// in hand. Only bump has one: --latest is a question for the forge,
	// and it is settled here so that no intent ever sees the word.
	Resolve func(ctx context.Context, s *Services, w io.Writer, target tree.Target, p *intent.Params, m upstream.Manners) error
	// Plural declares the verb's cohort mode: `bump-revision --for
	// <branch>`, which takes no port argument at all. Only
	// bump-revision has one, and that is a property of the intents
	// rather than of this shape — a cohort is a set of revision bumps
	// answering one measurement, and a plural bump would be several
	// unrelated changes sharing a branch.
	Plural func(c *cobra.Command, f *intentFlags) func() (bool, error)
}

// intentCatalogue is every write intent dockhand offers, in the order
// they are registered and therefore the order they are shown.
//
// A function and not a variable: each command owns the flag storage its
// Params are parsed into, so two command trees in one process — which is
// what the test suite is — must not share a --to.
func intentCatalogue() []intentVerb {
	return []intentVerb{bumpVerb(), bumpRevisionVerb(), refreshVerb()}
}

// bumpVerb moves a port to a new version. It carries the full shared set
// plus its own TWO — --to and --latest.
//
// --recalc IS GONE, and the argument for the deletion is worth keeping
// where a reader will meet it: `refresh-checksums` was already the verb
// for a re-derivation and is strictly better at it. REACH — it needs no
// version-literal location, so it works on the computed-version ports
// the bump planner declines as NotLiteral, which a flag on that planner
// could never touch. VOICE — every refresh summary prints that the
// checksums changed at an UNCHANGED version and that somebody must
// establish why before the change goes anywhere public, where the flag
// road performed the same edit silently, in the one moment this tool
// should be loudest. COVERAGE — it regenerates vendored blocks too, so
// nothing was lost in the move. --trace is gone with it, to `dockhand
// log`: an intent makes a change and may stay until it knows, and
// watching one happen is log's job.
func bumpVerb() intentVerb {
	return intentVerb{
		Definition: intent.Definition{
			Name:    "bump",
			Fetches: true,
			New: func(p intent.Params) (intent.Planner, error) {
				return bump.Bump{Version: p.Version, Tools: p.Tools,
					ClosesTicket: p.ClosesTicket, Riders: p.Riders, Dependents: p.Dependents}, nil
			},
		},
		Short: "Bump a port to a new version, as a branch",
		Flags: func(c *cobra.Command, p *intent.Params) func() error {
			c.Flags().StringVar(&p.Version, "to", "", "the version to bump to")
			c.Flags().BoolVar(&p.Latest, "latest", false, "resolve and bump to the newest upstream release (the default)")
			return func() error {
				switch {
				case p.Version != "" && p.Latest:
					return usagef("--to and --latest are mutually exclusive")
				case p.Version == "latest":
					// The literal string would be planned as a version;
					// resolving the newest release is a different workflow.
					return usagef("use --latest to resolve the newest release")
				}
				return nil
			}
		},
		Resolve: resolveLatest,
	}
}

// bumpRevisionVerb increments a port's revision for a stated reason. The
// edit is trivial; the reason is the part only a human has, so the flag
// is required — and it becomes the commit message, because why users
// must rebuild is exactly what the log should say.
func bumpRevisionVerb() intentVerb {
	return intentVerb{
		Definition: intent.Definition{
			Name:    "bump-revision",
			Aliases: []string{"revbump"},
			New: func(p intent.Params) (intent.Planner, error) {
				return bumprevision.BumpRevision{Reason: p.Reason, ClosesTicket: p.ClosesTicket,
					Riders: p.Riders, Dependents: p.Dependents}, nil
			},
		},
		Short: "Increment a port's revision (requires --reason)",
		Flags: func(c *cobra.Command, p *intent.Params) func() error {
			c.Flags().StringVar(&p.Reason, "reason", "", "why users must rebuild (required; becomes the commit message)")
			return func() error {
				if p.Reason == "" {
					return usagef("a revision bump needs --reason: it says why users must rebuild")
				}
				return nil
			}
		},
		Plural: cohortMode,
	}
}

// refreshCaution is printed with every refresh summary. The intent
// applies like any other — the user asking is the human in the loop —
// but a checksum that moves at an unchanged version means upstream
// re-rolled the artifact: possibly a benign re-tar, possibly a
// supply-chain event, and the edit cannot tell you which.
const refreshCaution = "note: these checksums changed at an UNCHANGED version — upstream re-rolled\n" +
	"the artifact. Establish why before this change goes anywhere public: it may\n" +
	"be a benign re-tar, or it may be a supply-chain event.\n"

// refreshVerb makes a port's recorded checksums true again at its
// unchanged version. IT IS THE ONLY ROAD TO A RE-DERIVATION now that
// `bump --recalc` was dropped into it — see bumpVerb for the three
// counts it won on.
func refreshVerb() intentVerb {
	return intentVerb{
		Definition: intent.Definition{
			Name:    "refresh-checksums",
			Aliases: []string{"refresh"},
			Fetches: true,
			Caution: refreshCaution,
			New: func(p intent.Params) (intent.Planner, error) {
				return refresh.Refresh{ClosesTicket: p.ClosesTicket, Riders: p.Riders,
					Dependents: p.Dependents}, nil
			},
		},
		Short: "Re-fetch a port's distfiles and repair its recorded checksums",
	}
}

// resolveLatest answers --latest: what the newest upstream release is,
// asked once, before any planner sees the word.
//
// It says what it resolved on the writer it is HANDED rather than on
// stderr, because where that sentence goes depends on how many ports are
// being asked about: one port says it to the user, and a sweep of four
// hundred would say it four hundred times from four hundred goroutines.
// The Manners it is handed decide how hard the forge is asked, for the
// same reason.
func resolveLatest(ctx context.Context, s *Services, w io.Writer, target tree.Target, p *intent.Params, m upstream.Manners) error {
	if p.Version != "" {
		return nil
	}
	ev, err := s.Eval()
	if err != nil {
		return err
	}
	h := portHandle(target, ev, s)
	resolved, rep, err := bump.ResolveLatest(ctx, s.Tools, h, s.fetch, upstream.GhRunner(s.Forge), m)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "latest: %s (%s)\n", resolved, rep.Verdict)
	p.Version = resolved
	return nil
}

// intentArgSketch is the one argument every write intent takes, with the
// grammar's own forms spelled out because a user who only ever sees
// `<port>` never learns that the verb sweeps.
const intentArgSketch = "<port|subport|portdir|category:x|maintainer:handle|all>"

// intentCommands builds the catalogue's cobra commands.
func intentCommands(s *Services) []*cobra.Command {
	verbs := intentCatalogue()
	cmds := make([]*cobra.Command, 0, len(verbs))
	for _, v := range verbs {
		cmds = append(cmds, intentCommand(s, v))
	}
	return cmds
}

// intentCommand builds one verb's command.
func intentCommand(s *Services, v intentVerb) *cobra.Command {
	var (
		f      intentFlags
		params intent.Params
		check  func() error
		plural func() (bool, error)
	)
	// The plural invocation names a branch and every member on it, so it
	// takes no port. The arity check asks the FLAGS before it counts
	// arguments, which is the one thing cobra's own ExactArgs cannot do.
	arity := func(cmd *cobra.Command, args []string) error {
		if plural != nil {
			if on, err := plural(); on || err != nil {
				return noArgs(cmd, args)
			}
		}
		return exactArgs(1)(cmd, args)
	}
	c := &cobra.Command{
		Use:     v.Name + " " + intentArgSketch,
		Aliases: v.Aliases,
		Short:   v.Short,
		Args:    arity,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The cohort mode first, because it answers a different question
			// with different parameters: the ports it changes come from a
			// proposal and the reason from the measurement, so the verb's own
			// --reason check below would be asking for a justification the
			// branch already carries.
			if plural != nil {
				on, err := plural()
				if err != nil {
					return err
				}
				if on {
					return runAccept(cmd.Context(), s, &f)
				}
			}
			// The verb's own contradictions first: a --to that fights
			// --latest is a plainer thing to be told than a --wait that
			// fights --plan, and the caller who typed both is owed the
			// nearer answer. Under --riders they are moot and skipped rather
			// than answered — the verb's parameters are not read by a
			// housekeeping change, so a revbump's required --reason would be
			// a demand for a justification of an edit nobody is making.
			if check != nil && !f.riders {
				if err := check(); err != nil {
					return err
				}
			}
			ticket, err := checkTicket(params.ClosesTicket)
			if err != nil {
				return err
			}
			if err := f.check(); err != nil {
				return err
			}
			params.Target, params.ClosesTicket = args[0], ticket
			params.Riders = f.riderPolicy()
			params.Tools = s.Tools
			return runIntent(cmd.Context(), s, v, params, &f)
		},
	}
	if v.Flags != nil {
		check = v.Flags(c, &params)
	}
	// After the verb's own flags, because the cohort mode reads the
	// shared set: --no-verify and --on mean the same thing on both roads,
	// and a plural mode with its own spellings would be two vocabularies
	// for one question.
	f.register(c)
	if v.Plural != nil {
		plural = v.Plural(c, &f)
	}
	// Shared by every intent, because every change may close a ticket and
	// the trailer is written at mint whatever the verb was.
	c.Flags().StringVar(&params.ClosesTicket, "closes", "",
		"Trac ticket number this change closes; becomes a Closes: trailer in the commit")
	return c
}

// checkTicket holds --closes to a Trac ticket number and hands back the
// bare number, with the leading hash a hand types accepted and dropped.
//
// It is checked at the boundary rather than rendered leniently later
// because the value becomes a URL in a commit message, and a commit is
// the one thing dockhand writes that nothing rewrites: a trailer
// pointing at https://trac.macports.org/ticket/see-the-PR is worse than
// a refusal one second before it.
func checkTicket(ticket string) (string, error) {
	if ticket == "" {
		return "", nil
	}
	n := strings.TrimPrefix(ticket, "#")
	if n == "" || strings.TrimLeft(n, "0123456789") != "" {
		return "", usagef("--closes takes a Trac ticket number: %q", ticket)
	}
	return n, nil
}

// intentFlags declares the realization flags every write intent shares.
//
// THE DEPTH QUESTION IS ONE FLAG, --no-verify, and the road behind it is
// always enqueue, opportunistically start, detach always. The shipped
// bare --verify went with the Gated delivery; --trace went to `log`;
// --wait arrived as the only way for a caller to stay.
type intentFlags struct {
	planOnly bool
	diff     bool
	inPlace  bool
	noVerify bool
	toPR     bool
	replace  bool
	test     bool
	keepEnv  bool
	riders   bool
	noRiders bool
	// noFetch declines the network round trip that keeps a change based
	// on upstream's newest tip. Spelled as a REFUSAL because fetching is
	// the default: a branch cut from a stale base is the defect, and a
	// person who wants the stale one — offline, on a plane, pinning a
	// reproduction to a known commit — is the one making the unusual ask.
	noFetch bool
	on      string
	wait    time.Duration
	waitSet bool

	// The cohort mode's three, declared here rather than in a struct of
	// their own because the arity check reads them beside the shared set:
	// `--for` decides whether this invocation takes a port argument at
	// all, and that question is asked of ONE value.
	forBranch     string
	exclude       []string
	forceWithheld []string

	release platform.Release
}

// register declares the shared realization flags on a command.
func (f *intentFlags) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.planOnly, "plan", false, "emit the plan on stdout as JSON and change nothing")
	c.Flags().BoolVar(&f.diff, "diff", false,
		"print the patch the branch would carry, as a git diff; write nothing")
	c.Flags().BoolVar(&f.inPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&f.noVerify, "no-verify", false,
		"mint the branch and ask for no build at all")
	c.Flags().BoolVar(&f.noFetch, "no-fetch", false,
		"base the change on your local primary branch without fetching upstream first")
	c.Flags().DurationVar(&f.wait, "wait", 0,
		"stay through the build without showing the log; watching one happen is dockhand log --trace")
	c.Flags().BoolVar(&f.toPR, "to-pr", false,
		"carry the change through to a pull request")
	c.Flags().BoolVar(&f.replace, "replace", false,
		"replace this port's in-flight branch, canceling its verification")
	c.Flags().BoolVar(&f.riders, "riders", false,
		"make housekeeping the whole change: plan the riders alone and drop what the verb would have done")
	c.Flags().BoolVar(&f.noRiders, "no-riders", false,
		"carry no housekeeping riders, and withhold none when there is nothing else to do")
	c.Flags().BoolVar(&f.test, "test", false,
		"also run the port's test suite (port test) in the verification environment")
	c.Flags().BoolVar(&f.keepEnv, "keep-env", false,
		"keep the verification environment after a pass, as a failure keeps its own")
	c.Flags().StringVar(&f.on, "on", "", "macOS release to verify on (one release)")
	// Read in a PreRun so the road below can tell "--wait 0" from "no
	// --wait": ChangeRequest.Wait is a POINTER precisely so that those are
	// two values — an expiry DETACHES and never fails, so a timeout is the
	// caller giving up on watching and never a verdict — and a duration
	// flag alone cannot say which of the two was typed.
	c.PreRun = func(cmd *cobra.Command, _ []string) { f.waitSet = cmd.Flags().Changed("wait") }
}

// riderPolicy is the pair of switches read as the one choice they are.
func (f *intentFlags) riderPolicy() intent.RiderPolicy {
	switch {
	case f.riders:
		return intent.RidersOnly
	case f.noRiders:
		return intent.RidersNone
	}
	return intent.RidersAlong
}

// check validates the shared combinations at the cobra boundary, and
// resolves what only the command line knows into what an operation
// takes. Flag parsing is this layer's business, not an operation's.
func (f *intentFlags) check() error {
	rides := f.test || f.keepEnv || f.waitSet
	writesNothing := f.noVerify || f.planOnly || f.diff || f.inPlace || f.riders
	switch {
	case f.diff && (f.inPlace || f.planOnly):
		return usagef("--diff is an output mode of its own; combine it with neither --plan nor --in-place")
	case rides && writesNothing:
		// All three ride a build and none of those five produces one:
		// riders never trigger a verification — there is nothing in a
		// housekeeping change for a VM to disagree with — and a mint-only
		// or write-nothing delivery leaves no run to test, no environment
		// to keep and no verdict to stay for.
		return usagef("--test, --keep-env and --wait ride a verification; --no-verify, --plan, --diff, --in-place and --riders each produce none")
	case f.riders && f.noRiders:
		return usagef("--riders and --no-riders are mutually exclusive")
	case f.toPR && (f.planOnly || f.diff || f.inPlace):
		return usagef("--to-pr carries a change to a pull request; it needs the default branch realization")
	case f.toPR && f.noVerify:
		// Not a contradiction of spelling but of meaning, which is why it
		// is said rather than resolved: both write Destination, and they
		// write opposite answers. Silently letting one win would make the
		// destination depend on the order two lines happen to be in.
		return usagef("--no-verify stops the change at the branch and --to-pr carries it to a pull request; ask for one")
	case f.toPR && f.riders:
		return usagef("--riders makes housekeeping the whole change, which is not a change to put in front of reviewers")
	case f.replace && (f.planOnly || f.diff || f.inPlace):
		return usagef("--replace acts on a minted branch; it needs the default branch realization")
	}
	release, err := releaseFlag(f.on)
	if err != nil {
		return err
	}
	f.release = release
	return nil
}

// delivery is the flags read as the ONE choice app.Delivery is. Five
// values, since Gated was deleted with the depth flag that spelled it:
// a change that starts its build immediately and one that queues differ
// in LATENCY and not in ownership, because the branch is minted either
// way.
func (f *intentFlags) delivery() app.Delivery {
	switch {
	case f.planOnly, f.diff:
		return app.Document
	case f.inPlace:
		return app.InPlace
	case f.noVerify, f.riders:
		// A rider changes nothing a build could notice, so a housekeeping
		// branch is minted and left alone rather than costing a guest.
		return app.Branch
	case f.toPR:
		return app.PullRequest
	}
	return app.Enqueue
}

// waitFor is --wait as the request takes it: nil to detach at once, or
// the longest this caller stays. A pointer so that "no wait" and "wait
// zero" are two values — expiry DETACHES and never fails, so a timeout
// is the caller giving up on watching and never a verdict.
func (f *intentFlags) waitFor() *time.Duration {
	if !f.waitSet {
		return nil
	}
	d := f.wait
	return &d
}

// runIntent is the whole of an intent verb's road: resolve the selector,
// acquire what the request declares, plan, prepare, and hand ONE
// operation the result.
//
// ARITY DECIDES WHICH OPERATION, and only arity. One target is
// app.Change and more than one is app.Survey, which are two operations
// with two exit partitions — a sweep puts declines on the quiet side,
// because a sweep over four hundred ports that exited non-zero on forty
// ordinary declines would have every CI wrapper around it wrong.
func runIntent(ctx context.Context, s *Services, v intentVerb, params intent.Params, f *intentFlags) error {
	needs := app.ChangeRequest{Delivery: f.delivery(), Fetches: v.Fetches && params.Riders != intent.RidersOnly}.Needs()
	if err := s.Acquire(ctx, needs); err != nil {
		return err
	}
	// The platform, before anything is minted: the record, the preflight
	// and the guest must be told the same one. See settleRelease.
	if err := settleRelease(ctx, s, f, needs.Verifier); err != nil {
		return err
	}
	res, err := resolveSelector(ctx, s, params.Target)
	if err != nil {
		return err
	}
	if len(res.Targets) == 0 {
		return usagef("%q named no port", params.Target)
	}
	if err := refuseByArity(len(res.Targets), params, f); err != nil {
		return err
	}
	planner := planning.Planner{
		Eval: mustEval(s), Fetch: s.Fetch(), Temp: s.Temp(), Catalog: definitions(),
	}
	if len(res.Targets) == 1 {
		return oneTarget(ctx, s, v, planner, params, f, res.Targets[0])
	}
	return manyTargets(ctx, s, v, planner, params, f, res.Targets)
}

// refuseByArity is the second half of the flag check, and it is the half
// that cannot be asked at parse time: arity is not known until the
// selector resolves, so a flag that means one thing about one port and
// nothing about four hundred is answered HERE.
//
// The list is app.SurveyRequest's own — "refused by arity are --replace,
// --diff, --in-place, --closes, --to, --to-pr, --wait" — and the reason
// each is refused rather than dropped is the same in every case: the
// sweep road carries none of them, so a flag accepted here would be a
// flag the operation silently ignores or, worse, honours by writing the
// opposite of what was asked. --in-place under a selector reached
// app.Survey with Delivery InPlace and minted a committed branch per
// port; --to copied one version literal into every plan. A refusal one
// second before that is the cheapest correction dockhand can offer, and
// every message names the road that DOES support the flag.
//
// --plan is deliberately absent: it is the one write-nothing mode a
// sweep can honour, and it does — see manyTargets, where each target's
// document reaches stdout and the census moves to stderr so the stream
// stays parseable. --no-verify, --test, --keep-env, --riders,
// --no-riders, --on and --latest are carried by SurveyRequest and are
// not asked about here.
//
// It is TOTAL over the count rather than guarded at the call site: a
// single target refuses nothing, and a check whose caller has to
// remember that is a check that grows a second caller without one.
func refuseByArity(n int, params intent.Params, f *intentFlags) error {
	if n <= 1 {
		return nil
	}
	switch {
	case f.toPR:
		// A USAGE error and not a machine gate: a flag that turned a
		// maintainer:me sweep into four hundred pull requests would be the
		// single most expensive typo dockhand could offer.
		return usagef("--to-pr under a selector naming %d ports; name one port, or promote the branches you mean", n)
	case f.inPlace:
		return usagef("--in-place edits one Portfile where it stands; a selector naming %d ports has no one file to edit — name one port", n)
	case f.diff:
		return usagef("--diff prints one branch's patch; a selector naming %d ports would run %d of them into one stream — name one port, or --plan for the whole selector", n, n)
	case f.replace:
		return usagef("--replace replaces one port's in-flight branch; a sweep meets its own standing branches and resumes past them — name one port")
	case f.waitSet:
		return usagef("--wait stays through one build; a sweep over %d ports enqueues and detaches — name one port, or `dockhand status` for the standings", n)
	case params.Version != "":
		return usagef("--to names one port's version; a selector naming %d ports would set every one of them to %q — name one port, or --latest for the whole selector", n, params.Version)
	case params.ClosesTicket != "":
		return usagef("--closes names the ticket one change closes; a selector naming %d ports would put the same trailer on every commit — name one port", n)
	}
	return nil
}

// oneTarget is the single-port road: plan, show, prepare, and app.Change.
func oneTarget(ctx context.Context, s *Services, v intentVerb, planner planning.Planner, params intent.Params, f *intentFlags, target tree.Target) error {
	if v.Resolve != nil && params.Riders != intent.RidersOnly {
		// The zero Manners, which is the single port's: unpaced, uncached,
		// and asking with git's own user agent.
		if err := v.Resolve(ctx, s, s.Err, target, &params, upstream.Manners{}); err != nil {
			return err
		}
	}
	params.Target = target.Portdir
	pl, err := planner.Plan(ctx, v.Name, target, params)
	if err != nil {
		return sayDecline(s, f, err)
	}
	// The summary comes first whatever happens next: when the plan is
	// about to be realized, this is the only chance to see what is being
	// done before it is done.
	report.Plan(s.Err, pl)
	// The caution is a fact about the HEADLINE edit, so it is printed
	// only where the headline was planned: under --riders the verb chose
	// the port and nothing else of it ran, and refresh's caution over a
	// modeline insertion would name a supply-chain event that had not
	// happened, over a change that had not happened.
	if v.Caution != "" && params.Riders != intent.RidersOnly {
		fmt.Fprint(s.Err, v.Caution)
	}
	if f.planOnly {
		return emitPlan(s.Out, pl)
	}
	prepared, err := preparedChange(ctx, s, pl, !f.noFetch)
	if err != nil {
		return err
	}
	if f.diff {
		return emitDiff(ctx, s, prepared)
	}
	if f.inPlace {
		return writeInPlace(s, pl, prepared)
	}
	return changeOne(ctx, s, pl, prepared, f)
}

// changeOne builds and runs app.Change, then renders its typed result
// and returns the band the result computed.
//
// THE ONE LINE OF SEQUENCING THIS PACKAGE IS PERMITTED IS AT THE END OF
// IT, and it is written out rather than hidden: under --to-pr on a host
// with no verifier, app.Promote follows app.Change. It is a DELEGATION
// between two operations and not a road assembled from stages — Change
// never publishes, and Promote is the same operation the `promote` verb
// runs, so what a pull request says, how a fork remote is found and how
// a re-publication converges are all decided in one place. If a second
// line of sequencing ever appears in this package, the operation it
// belongs to is missing.
func changeOne(ctx context.Context, s *Services, pl *plan.Plan, prepared change.Prepared, f *intentFlags) error {
	repo, err := s.Repo()
	if err != nil {
		return err
	}
	st, err := s.State()
	if err != nil {
		return err
	}
	led, err := s.Ledger()
	if err != nil {
		return err
	}
	me := s.Me()
	residency := probeResidency(ctx, repo)
	op := app.Change{
		Repo:      repo,
		Ledger:    led,
		State:     st,
		Stage:     staging.New(repo, s.Temp(), s.session),
		Local:     s.ProposeTree(),
		Verifier:  s.VerifyProvider(),
		Me:        me,
		Residency: residencyFunc(repo),
		Now:       s.Now,
		Progress:  sink{w: s.Err},
	}
	req := app.ChangeRequest{
		Prepared:  prepared,
		Delivery:  f.delivery(),
		Platform:  f.release,
		Test:      f.test,
		KeepEnv:   f.keepEnv,
		Replace:   inFlight(f.replace),
		Prov:      change.Provenance{AskedBy: record.Human, Via: record.MintedSingle, Agent: s.Agent},
		Slug:      pl.Slug,
		Riders:    pl.Riders,
		Wait:      f.waitFor(),
		Residency: residency,
	}
	res, runErr := op.Run(ctx, req)
	report.Change(s.Out, quietWhereNoBuildWasAsked(res, f.delivery()), residency, report.Created)
	if runErr != nil {
		return runErr
	}
	if f.toPR && res.Did == app.Minted && res.Deferred != nil && res.Deferred.Reason == app.NoProvider {
		// THE ONE LINE. On a host that cannot verify there will never be a
		// pass, so nothing will ever publish this change through the
		// machine's slot; the only remaining reading of --to-pr is "publish
		// it now, on the person's authority", which is exactly what
		// app.Promote is.
		if err := promoteAfterChange(ctx, s, res.Ref.Branch()); err != nil {
			return err
		}
	}
	return exitWith(res.Exit())
}

// quietWhereNoBuildWasAsked withdraws the no-provider advisory from a
// result whose caller asked for no build at all.
//
// THE ADVISORY IS A FACT ABOUT THE MACHINE AND --no-verify IS A FACT
// ABOUT THE INVOCATION, and the two reach app through the same nil.
// cli acquires a verifier exactly where Needs says a build may start,
// so --no-verify and --riders wire none; app reads a nil resolver as
// verify.ErrNoProvider, which is correct — a host with no tart wires
// none either — and answers a mint with "unverified; install tart and
// `dockhand verify`". On a machine with tart installed and five
// provisioned bases that sentence is false twice over: it instructs a
// person to install software they already have, and a reader who takes
// it as a diagnosis concludes their VM stack is broken. It is rule 7
// arriving through the wiring rather than through a value.
//
// The premise of the sentence is THIS LAYER'S OWN CHOICE, so this is
// where it is withdrawn, and the withdrawal is total rather than
// conditional on whether a verifier could have been found: asking for
// no build is not a question about the machine, and a mint that was
// asked to stay a mint is reported as one and says nothing further. It
// touches nothing else — the deferral is read for an exit code only on
// a QUEUED result, and a delivery that never enqueued cannot produce
// one.
func quietWhereNoBuildWasAsked(res app.Result, d app.Delivery) app.Result {
	if d == app.Branch && res.Deferred != nil && res.Deferred.Reason == app.NoProvider {
		res.Deferred = nil
	}
	return res
}

// promoteAfterChange is the delegation's body, kept short on purpose:
// everything about a publication belongs to app.Promote, and this hands
// it a branch.
func promoteAfterChange(ctx context.Context, s *Services, branch string) error {
	if branch == "" {
		return nil
	}
	op, err := promoteOp(s)
	if err != nil {
		return err
	}
	res, err := op.Run(ctx, branch, publish.Asks{})
	report.Promotion(s.Out, s.Err, res)
	return err
}

// inFlight maps --replace onto app.InFlight. It is the only flag that
// writes one: Advance and Supersede are chosen by the sweep road and
// never typed.
func inFlight(replace bool) app.InFlight {
	if replace {
		return app.Replace
	}
	return app.Refuse
}

// manyTargets is the sweep road: app.Survey, fed one target at a time by
// a pool that plans and prepares as it goes.
//
// THE POOL IS THE PRODUCER AND THE OPERATION IS THE CONSUMER, which is
// what makes a 400-port sweep resumable. A draft admitted the whole
// selector at once after every target had prepared, and an adversarial
// pass priced it: a bump sweep fetches one target at a time, up to
// twenty minutes each, so nothing was committed for hours and a Ctrl-C
// lost all of it. Here each target is admitted, committed, recorded and
// opportunistically started before the next is looked at, and a rerun
// resumes by meeting its own standing branches (InFlight Advance).
func manyTargets(ctx context.Context, s *Services, v intentVerb, planner planning.Planner, params intent.Params, f *intentFlags, targets []tree.Target) error {
	repo, err := s.Repo()
	if err != nil {
		return err
	}
	st, err := s.State()
	if err != nil {
		return err
	}
	led, err := s.Ledger()
	if err != nil {
		return err
	}
	// The sweep's politeness, assembled once for the whole selector: the
	// pacer, the observation cache and the user agent. One port needs
	// none of it — the zero Manners is the single-target road — and four
	// hundred arriving at one forge in one minute need all of it.
	m, _ := sweepManners(s)
	next := sweepPool(s, v, params, f, targets, m,
		func(ctx context.Context, t tree.Target, p intent.Params) (*plan.Plan, error) {
			return planner.Plan(ctx, v.Name, t, p)
		},
		func(ctx context.Context, pl *plan.Plan) (change.Prepared, error) {
			return preparedChange(ctx, s, pl, !f.noFetch)
		})
	op := app.Survey{
		Repo:     repo,
		Ledger:   led,
		State:    st,
		Stage:    staging.New(repo, s.Temp(), s.session),
		Local:    s.ProposeTree(),
		Verifier: s.VerifyProvider(),
		Me:       s.Me(),
		Now:      s.Now,
		Progress: sink{w: s.Err},
	}
	sw, err := op.Run(ctx, app.SurveyRequest{
		Next:      next,
		Delivery:  f.delivery(),
		Platform:  f.release,
		Test:      f.test,
		KeepEnv:   f.keepEnv,
		Admission: sweepAdmission(),
		InFlight:  app.Advance,
		Prov:      change.Provenance{AskedBy: record.Human, Via: record.MintedSweep, Agent: s.Agent},
	})
	report.Sweep(sweepCensus(s, f), sw, probeResidency(ctx, repo))
	if err != nil {
		return err
	}
	return exitWith(sw.Exit())
}

// sweepPool is manyTargets' producer: one target at a time, resolved,
// planned, SHOWN and prepared, with a plan failure becoming that
// target's decline rather than the sweep's end.
//
// SHOWN IS THE WORD THAT WAS MISSING. --plan is the one write-nothing
// mode a selector can honour — app.Survey answers Delivery Document
// with a Shown row per target and writes nothing anywhere — but a row
// saying "shown" is not a document, and the plan the pool computed was
// dropped on the floor: a `--plan` over a category planned every port,
// discarded every plan, and printed a one-line census of zeroes. The
// plan is emitted HERE, where it exists, because the pool is the only
// place on this road that holds one.
//
// It takes the plan and prepare steps as functions rather than reaching
// for a planning.Planner, so that what this road does with a plan is
// exercisable without a ports tree, an evaluator and a git checkout.
func sweepPool(s *Services, v intentVerb, params intent.Params, f *intentFlags, targets []tree.Target,
	m upstream.Manners,
	planOne func(ctx context.Context, t tree.Target, p intent.Params) (*plan.Plan, error),
	prepareOne func(ctx context.Context, pl *plan.Plan) (change.Prepared, error),
) func(context.Context) (app.Planned, bool) {
	i := 0
	return func(ctx context.Context) (app.Planned, bool) {
		if i >= len(targets) {
			return app.Planned{}, false
		}
		t := targets[i]
		i++
		p := params
		p.Target = t.Portdir
		if v.Resolve != nil && p.Riders != intent.RidersOnly {
			// The sweep's Manners: paced and cached, because four hundred
			// ports arriving at one forge in one minute need all of it.
			if err := v.Resolve(ctx, s, io.Discard, t, &p, m); err != nil {
				return app.Planned{Target: t.Portdir, Decline: err}, true
			}
		}
		pl, err := planOne(ctx, t, p)
		if err != nil {
			return app.Planned{Target: t.Portdir, Decline: err}, true
		}
		if f.planOnly {
			// The document the caller asked for, on the stream --plan
			// promises: one JSON object per target, in selector order, which
			// a consumer reads with a json.Decoder in a loop. The single
			// target's road emits exactly this object for exactly this flag,
			// so the two arities speak one language.
			//
			// And it RETURNS, for the same reason oneTarget returns before
			// its own prepare: --plan changes nothing, so it holds the plan
			// against no base commit and cannot decline for a drift nobody
			// was going to commit over. app.Survey answers a Document
			// delivery with a Shown row and never looks at the Prepared.
			if err := emitPlan(s.Out, pl); err != nil {
				return app.Planned{Target: t.Portdir, Decline: err}, true
			}
			return app.Planned{Target: t.Portdir, Slug: pl.Slug, Riders: pl.Riders}, true
		}
		prepared, err := prepareOne(ctx, pl)
		if err != nil {
			return app.Planned{Target: t.Portdir, Decline: err}, true
		}
		return app.Planned{Target: t.Portdir, Prepared: prepared, Slug: pl.Slug, Riders: pl.Riders}, true
	}
}

// sweepCensus is the stream the sweep's own summary is written to, and
// it moves for one flag only.
//
// Under --plan stdout belongs to the plan documents, so the census — a
// person's line, not a machine's — goes to stderr beside the selector's
// notes and the per-target progress. That is the single road's
// discipline too: there report.Plan writes the human summary to stderr
// and emitPlan writes the document to stdout, and a caller piping
// stdout into jq gets JSON and nothing else on both arities.
func sweepCensus(s *Services, f *intentFlags) io.Writer {
	if f.planOnly {
		return s.Err
	}
	return s.Out
}

// sweepAdmission is run.Admission's two integers, which a sweep cannot
// legally run without: Admission REFUSES its own unset zero, on rule 7,
// so there is no such thing as "no cap" and a value has to come from
// somewhere.
//
// THEY ARE CONSTANTS AND NOT FLAGS, and the surface leaves the spelling
// open. What the numbers bound is the store: MaxQueued is how many
// unsettled attempts the state ref may carry at once, and MaxPerPass is
// how many one invocation may add. A person who wants them typeable gets
// a flag when somebody has tuned them; everyone else gets a bound that
// exists.
func sweepAdmission() run.Admission {
	return run.Admission{Set: true, MaxQueued: 200, MaxPerPass: 50}
}

// prepare turns a plan into the complete file set a change commits,
// against THE BASE COMMIT'S BYTES and never the working file.
//
// The Portfile it holds the plan against is read out of git at the base,
// because a plan is made against the bytes a commit would land on: a
// Portfile a person edited on the primary branch since the plan was made
// is ErrDrift here rather than a commit nobody predicted.
//
// THE BASE IS UPSTREAM'S FRESHLY FETCHED TIP by default, which widens
// what ErrDrift can mean. It used to be this checkout's local primary,
// so drift was always the person's own edit; it can now also be the
// port having moved upstream since they last pulled, which is a drift
// they did nothing to cause and the honest answer either way — the plan
// was made against bytes that are not what the commit would land on.
// The remedy differs, so the sentence names both.
// preparedChange resolves the base this road cuts from and hands the
// plan to the preparer. The base RESOLUTION stays here — it may fetch a
// remote and it decides which commit a road cuts from, both of which are
// the road's — and so does the drift sentence, because a sentinel's
// words belong to whoever met it.
func preparedChange(ctx context.Context, s *Services, pl *plan.Plan, fetch bool) (change.Prepared, error) {
	repo, err := s.Repo()
	if err != nil {
		return change.Prepared{}, err
	}
	base, err := baseOf(ctx, s, repo, fetch)
	if err != nil {
		return change.Prepared{}, err
	}
	rel, err := repo.RelPath(pl.Portdir)
	if err != nil {
		return change.Prepared{}, err
	}
	var ev change.Evaluator
	if s.ev != nil {
		ev = blobEvaluator{ev: s.ev}
	}
	p := prepare.Preparer{Repo: repo, Temp: s.Temp(), Plan: planningFor(s), Eval: ev}
	prepared, perr := p.AtBase(ctx, pl, base, change.TreePath(rel))
	if errors.Is(perr, change.ErrDrift) {
		return prepared, fmt.Errorf("%w; %s", perr, driftRemedy(fetch))
	}
	return prepared, perr
}

// driftRemedy is the sentence that turns drift into a next step, and
// there are two of them because the base moved.
//
// While the base was this checkout's local primary, drift had ONE cause:
// the person had edited the Portfile on that branch since planning, and
// the remedy was to look at their own edit. Basing on upstream's
// freshly fetched tip adds a second, and it is the more likely one —
// the port moved upstream since they last pulled, they did nothing, and
// their working tree is exactly as clean as they think it is. A message
// naming only the first would send them hunting for an edit that does
// not exist.
//
// Both are named rather than guessed between. Telling the two apart
// would mean comparing the working file against both commits and
// deciding which explanation fits, which is a diagnosis this line does
// not need to make: the two remedies are one command each, and a person
// who reads both knows immediately which is theirs.
func driftRemedy(fetched bool) string {
	if !fetched {
		return "the Portfile in your tree is not what the base commit holds — check your own edits on the primary branch"
	}
	return "the Portfile in your tree is not what the base commit holds: either the port moved upstream since you last pulled (`git pull`, then bump again) or you have edits on your primary branch"
}

// baseOf is the commit a change is measured from: this checkout's
// primary branch, with the moment it landed.
func baseOf(ctx context.Context, s *Services, repo *gitRepo, fetch bool) (record.Base, error) {
	rev, err := s.BaseRef(ctx, fetch)
	if err != nil {
		return record.Base{}, err
	}
	sha, err := repo.RevParse(ctx, rev)
	if err != nil {
		return record.Base{}, err
	}
	at, err := repo.CommittedAt(ctx, sha)
	if err != nil {
		return record.Base{}, err
	}
	return record.Base{Sha: sha, CommittedAt: at}, nil
}

// freshPrimary fetches upstream's primary branch and names the
// remote-tracking ref to cut from, so a change is minted on the newest
// tip upstream has rather than on whatever this checkout last pulled.
//
// WHY THE BASE AND NOT THE LOCAL BRANCH. The base is the commit the mint
// makes a parent (change.Commit grafts the file set onto it), so it is
// literally what the pull request will be based on. A checkout a week
// behind produced a branch a week behind, which merges badly, reviews
// against stale neighbours, and is the one thing a maintainer cannot see
// by looking at dockhand's output.
//
// IT MOVES NO LOCAL REF AND READS NO WORKING TREE. git.FetchBranch
// updates one remote-tracking ref and nothing else; the person's own
// primary branch, their checkout and their index are exactly as they
// were. That is what makes this safe to do by default: dockhand cutting
// from a fresher commit than the one checked out costs the person
// nothing, where fast-forwarding their branch for them would be this
// tool reaching into a working tree it promises not to touch.
//
// A FETCH THAT FAILED FALLS BACK AND SAYS SO. Offline, behind a proxy,
// an ssh key not loaded: none of those should stop a bump, and none of
// them may pass silently either — a base quietly older than it claims is
// the shape of defect this tree calls rule 7. The caller reads the error
// as "use what is here"; the sentence is written here because this is
// where the reason is known.
//
// The ahead line is the OTHER half of a warning docs/todo.md files
// against the retire sweep: "origin/master moved past your master by N
// commits; a branch cut from it will carry them". This is now a place
// that condition is created, so this is a place it is said.
func freshPrimary(ctx context.Context, run gh.Runner, repo *gitRepo, w io.Writer, primary string) (string, error) {
	// WHICH REMOTE IS UPSTREAM IS ASKED, NOT ASSUMED. "origin" is a
	// convention: `git clone <your fork>` makes origin the FORK and sets
	// the primary branch to track it, so a base cut from origin would be
	// cut from a copy that may be months behind the project. gh.Upstream
	// asks the forge, which is the only party that knows which repository
	// is the project and which is somebody's copy of it.
	//
	// A CHECKOUT WHOSE UPSTREAM CANNOT BE ESTABLISHED IS TOLD SO AND
	// PLANS ANYWAY, on its local primary branch. That is the ruling: the
	// answer being unavailable — no forge, no network, every remote a
	// fork — is not a reason a person cannot bump a port, and it is not a
	// reason to quietly fetch from whatever remote happened to be first
	// either. gh.Upstream's error carries what it looked for and what to
	// add, so the sentence is worth printing whole.
	remote, _, err := gh.Upstream(ctx, run, repo)
	if err != nil {
		fmt.Fprintf(w, "not fetching: %s\n", err)
		fmt.Fprintf(w, "planning against your local %s, which may be behind\n", primary)
		return "", err
	}
	if err := repo.FetchBranch(ctx, remote, primary); err != nil {
		fmt.Fprintf(w, "could not fetch %s/%s (%s); planning against your local %s, which may be behind\n",
			remote, primary, err, primary)
		return "", err
	}
	ref := remote + "/" + primary
	if n, err := repo.Behind(ctx, primary, ref); err == nil && n > 0 {
		fmt.Fprintf(w, "%s is %s ahead of your %s; the change is cut from %s so it carries them\n",
			ref, commits(n), primary, ref)
	}
	return ref, nil
}

// commits is the count with its noun, for the one sentence that carries
// a number of them.
func commits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// emitPlan writes the plan document --plan asked for.
func emitPlan(w io.Writer, pl *plan.Plan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(pl)
}

// declineDocument is what --plan emits when there is no plan: the
// decline, machine-readable, on the stream the plan would have used.
//
// A caller asking for JSON gets JSON however the run ends. Without it a
// declined --plan writes nothing at all to stdout and leaves the reason
// in an English sentence on stderr, so every consumer of --plan has two
// parsers or one blind spot.
type declineDocument struct {
	Exit declineExit `json:"exit"`
}

// declineExit is the twin with the two things a decline knows that a
// bare exit status does not: what specifically was found, and what to do
// about it. They ride INSIDE the exit object rather than beside it
// because they are the same fact at a finer grain — the reason names the
// kind, the detail names the instance.
type declineExit struct {
	exitcode.Twin
	Detail string `json:"detail,omitempty"`
	Remedy string `json:"remedy,omitempty"`
	// Withheld names the riders this decline held back with it, by rule.
	Withheld []string `json:"withheld,omitempty"`
}

// sayDecline writes the decline document when the caller asked for one
// and returns the error either way. The error still travels: the
// document says what happened and the exit status is what a shell reads,
// and the two are built from the same error so they cannot disagree.
//
// Only --plan gets a document. --diff's stdout is a patch — a stream
// somebody pipes into `git apply` — and giving one flag two output
// languages would break the consumer that trusts it.
func sayDecline(s *Services, f *intentFlags, err error) error {
	detail, remedy, withheld, ok := declineFacts(err)
	if !f.planOnly || !ok {
		return err
	}
	doc := declineDocument{Exit: declineExit{
		Twin: TwinOf(err), Detail: detail, Remedy: remedy, Withheld: withheld,
	}}
	enc := json.NewEncoder(s.Out)
	enc.SetIndent("", "  ")
	if werr := enc.Encode(doc); werr != nil {
		fmt.Fprintf(s.Err, "warning: writing the decline document: %v\n", werr)
	}
	return err
}

// declineFacts reads the two things a decline knows that a bare exit
// status does not, from either of the two decline types, and reports
// whether the error is a decline at all.
//
// Both are named here rather than reached for through an interface,
// because they say the same two things in different shapes: a planner's
// decline carries its detail as prose the planner wrote, while a
// location decline's detail IS the field it could not find.
func declineFacts(err error) (detail, remedy string, withheld []string, ok bool) {
	var p *plan.Decline
	if errors.As(err, &p) {
		return p.Detail, p.Type.Remedy(), p.Withheld, true
	}
	var st *portstyle.Decline
	if errors.As(err, &st) {
		// A location decline withholds nothing: it is raised before any
		// rule has been asked, by the layer that could not find a field.
		return st.Field.String(), st.Remedy(), nil, true
	}
	return "", "", nil, false
}

// resolveSelector expands one selector, saying on stderr what the
// grammar decided.
func resolveSelector(ctx context.Context, s *Services, arg string) (sweep.Resolution, error) {
	res, err := sweep.Resolve(ctx, sweep.Sources{
		Tree:  s.Tree,
		Login: forgeLogin(s),
		Email: gitIdentity(s),
	}, []string{arg})
	for _, n := range res.Notes {
		fmt.Fprintln(s.Err, "selector: "+n)
	}
	return res, err
}
