package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// intentAction is the one road every write intent travels, and the
// intent.Planner contract's caller: resolve the target, acquire the
// session, let the planner decide, show the summary, gate, realize.
// What varies between intents is what plans and what the change is
// called — and the plan carries its own naming (Port, Slug, Summary) —
// so the road is one and the intents are data.
//
// The data is the catalogue's own: a Definition says which planner to
// build and what the verb costs, and Params is everything the command
// line gathered for it. Nothing here branches on which intent is
// running.
type intentAction struct {
	// def is the catalogue entry being run: the name messages use,
	// whether a fetcher must be acquired, the caution to print after the
	// summary, and the constructor for the planner.
	def intent.Definition
	// params is what the command line gathered — complete but for the
	// two fields only a live run can fill: the tool finder, and a
	// version that had to be asked of upstream.
	params intent.Params
	opts   engine.Policy // which realization, from the shared flags
	verify bool          // build in a pristine VM before realizing anything
	// toPR is --to-pr as typed, and road is what the boundary made of it.
	// Two fields because the flag is a fact about the invocation and the
	// road is a fact about this machine and this invoker — see topr.go,
	// where the five rows are.
	toPR bool
	road toPRRoad
	// resolve answers what the command line could not answer before the
	// run existed. Only bump has one: --latest is a question for the
	// forge, and it is settled here so that no intent ever sees the
	// word.
	//
	// It says what it resolved on the writer it is handed rather than
	// on the run's stderr, because where that sentence goes depends on
	// how many ports are being asked about. One port says it to the
	// user; a sweep of four hundred says it four hundred times from
	// four hundred goroutines, which is both a data race and a
	// transcript nobody can read.
	//
	// The Manners it is handed decide how hard the forge is asked, for
	// the same reason: one port needs none of it, and the four hundred
	// that arrive at the same forge in the same minute need all of it.
	// The zero value is the single-port road, so nothing about one
	// invocation changes.
	resolve func(ctx context.Context, rs *runstate.Context, w io.Writer, h port.Handle, f *portfetch.Fetcher, p *intent.Params, m upstream.Manners) error
}

var _ Action = intentAction{}

// Execute resolves the selector and takes one of two roads.
//
// Arity decides, and only arity. A selector that named one port is the
// single-port road exactly as it always was: the same prose on the same
// streams, the same exit codes, no row and no census. That a sweep of
// one is internally the same thing is true and must stay invisible —
// the live proof drives single targets and every golden pins one.
//
// A bare token that names both a category and a port stays ambiguous
// here for the same reason. Where it expands to exactly one port, that
// is what `bump guile` has always meant and it keeps meaning it; the
// refusal the collision deserves belongs to the plural road, where the
// cost of guessing wrong is hundreds of branches.
func (a intentAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// The run's provenance onto the realization, here and once: both
	// roads below take this Action by value, so a single port and a
	// sweep of four hundred record the same declaration, and neither has
	// to remember to ask.
	a.opts.Invoker, a.opts.Agent = rs.Invoker, rs.Agent
	if a.toPR {
		// The boundary first, and before the selector expands: which of
		// --to-pr's two meanings this invocation gets is a fact about the
		// machine and the invoker, and three of the five rows refuse. A
		// refusal that arrived after the mint would leave the user a branch
		// they asked for only as a step toward a publication that was never
		// going to happen.
		road, err := toPRBoundary(rs)
		if err != nil {
			return err
		}
		a.road = road
		if road == toPRQueued {
			// The record's own word for "somebody asked for a pull request",
			// which is what the reconciler's slot reads to know a change is
			// its business at all. Set here rather than at flag-parse time
			// because it is the boundary's conclusion and not the flag's.
			a.opts.Destination = record.ToPublished
		}
	}
	res, err := resolveSelector(ctx, rs, a.params.Target)
	if err != nil {
		return err
	}
	if len(res.Targets) == 1 {
		return a.single(ctx, rs, res.Targets[0])
	}
	if a.toPR {
		return toPRSelectorRefusal(a.params.Target, len(res.Targets))
	}
	return a.many(ctx, rs, res)
}

