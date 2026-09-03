package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/runstate"
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
	// resolve answers what the command line could not answer before the
	// run existed. Only bump has one: --latest is a question for the
	// forge, and it is settled here so that no intent ever sees the
	// word.
	resolve func(ctx context.Context, rs *runstate.Context, h port.Handle, f *portfetch.Fetcher, p *intent.Params) error
}

var _ Action = intentAction{}

func (a intentAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs, false, []string{a.params.Target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("%s takes exactly one port; %q names %d", a.def.Name, a.params.Target, len(targets))
	}
	ev, err := rs.Evaluator(ctx)
	if err != nil {
		return err
	}
	root, err := rs.TempDir()
	if err != nil {
		return err
	}
	h := port.New(targets[0], ev).WithTempDir(root)

	// The fetcher is acquired only for planners that read the network,
	// and handed on as the interface — nil stays a nil interface, not a
	// typed nil in disguise.
	var pf *portfetch.Fetcher
	var df distfile.Fetcher
	if a.def.Fetches {
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
	if a.resolve != nil {
		if err := a.resolve(ctx, rs, h, pf, &params); err != nil {
			return err
		}
	}
	planner, err := a.def.New(params)
	if err != nil {
		return err
	}
	p, err := planner.Plan(ctx, h, df)
	if err != nil {
		return a.sayDecline(rs, err)
	}

	// The summary comes first whatever happens next: when the plan is
	// about to be realized, this is the only chance to see what is
	// being done before it is done.
	render.RenderPlan(rs.Err, p)
	if a.def.Caution != "" {
		fmt.Fprint(rs.Err, a.def.Caution)
	}
	opts := a.opts
	eng := rs.Deps()
	if a.verify {
		// Before realizing, not after: a Portfile known not to build
		// never becomes a branch or lands in a tree. A pass carries into
		// the realization — the verdict is about these exact bytes, so
		// the branch records it rather than building them again.
		proof, err := eng.VerifyPlan(ctx, p, opts.On, opts.Test)
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
	return eng.Run(ctx, p, opts)
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
	// realization flags in hand as well, because --force is one switch
	// spelling two meanings: replace the in-flight branch, and re-derive
	// the port from scratch. A verb with no flags of its own leaves this
	// nil.
	Flags func(c *cobra.Command, p *intent.Params, f *intentFlags) func() error
	// Resolve fills in what the command line could not, with the run's
	// handle and fetcher in hand. Only bump has one.
	Resolve func(ctx context.Context, rs *runstate.Context, h port.Handle, f *portfetch.Fetcher, p *intent.Params) error
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

// intentCommand builds one verb's command. Every intent takes one port
// and shares the realization flags, so the argument sketch, the arity
// check, the flag set and the road they all travel are written once.
func intentCommand(v intentVerb) *cobra.Command {
	var (
		f      intentFlags
		params intent.Params
		check  func() error
	)
	c := &cobra.Command{
		Use:     v.Name + " <port|subport|portdir>",
		Aliases: v.Aliases,
		Short:   v.Short,
		Args:    exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			// The verb's own contradictions first: a --to that fights
			// --latest is a plainer thing to be told than a --trace that
			// fights --plan, and the caller who typed both is owed the
			// nearer answer.
			if check != nil {
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
			return intentAction{
				def: v.Definition, params: params,
				opts: f.opts, verify: f.verifyIt, resolve: v.Resolve,
			}, nil
		}),
	}
	if v.Flags != nil {
		check = v.Flags(c, &params, &f)
	}
	// Shared by every intent, because every change may close a ticket
	// and the trailer is written at mint whatever the verb was.
	c.Flags().StringVar(&params.ClosesTicket, "closes", "",
		"Trac ticket number this change closes; becomes a Closes: trailer in the commit")
	f.register(c)
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
	detail, remedy, ok := declineFacts(err)
	if !a.opts.PlanOnly || !ok {
		return err
	}
	doc := declineDocument{Exit: declineExit{
		Twin:   TwinOf(err),
		Detail: detail,
		Remedy: remedy,
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
func declineFacts(err error) (detail, remedy string, ok bool) {
	var p *plan.Decline
	if errors.As(err, &p) {
		return p.Detail, p.Type.Remedy(), true
	}
	var s *portstyle.Decline
	if errors.As(err, &s) {
		return s.Field.String(), s.Remedy(), true
	}
	return "", "", false
}

// intentFlags declares the realization flags every write intent
// shares, returning the bound options and the pre-realization verify
// switch.
type intentFlags struct {
	opts     engine.Policy
	on       string
	verifyIt bool
	// force and noVerify are bound to cobra by address and mapped into
	// the policy by check: what the engine takes is a choice with a
	// name, and a bool is what a flag is.
	force    bool
	noVerify bool
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
	c.Flags().BoolVar(&f.force, "force", false,
		"replace an in-flight branch (canceling its verification) and re-derive the port from scratch")
	c.Flags().BoolVar(&f.opts.Test, "test", false,
		"also run the port's test suite (`port test`) in the verification environment")
	c.Flags().BoolVar(&f.opts.Trace, "trace", false,
		"stay attached after submitting: stream the build log until it finishes")
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
	}
	if f.force {
		f.opts.OnInFlight = engine.Replace
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
