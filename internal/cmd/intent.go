package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// intentAction is the one road every write intent travels, and the
// plan.Planner contract's caller: resolve the target, acquire the
// session, let the planner decide, show the summary, gate, realize.
// What varies between intents is what plans and what the change is
// called — and the plan carries its own naming (Port, Slug, Summary) —
// so the road is one and the intents are data.
type intentAction struct {
	verb    string      // for messages: "bump", "refresh-checksums", …
	target  string      // the port|subport|portdir argument
	opts    realizeOpts // which realization, from the shared flags
	verify  bool        // build in a pristine VM before realizing anything
	fetches bool        // the planner reads the network
	caution string      // printed after the summary, when the intent has one
	// prepare yields the planner, resolving whatever only the command
	// line knows — bump turns "latest" into a version here.
	prepare func(ctx context.Context, h port.Handle, f *portfetch.Fetcher, report io.Writer) (plan.Planner, error)
}

var _ Action = intentAction{}

func (a intentAction) Execute(ctx context.Context, rs *runstate.Context) error {
	targets, err := resolveTargets(rs.TreeRoot, false, []string{a.target})
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

	planner, err := a.prepare(ctx, h, pf, rs.Err)
	if err != nil {
		return err
	}
	p, err := planner.Plan(ctx, h, df)
	if err != nil {
		return err
	}

	// The summary comes first whatever happens next: when the plan is
	// about to be realized, this is the only chance to see what is
	// being done before it is done.
	renderPlan(rs.Err, p)
	if a.caution != "" {
		fmt.Fprint(rs.Err, a.caution)
	}
	opts := a.opts
	if a.verify {
		// Before realizing, not after: a Portfile known not to build
		// never becomes a branch or lands in a tree. A pass carries into
		// the realization — the verdict is about these exact bytes, so
		// the branch records it rather than building them again.
		release, err := releaseFlag(opts.on)
		if err != nil {
			return err
		}
		if err := verifyPlan(ctx, rs, p, release, a.opts.test); err != nil {
			return err
		}
		opts.verified = true
	}
	return realizePlan(ctx, rs, p, opts)
}

// intentFlags declares the realization flags every write intent
// shares, returning the bound options and the pre-realization verify
// switch.
type intentFlags struct {
	opts     realizeOpts
	verifyIt bool
}

// register declares the shared realization flags on a command.
func (f *intentFlags) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.opts.planOnly, "plan", false, "emit the plan on stdout and change nothing")
	c.Flags().BoolVar(&f.opts.diff, "diff", false,
		"print the patch the branch would carry, as a git diff; write nothing")
	c.Flags().BoolVar(&f.opts.inPlace, "in-place", false,
		"edit the Portfile where it stands, uncommitted — no branch, no commit")
	c.Flags().BoolVar(&f.verifyIt, "verify", false,
		"build the result in a pristine VM before realizing it; failure realizes nothing")
	c.Flags().BoolVar(&f.opts.noVerify, "no-verify", false,
		"mint the branch without submitting background verification")
	c.Flags().BoolVar(&f.opts.force, "force", false,
		"replace an in-flight branch (canceling its verification) and re-derive the port from scratch")
	c.Flags().BoolVar(&f.opts.test, "test", false,
		"also run the port's test suite (`port test`) in the verification environment")
	c.Flags().BoolVar(&f.opts.trace, "trace", false,
		"stay attached after submitting: stream the build log until it finishes")
	c.Flags().StringVar(&f.opts.on, "on", "", "macOS release to verify on")
}

// check validates the shared flag combinations at the cobra boundary.
func (f *intentFlags) check() error {
	switch {
	case f.opts.diff && f.opts.inPlace, f.opts.diff && f.opts.planOnly:
		return usagef("--diff is an output mode of its own; combine it with neither --plan nor --in-place")
	case f.verifyIt && f.opts.noVerify:
		return usagef("--verify and --no-verify are mutually exclusive")
	case f.opts.trace && (f.opts.noVerify || f.opts.planOnly || f.opts.diff || f.opts.inPlace):
		return usagef("--trace follows a submitted verification; it needs the default branch realization")
	case f.opts.test && (f.opts.noVerify || f.opts.planOnly || f.opts.diff || f.opts.inPlace):
		return usagef("--test rides a verification; it needs the default branch realization")
	}
	return nil
}

// renderPlan writes the human-facing summary of a plan.
func renderPlan(w io.Writer, p *plan.Plan) {
	target := p.Portdir
	if p.Subport != "" {
		target += " (subport " + p.Subport + ")"
	}
	fmt.Fprintf(w, "plan: %s %s, %d edits\n", p.Intent, target, len(p.Edits))
	for _, e := range p.Edits {
		fmt.Fprintf(w, "  %-16s %s -> %s\n", e.Reason+":", e.Old, e.New)
	}
	fmt.Fprintln(w, "predicted delta:")
	for _, cd := range p.Predicted {
		var parts []string
		for _, ch := range cd.Changes {
			parts = append(parts, renderChange(ch))
		}
		fmt.Fprintf(w, "  %s: %s\n", cd.Subport, strings.Join(parts, "; "))
	}
}

// renderChange keeps the delta line readable: a small change prints in
// full, and a big one — a cargo port's distfiles run to hundreds of
// entries — summarizes to counts. A field run measured the inlined
// form at 87KB on one line, burying the branch and verify lines the
// user actually needed; the full values still live in --plan's JSON.
func renderChange(ch plan.Change) string {
	const inlineMax = 6
	if len(ch.Old) <= inlineMax && len(ch.New) <= inlineMax {
		return fmt.Sprintf("%s %s -> %s",
			ch.Field, strings.Join(ch.Old, " "), strings.Join(ch.New, " "))
	}
	before := map[string]bool{}
	for _, v := range ch.Old {
		before[v] = true
	}
	changed := 0
	for _, v := range ch.New {
		if !before[v] {
			changed++
		}
	}
	return fmt.Sprintf("%s %d -> %d entries (%d new or changed)",
		ch.Field, len(ch.Old), len(ch.New), changed)
}