// single is the one-port road: plan it, show it, realize it.
func (a intentAction) single(ctx context.Context, rs *runstate.Context, target tree.Target) error {
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	h := port.New(target, ev).WithTempDir(root)

	// The fetcher is acquired only for planners that read the network,
	// and handed on as the interface — nil stays a nil interface, not a
	// typed nil in disguise.
	var pf *portfetch.Fetcher
	var df distfile.Fetcher
	if a.def.Fetches && a.params.Riders != intent.RidersOnly {
		if pf, err = rs.Fetcher(ctx); err != nil {
			return err
		}
		df = pf
	}

	// The run's own two contributions to the parameters, on a copy: the
	// action is a value, and an invocation must not leave a resolved
	// version behind in the catalogue it was built from.
	params := a.params
	params.Tools = rs.Tools
	params.Dependents = dependentRoster(rs, target)
	if a.resolve != nil && params.Riders != intent.RidersOnly {
		// The zero Manners, which is the single port's: unpaced,
		// uncached, and asking with git's own user agent, exactly as
		// this road has always asked.
		if err := a.resolve(ctx, rs, rs.Err, h, pf, &params, upstream.Manners{}); err != nil {
			return err
		}
	}
	planner, err := a.def.New(params)
	if err != nil {
		return err
	}
	if params.Riders == intent.RidersOnly {
		// --riders makes housekeeping the whole change: the verb chose the
		// port and nothing else of it is used. This is the one place the
		// road does not run what the catalogue named, and it is still not
		// a branch on WHICH intent — every verb's housekeeping is the same
		// change, which is why there is one planner for it rather than
		// three.
		planner = intent.Housekeeping{}
	}
	p, err := planner.Plan(ctx, h, df)
	if err != nil {
		return a.sayDecline(rs, err)
	}

	// The summary comes first whatever happens next: when the plan is
	// about to be realized, this is the only chance to see what is
	// being done before it is done.
	render.RenderPlan(rs.Err, p)
	// The caution is a fact about the headline edit, so it is printed
	// only where the headline was planned. Under --riders the verb chose
	// the port and nothing else of it ran — no fetch, no checksum, no
	// comparison — and refresh's caution over a modeline insertion named
	// a supply-chain event that had not happened, over a change that had
	// not happened, on the operator's own stream.
	if a.def.Caution != "" && params.Riders != intent.RidersOnly {
		fmt.Fprint(rs.Err, a.def.Caution)
	}
	opts := a.opts
	eng := rs.Deps()
	if a.verify {
		// Before realizing, not after: a Portfile known not to build
		// never becomes a branch or lands in a tree. A pass carries into
		// the realization — the verdict is about these exact bytes, so
		// the branch records it rather than building them again.
		proof, err := eng.VerifyPlan(ctx, p, opts)
		if err != nil {
			return err
		}
		// The whole proof and not the lint summary alone: the minted
		// commit's record names the environment the verdict was earned
		// in, so a gate-verified tip reads exactly like a
		// background-verified one instead of claiming a pass reached
		// nowhere.
		opts.Verified, opts.GateProof = true, proof
	}
	if a.road != toPRImmediate {
		_, err = eng.Run(ctx, p, opts)
		return err
	}
	return a.mintAndPublish(ctx, rs, eng, p, opts)
}

// mintAndPublish is --to-pr's immediate form: the ring-3 questions, then
// the mint, then the human publish road, in one invocation.
//
// The prechecks are MINT-FREE and that is the whole reason they exist
// here rather than being left to promote, which asks both again over the
// branch it is pushing. A change whose own pull request already merged,
// or whose title an open one already proposes, is a change this
// invocation is not going to publish — and learning that after the mint
// would leave a branch behind for a person to find and remove, in the
// merged case pointing at work the project has already taken.
//
// Publication is Promote with the invoker declared human, which is the
// same road `dockhand promote` walks and deliberately not a second one:
// what a pull request says, how a fork remote is found, how a
// re-publication converges on the pull request already open, and the
// audit row it leaves are all decided in one place.
func (a intentAction) mintAndPublish(ctx context.Context, rs *runstate.Context,
	eng *engine.Engine, p *plan.Plan, opts engine.Policy) error {
	repo, err := eng.RepoFor(ctx, p.Portdir)
	if err != nil {
		return err
	}
	if err := eng.PrecheckPublish(ctx, repo, p); err != nil {
		return err
	}
	sayToPRImmediate(rs)
	done, err := eng.Run(ctx, p, opts)
	if err != nil {
		return err
	}
	if done.Realization != engine.BranchMinted {
		// Nothing was minted — an empty plan, or a branch that already
		// stood. There is no new change to put in front of anybody, and
		// publishing the branch that was already there is a promote the
		// person can type if that is what they meant.
		return nil
	}
	return eng.Promote(ctx, repo, done.Branch, engine.PromoteOpts{
		Invoker: record.Human, Closes: a.params.ClosesTicket})
}

