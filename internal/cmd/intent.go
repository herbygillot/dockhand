package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// intentAction is the one road every write intent travels, and the
// plan.Planner contract's caller: resolve the target, acquire the
// session, let the planner decide, show the summary, gate, realize.
// What varies between intents is what plans and what the change is
// called — and the plan carries its own naming (Port, Slug, Summary) —
// so the road is one and the intents are data.
type intentAction struct {
	verb    string        // for messages: "bump", "refresh-checksums", …
	target  string        // the port|subport|portdir argument
	opts    engine.Policy // which realization, from the shared flags
	verify  bool          // build in a pristine VM before realizing anything
	fetches bool          // the planner reads the network
	caution string        // printed after the summary, when the intent has one
	// prepare yields the planner, resolving whatever only the command
	// line knows — bump turns "latest" into a version here.
	prepare func(ctx context.Context, rs *runstate.Context, h port.Handle, f *portfetch.Fetcher) (plan.Planner, error)
}

var _ Action = intentAction{}

func (a intentAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("%s takes exactly one port; %q names %d", a.verb, a.target, len(targets))
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
	if a.fetches {
		if pf, err = rs.Fetcher(ctx); err != nil {
			return err
		}
		df = pf
	}

	planner, err := a.prepare(ctx, rs, h, pf)
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
	if a.caution != "" {
		fmt.Fprint(rs.Err, a.caution)
	}
	opts := a.opts
	eng := rs.Deps()
	if a.verify {
		// Before realizing, not after: a Portfile known not to build
		// never becomes a branch or lands in a tree. A pass carries into
		// the realization — the verdict is about these exact bytes, so
		// the branch records it rather than building them again.
		lint, err := eng.VerifyPlan(ctx, p, opts.On, opts.Test)
		if err != nil {
			return err
		}
		opts.Verified, opts.GateLint = true, lint
	}
	return eng.Run(ctx, p, opts)
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
		f.opts.Destination = engine.ToBranch
	}
	release, err := releaseFlag(f.on)
	if err != nil {
		return err
	}
	f.opts.On = release
	return nil
}
