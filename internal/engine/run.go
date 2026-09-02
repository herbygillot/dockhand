package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// InFlight is what a realization does about a branch already standing
// for this port. Refusing is the default because the standing branch
// may carry work; replacing is the user asking for it by name.
type InFlight int

const (
	Refuse InFlight = iota
	Replace
)

// Destination is how far one invocation's contract reaches.
// ToVerification is the default — mint the branch AND submit its
// verification — and ToBranch narrows it to the branch alone, which is
// what --no-verify asks for. Nothing persists it yet; the note learns
// about a destination with schema 3.
type Destination int

const (
	ToVerification Destination = iota
	ToBranch
)

// Policy is one invocation's choice of realization, shared by every
// intent that writes: print the plan, print the diff, edit in place,
// or — the default — mint the branch and submit verification.
type Policy struct {
	PlanOnly bool
	Diff     bool
	InPlace  bool
	Trace    bool
	Test     bool
	// OnInFlight is --force: replace what is already in flight for this
	// port, rather than refusing.
	OnInFlight InFlight
	// Destination is --no-verify: stop at the branch.
	Destination Destination
	// On is the release to verify on, already parsed by the caller;
	// the zero value means the provider default. Flag parsing is the
	// CLI's business, not the engine's.
	On platform.Release
	// GateLint is the gate's lint evidence, carried to the minted
	// commit's note so a gate-verified tip reads exactly like a
	// background-verified one.
	GateLint string
	// Verified says the synchronous --verify gate already ran and
	// passed on this plan's content, so realization records the verdict
	// instead of buying the same build twice.
	Verified bool
}

// Run carries a plan to its chosen realization. Every write intent
// arrives here, so a plan becomes a branch the same way whichever
// intent produced it (D21).
func (e *Engine) Run(ctx context.Context, p *plan.Plan, o Policy) error {
	if o.PlanOnly {
		return p.Encode(e.Out)
	}
	if o.Diff {
		return e.diffFromPlan(ctx, p)
	}
	if o.InPlace {
		// The deliberate opt-out (D21): edit where the user stands,
		// uncommitted — for the user running their own workflow, and
		// the only write mode a non-git tree has.
		return e.applyPlan(ctx, p)
	}
	m, err := e.mint(ctx, p, o.OnInFlight == Replace)
	if err != nil || m == nil {
		return err
	}
	if o.Verified {
		// The --verify gate built exactly these bytes — the minted blob
		// and the gate's shadow are both the plan's Materialize over the
		// same base — so the verdict transfers to the commit by content
		// identity.
		// Recording it beats resubmitting: the same build twice proves
		// nothing the first one did not.
		return e.markVerified(ctx, m, p, o.On, o.Test, o.GateLint)
	}
	if o.Destination == ToBranch {
		return nil
	}
	// The branch is live the moment it exists (D21): verification is
	// submitted against the tip and the guest drives its own build, so
	// this process is free to exit; status collects the verdict.
	return e.submit(ctx, m, p.Port, o.On, o.Trace, o.Test)
}

// markVerified writes the minted commit's note as passed, on the
// strength of the pre-mint gate having built identical content.
func (e *Engine) markVerified(ctx context.Context, m *Minted, p *plan.Plan, release platform.Release, tested bool, lint string) error {
	if release.IsZero() {
		// The gate ran, so a provider exists; its default names the run.
		prov, perr := e.Verifier(ctx)
		if perr != nil {
			return perr
		}
		if release, perr = verdict.ResolveRelease(release, prov.Capabilities().Platforms); perr != nil {
			return perr
		}
	}
	return e.recordRun(ctx, m.Repo, m.Sha, p.Port, release.Name, record.Run{State: record.Passed, Tested: tested, Linted: true, Lint: lint},
		fmt.Sprintf("verified before minting; the tip is recorded as passed on %s", release.Name))
}

// applyPlan carries out a plan against the working tree — the
// --in-place realization. Every intent arrives through Run, so a plan
// is executed the same way whichever produced it.
func (e *Engine) applyPlan(ctx context.Context, p *plan.Plan) error {
	ev, err := e.Eval(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Apply(ctx, ev); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "applied: %s %s (%d edits, delta as predicted)\n",
		p.Intent, p.Portdir, len(p.Edits))
	return err
}

// diffFromPlan renders a plan as the patch its branch would carry,
// writing nothing the workspace can see: the edited blob is grafted
// into the base tree exactly as a mint would, and the two trees are
// diffed instead of committed. Repo-relative a/ and b/ paths come out
// correct because the trees carry the full structure.
func (e *Engine) diffFromPlan(ctx context.Context, p *plan.Plan) error {
	if len(p.Edits) == 0 {
		fmt.Fprintln(e.Err, "no edits; nothing to diff")
		return nil
	}
	repo, primary, path, edited, err := e.planOnBase(ctx, p)
	if err != nil {
		return err
	}
	tree, err := repo.GraftTree(ctx, primary, path, edited)
	if err != nil {
		return err
	}
	patch, err := repo.DiffTrees(ctx, primary+"^{tree}", tree)
	if err != nil {
		return err
	}
	// Page the way git diff would: through the user's own pager
	// (GIT_PAGER, core.pager, PAGER — git's chain), and only when
	// stdout is a terminal. diff-tree itself runs behind a pipe, so
	// the pager decision is dockhand's to make at its own stdout.
	if f, ok := e.Out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			if pager := repo.Pager(ctx); pager != "" && pager != "cat" {
				return git.RunPager(ctx, pager, patch, e.Out, e.Err)
			}
		}
	}
	_, err = e.Out.Write(patch)
	return err
}