// dependentRoster is what the tree's reverse index says depends on this
// port, read only where a finding rule would use it.
//
// The gate is the Portfile itself, and it is a cost gate rather than a
// correctness one. The roster's one reader is the instruction-comment
// rule, which consults it to vouch for a token its word list would
// otherwise stop on — and only for a Portfile that already carries a
// revbump-instruction comment, which a few dozen ports in a 41630-port
// tree do. Building the reverse index is a full sequential pass over
// the PortIndex, so filling this unconditionally would spend that pass
// on every `--plan` of every leaf port to narrow a roster nothing was
// going to read.
//
// An unreadable Portfile or an unindexed tree answers with nothing, and
// that is not a silent loss: the rule's fallback is the word list it
// uses for every ordinary port, and its own refusal — stop at the first
// token you cannot justify, beside a verbatim quote — is what a reader
// finishes by hand. A note is written to stderr where the comment
// exists and the index could not answer, so a roster that came out
// short says why.
func dependentRoster(rs *runstate.Context, target tree.Target) []string {
	path, err := target.Portfile()
	if err != nil {
		return nil
	}
	src, err := os.ReadFile(path)
	if err != nil || !intent.MentionsRevbump(src) {
		return nil
	}
	name := target.Subport
	if name == "" {
		// Resolved by location, so the index never named it. The portdir's
		// basename is the parent port's name, which is the right lookup
		// for the directory being edited — a subport would have arrived
		// with Subport set.
		name = filepath.Base(target.Portdir)
	}
	t, err := rs.Tree()
	if err == nil {
		var rev portindex.Reverse
		if rev, err = t.Dependents(); err == nil {
			var out []string
			for _, d := range rev.ByPort[strings.ToLower(name)] {
				out = append(out, d.Name)
			}
			return out
		}
	}
	fmt.Fprintf(rs.Err, "note: %s carries a revision-bump comment and the reverse index could not be read (%v); the ports it names are read from the comment's own words alone\n",
		filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), err)
	return nil
}

// intentVerb is one row of the write-intent catalogue: the kit's own
// Definition, and the three things a cobra command needs that a
// Definition has no business carrying — the one-line help, the verb's
// own flags, and the resolution only a live run can perform.
//
// The split is where the knowledge is. What an intent IS — its name,
// its aliases, whether it goes to the network, the caution a reader is
// owed — belongs to the kit and travels as data. How a terminal asks
// for it belongs here, and is the only part cobra ever sees.
type intentVerb struct {
	intent.Definition
	// Short is the one-line description in `dockhand --help`.
	Short string
	// Flags declares the verb's own flags on the command, binding them
	// straight to the parameters they become, and returns the check for
	// the combinations only this verb can judge. It has the shared
	// realization policy in hand as well, because a verb's own parameter
	// can imply one: bump's --recheck is a question for the planner, and
	// a re-derivation that has to be built from source is a fact the
	// engine needs. A verb with no flags of its own leaves this nil.
	Flags func(c *cobra.Command, p *intent.Params, f *intentFlags) func() error
	// Resolve fills in what the command line could not, with the run's
	// handle and fetcher in hand. Only bump has one. Its prose goes to
	// the writer it is given, and it asks upstream under the Manners it
	// is given, for the reasons intentAction.resolve states.
	Resolve func(ctx context.Context, rs *runstate.Context, w io.Writer, h port.Handle, f *portfetch.Fetcher, p *intent.Params, m upstream.Manners) error
	// Plural declares the verb's cohort mode: it registers the flags
	// that ask for one and returns the reader of them, which answers
	// with the Action to run instead of the single-port road — or with
	// nil, for the ordinary invocation.
	//
	// Only bump-revision has one, and that is a property of the intents
	// rather than of this shape: a cohort is a set of revision bumps
	// answering one measurement, and what makes it one commit is that
	// every member's edit is the same mechanical edit for the same
	// stated reason. A plural bump would be several unrelated changes
	// sharing a branch.
	//
	// It is what lets the verb take no port argument: the arity check
	// below asks this first, because `bump-revision --for <branch>`
	// names a branch and every member on it, and demanding a port would
	// be demanding the answer the proposal already holds.
	Plural func(c *cobra.Command, f *intentFlags) func() (Action, error)
}

// intentCatalogue is every write intent dockhand offers, in the order
// they are registered and therefore the order they are shown. A fourth
// intent is a fourth entry here and a package under internal/intent —
// not a fourth hand-written cobra constructor, whose flag validation,
// caution and fetch behaviour would be three more places to get subtly
// wrong.
//
// A function and not a variable: each command owns the flag storage its
// Params are parsed into, so two command trees in one process — which
// is what the test suite is — must not share a --to.
func intentCatalogue() []intentVerb {
	return []intentVerb{bumpVerb, bumpRevisionVerb, refreshVerb}
}

// intentCommands builds the catalogue's cobra commands.
func intentCommands() []*cobra.Command {
	verbs := intentCatalogue()
	cmds := make([]*cobra.Command, 0, len(verbs))
	for _, v := range verbs {
		cmds = append(cmds, intentCommand(v))
	}
	return cmds
}

// intentArgSketch is the one argument every write intent takes, and
// the grammar's own forms are spelled out because a user who only ever
// sees `<port>` never learns that the verb sweeps. One argument and not
// several: `all` is only itself, and a caller wanting two categories
// runs the verb twice — which is also the only way the two get their
// own exit statuses.
const intentArgSketch = "<port|subport|portdir|category:x|maintainer:handle|all>"

// intentCommand builds one verb's command. Every intent takes one
// selector and shares the realization flags, so the argument sketch,
// the arity check, the flag set and the road they all travel are
// written once.
func intentCommand(v intentVerb) *cobra.Command {
	var (
		f      intentFlags
		params intent.Params
		check  func() error
		plural func() (Action, error)
	)
	// The plural invocation names a branch and every member on it, so it
	// takes no port. The arity check asks the flags before it counts
	// arguments, which is the one thing cobra's own ExactArgs cannot do.
	arity := func(cmd *cobra.Command, args []string) error {
		if plural != nil {
			if a, err := plural(); a != nil || err != nil {
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
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			// The cohort mode first, because it answers a different
			// question with different parameters: the ports it changes come
			// from a proposal and the reason from the measurement, so the
			// verb's own --reason check below is asking for a justification
			// the branch already carries.
			if plural != nil {
				if a, err := plural(); a != nil || err != nil {
					return a, err
				}
			}
			// The verb's own contradictions first: a --to that fights
			// --latest is a plainer thing to be told than a --trace that
			// fights --plan, and the caller who typed both is owed the
			// nearer answer.
			//
			// Under --riders they are moot, and skipped rather than
			// answered. The verb's parameters are not read by a
			// housekeeping change, so a revbump's required --reason would
			// be a demand for a justification of an edit nobody is making.
			if check != nil && !f.riders {
				if err := check(); err != nil {
					return nil, err
				}
			}
			ticket, err := checkTicket(params.ClosesTicket)
			if err != nil {
				return nil, err
			}
			if err := f.check(); err != nil {
				return nil, err
			}
			params.Target, params.ClosesTicket = args[0], ticket
			params.Riders = f.riderPolicy()
			return intentAction{
				def: v.Definition, params: params,
				opts: f.opts, verify: f.verifyIt, resolve: v.Resolve,
				toPR: f.toPR,
			}, nil
		}),
	}
	if v.Flags != nil {
		check = v.Flags(c, &params, &f)
	}
	// After the shared flags exist, because the cohort mode reads them:
	// --no-verify and --on mean the same thing on both roads, and a
	// plural mode with its own spellings would be two vocabularies for
	// one question.
	f.register(c)
	if v.Plural != nil {
		plural = v.Plural(c, &f)
	}
	// Shared by every intent, because every change may close a ticket
	// and the trailer is written at mint whatever the verb was.
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

// declineDocument is what --plan emits when there is no plan: the
// decline, machine-readable, on the stream the plan would have used.
//
// A caller asking for JSON gets JSON however the run ends. Before
// this, a declined --plan wrote nothing at all to stdout and left the
// reason in an English sentence on stderr, so every consumer of --plan
// had two parsers or one blind spot.
type declineDocument struct {
	Exit declineExit `json:"exit"`
}

// declineExit is the twin with the two things a decline knows that a
// bare exit status does not: what specifically was found, and what to
// do about it. They ride inside the exit object rather than beside it
// because they are the same fact at a finer grain — the reason names
// the kind, the detail names the instance.
type declineExit struct {
	exitcode.Twin
	Detail string `json:"detail,omitempty"`
	Remedy string `json:"remedy,omitempty"`
	// Withheld names the riders this decline held back with it, by rule.
	// The reason already says a decline withheld something; this says
	// what, which is the part a sweep deciding whether to come back with
	// --riders actually needs.
	Withheld []string `json:"withheld,omitempty"`
}

// sayDecline writes the decline document when the caller asked for one
// and returns the error either way. The error still travels: the
// document says what happened and the exit status is what a shell
// reads, and the two are built from the same error so they cannot
// disagree.
//
// Only --plan gets a document. --diff's stdout is a patch — a stream
// somebody pipes into `git apply` — and giving one flag two output
// languages would break the consumer that trusts it.
func (a intentAction) sayDecline(rs *runstate.Context, err error) error {
	detail, remedy, withheld, ok := declineFacts(err)
	if !a.opts.PlanOnly || !ok {
		return err
	}
	doc := declineDocument{Exit: declineExit{
		Twin:     TwinOf(err),
		Detail:   detail,
		Remedy:   remedy,
		Withheld: withheld,
	}}
	enc := json.NewEncoder(rs.Out)
	enc.SetIndent("", "  ")
	if werr := enc.Encode(doc); werr != nil {
		// The decline is the answer and the document is how it was
		// asked for; a stdout that will not take it is worth saying,
		// and worth saying without replacing the reason.
		fmt.Fprintf(rs.Err, "warning: writing the decline document: %v\n", werr)
	}
	return err
}

// declineFacts reads the two things a decline knows that a bare exit
// status does not — what specifically was found, and what to do about
// it — from either of the two decline types, and reports whether the
// error is a decline at all.
//
// Both are named here rather than reached for through an interface,
// because they say the same two things in different shapes: a
// planner's decline carries its detail as prose the planner wrote,
// while a location decline's detail IS the field it could not find.
// Missing the second is how the revision-less Portfile — the most
// common decline in the tree after already-current — ended up as the
// one --plan that still wrote nothing to stdout.
func declineFacts(err error) (detail, remedy string, withheld []string, ok bool) {
	var p *plan.Decline
	if errors.As(err, &p) {
		return p.Detail, p.Type.Remedy(), p.Withheld, true
	}
	var s *portstyle.Decline
	if errors.As(err, &s) {
		// A location decline withholds nothing: it is raised before any
		// rule has been asked, by the layer that could not find a field.
		return s.Field.String(), s.Remedy(), nil, true
	}
	return "", "", nil, false
}

// intentFlags declares the realization flags every write intent
// shares, returning the bound options and the pre-realization verify
// switch.
type intentFlags struct {
	opts     engine.Policy
	on       string
	verifyIt bool
	// replace and noVerify are bound to cobra by address and mapped into
	// the policy by check: what the engine takes is a choice with a
	// name, and a bool is what a flag is.
	//
	// replace was --force until S10. One switch spelled two questions —
	// what to do about a branch already in flight, and whether to
	// re-derive a port at the version it already carries — and a user who
	// wanted the second got the first as a side effect, which on a
	// standing branch is a demolition. They are two flags now: --replace
	// here, and bump's own --recheck.
	replace  bool
	noVerify bool
	// toPR asks for a pull request in the same breath as the change. What
	// it can mean here is the boundary's to say — see topr.go — so nothing
	// but the bare switch is kept at this layer.
	toPR bool
	// riders and noRiders are the two ends of one policy, spelled as two
	// switches because that is what a command line has. riderPolicy maps
	// the pair onto the value the planners take; check refuses the
	// contradiction first, so the mapping never has to.
	riders   bool
	noRiders bool
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

// register declares the shared realization flags on a command.
func (f *intentFlags) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.opts.PlanOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&f.opts.Diff, "diff", false,
		"print the patch the branch would carry, as a git diff; write nothing")
	c.Flags().BoolVar(&f.opts.InPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&f.verifyIt, "verify", false,
		"build the result in a pristine VM before realizing it; failure realizes nothing")
	c.Flags().BoolVar(&f.noVerify, "no-verify", false,
		"mint the branch without submitting background verification")
	c.Flags().BoolVar(&f.replace, "replace", false,
		"replace this port's in-flight branch, canceling its verification and removing its notes")
	c.Flags().BoolVar(&f.riders, "riders", false,
		"make housekeeping the whole change: plan the riders alone and drop what the verb would have done")
	c.Flags().BoolVar(&f.noRiders, "no-riders", false,
		"carry no housekeeping riders, and withhold none when there is nothing else to do")
	c.Flags().BoolVar(&f.toPR, "to-pr", false,
		"carry the change through to a pull request: queued for the reconciler's publish slot where this machine can verify, and published in this invocation where it cannot")
	c.Flags().BoolVar(&f.opts.Test, "test", false,
		"also run the port's test suite (`port test`) in the verification environment")
	c.Flags().BoolVar(&f.opts.Trace, "trace", false,
		"stay attached after submitting: stream the build log until it finishes")
	c.Flags().BoolVar(&f.opts.KeepEnv, "keep-env", false,
		"keep the verification environment after a pass, as a failure keeps its own")
	c.Flags().StringVar(&f.on, "on", "", "macOS release to verify on")
}

// check validates the shared flag combinations at the cobra boundary,
// and resolves what only the command line knows into what the engine
// takes — the parsed release, and the two switches that are choices
// there and flags here: flag parsing is the CLI's business, not the
// engine's.
func (f *intentFlags) check() error {
	switch {
	case f.opts.Diff && f.opts.InPlace, f.opts.Diff && f.opts.PlanOnly:
		return usagef("--diff is an output mode of its own; combine it with neither --plan nor --in-place")
	case f.verifyIt && f.noVerify:
		return usagef("--verify and --no-verify are mutually exclusive")
	case f.opts.Trace && (f.noVerify || f.opts.PlanOnly || f.opts.Diff || f.opts.InPlace):
		return usagef("--trace follows a submitted verification; it needs the default branch realization")
	case f.opts.Test && (f.noVerify || f.opts.PlanOnly || f.opts.Diff || f.opts.InPlace):
		return usagef("--test rides a verification; it needs the default branch realization")
	case f.opts.KeepEnv && (f.noVerify || f.opts.PlanOnly || f.opts.Diff || f.opts.InPlace):
		return usagef("--keep-env keeps a submitted run's environment; it needs the default branch realization")
	case f.opts.KeepEnv && f.verifyIt:
		// The gate verifies synchronously and releases the environment
		// with the verdict, before the branch is minted; there is no
		// submitted run for the ask to ride and nothing to keep. Refused
		// rather than dropped (ruled 2026-09-05 with D27's
		// implementation, pending the maintainer).
		return usagef("the gate releases its environment with the verdict; --keep-env keeps a submitted run's")
	case f.riders && f.noRiders:
		return usagef("--riders and --no-riders are mutually exclusive")
	case f.riders && (f.verifyIt || f.opts.Trace || f.opts.Test || f.opts.KeepEnv):
		return usagef("riders never trigger a verification; there is nothing in a housekeeping change for a VM to disagree with")
	case f.toPR && (f.opts.PlanOnly || f.opts.Diff || f.opts.InPlace):
		return usagef("--to-pr carries a change to a pull request; it needs the default branch realization")
	case f.toPR && f.noVerify:
		// Not a contradiction of spelling but of meaning, which is why it
		// is said rather than resolved: both write Destination, and they
		// write opposite answers. --no-verify says the change stops at the
		// branch and nobody is owed a verdict; --to-pr says it carries on
		// to a pull request. Silently letting one win would make the
		// destination depend on the order two lines happen to be written
		// in.
		return usagef("--no-verify stops the change at the branch and --to-pr carries it to a pull request; ask for one")
	case f.toPR && f.riders:
		return usagef("--riders makes housekeeping the whole change, which is not a change to put in front of reviewers")
	case f.replace && (f.opts.PlanOnly || f.opts.Diff || f.opts.InPlace):
		// --force used to be accepted here and quietly meant the other
		// thing — the re-derivation — which is why the two are apart now.
		// A flag about a branch, given to a realization that mints none,
		// is worth a sentence rather than silence.
		return usagef("--replace acts on a minted branch; it needs the default branch realization")
	}
	if f.replace {
		f.opts.OnInFlight = engine.Replace
	}
	if f.riders {
		// A rider changes nothing a build could notice — that is what the
		// double proof established — so a housekeeping branch is minted
		// and left alone rather than costing a VM. The record's own word,
		// as --no-verify uses it, because the drain reads it back to know
		// that nobody is owed a verdict.
		f.opts.Destination = record.ToBranch
	}
	if f.noVerify {
		// The record's own word, because the engine writes this onto the
		// note at mint: a destination that stops at the branch is a fact
		// about the change and not a mood of this invocation, and the
		// drain reads it back to know that nobody asked for a verdict.
		f.opts.Destination = record.ToBranch
	}
	release, err := releaseFlag(f.on)
	if err != nil {
		return err
	}
	f.opts.On = release
	return nil
}
