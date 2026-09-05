package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// cycleAction is the pass that acts on the world (D27). It runs the
// reconciliation `status` runs — every branch dockhand observes is
// read, finished runs are settled, every pull request is judged — and
// then does what `status` only names: retires the branch of a merged
// pull request, locally and off the fork; reclaims untracked workers
// when asked; starts what was deferred; and, run as the machine, walks
// the publish slot. `clean` folded into it — `cycle` is `clean` plus
// the rest — and `clean`'s --superseded came with it.
//
// One reconciler, two verbs. The sweep and the report reached the same
// verdicts by two code paths once, and the only way to know they agreed
// was to run both; now there is one pass, and the verbs part company
// only in what the pass was allowed to do about what it found. Retire
// precedes the drain, and the drain is last: a branch whose PR merged
// and whose run was deferred must not have its run started and then be
// deleted in the same pass.
//
// Each thing this removes has its own flag, and the flag's shape
// follows the default. What happens unless withheld gets --keep-<x>:
// a merged pull request is GitHub's own word that the work landed, so
// its branch goes by default and --keep-merged withholds it. What
// happens only when asked gets a plain flag: a supersede is dockhand's
// own inference from two branch names, and an untracked worker is an
// environment nobody has characterised, so --superseded and
// --reclaim-orphans are the person saying they meant it. Deletion
// stays in the namespace whatever a pull request did: a hand-made
// branch carrying a verify note is shown and settled and never removed
// here, and its line says so.
//
// THE PUBLISH SLOT IS HANDED IN ONLY UNDER THE MACHINE INVOKER (ruled
// 2026-09-05 with D27's implementation, pending the maintainer). The
// slot is the machine road by construction — every gate it walks asks
// as record.Machine — so a person's `cycle` passing one in would be a
// machine publication typed by a person, the mirror of the
// promote-is-human refusal. `dockhand cycle --auto` is what `auto` was:
// the unattended pass a cron or launchd entry runs. It hands in the
// slot, states the pass-level refusal once on stderr, and exits with
// what the slot left unfinished — 62 while pending, and never a
// refusal. On this build the road refuses at its first line, which
// means wiring it changes no observable behaviour today; that is the
// point. A person's `cycle` retires, drains and reclaims, and
// publishes nothing.
//
// The worker audit is not rendered here. It is a rendering `status`
// wants; this pass touches the untracked workers only when told to,
// and says what it did to each.
type cycleAction struct {
	// keepMerged withholds the one deletion the pass performs unasked,
	// without changing the verdict — what a merged pull request means
	// does not depend on whether anybody is willing to act on it. The
	// branch stands and its line says it was kept, and why.
	keepMerged bool
	// superseded adds the second sweep: the branches a newer sibling
	// replaced. It is a flag and not the default because a supersede is
	// dockhand's OWN inference from two branch names about one port, made
	// without asking anybody, and inference is not grounds to delete
	// work. Nothing else in the tool removes a superseded branch; this
	// flag is the person saying they meant it.
	superseded bool
	// reclaimOrphans releases the workers the backend is running that no
	// note in this checkout claims — this checkout's own and the
	// unattributed. A worker another checkout started is named and left
	// to that checkout's own cycle: it may be a kept failure somebody
	// there is still reading.
	reclaimOrphans bool
}

var _ Action = cycleAction{}

func (a cycleAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// The one construction of a publish slot in the command tree, and it
	// is behind the invoker. A person's pass hands in nil, which the
	// reconciler reads as "no publish road" rather than as a road to
	// refuse — a refusal printed on every cycle a maintainer typed would
	// be noise about a question they never asked.
	var slot *engine.PublishSlot
	if rs.Invoker == record.Machine {
		slot = &engine.PublishSlot{}
	}
	e := rs.Deps()
	rep, err := e.Reconcile(ctx, engine.ReconcileOpts{
		Retire: true, KeepMerged: a.keepMerged, Drain: true, Reclaim: a.reclaimOrphans, Publish: slot})
	if err != nil {
		return err
	}
	rep.Text(rs.Out, rs.Err)
	if a.superseded {
		// After the merged sweep and never instead of it. A branch whose
		// pull request merged is retired as merged — the forge's own word
		// about the work, and the more informative of the two answers —
		// and only what survives that is asked whether a sibling replaced
		// it.
		//
		// A separate pass rather than a phase of the reconciler, because
		// it asks a different question of a different source: being
		// superseded is a local fact about two branches in one namespace,
		// so this costs no gh call and works with no network, while every
		// phase of the reconciler is about what GitHub said.
		repo, err := rs.Repo(ctx)
		if err != nil {
			return err
		}
		said, err := e.CleanSuperseded(ctx, repo)
		render.Prose(said, rs.Out, rs.Err)
		if err != nil {
			return err
		}
	}
	if slot == nil {
		return nil
	}
	sayPassRefusal(rs, slot)
	// What the machine's pass should exit with, which is the WAITING and
	// never the refusing: see PublishSlot.Outcome. A change whose
	// verification is still going, a forge that would not answer, a cap
	// that stopped the pass short — those are 62, ask again later. A
	// closed road is not.
	return slot.Outcome()
}

// sayPassRefusal states a refusal that was about the PASS rather than
// about any branch — today, the build gate, asked once before the slot
// considers a single candidate.
//
// Branch-scoped outcomes are already under their branches in the report,
// which is where every other phase's prose goes. This one has no branch
// to go under, and printing it under each of them would bury the report
// it was printed in. It is on stderr, once, so an operator reading a
// cron log can tell a pass that considered publication and was refused
// from a pass that never had a publish road at all.
func sayPassRefusal(rs *runstate.Context, slot *engine.PublishSlot) {
	for _, r := range slot.Results {
		if r.Err != nil && r.Branch == "" {
			fmt.Fprintln(rs.Err, r.Err)
		}
	}
}

// Cycle builds the cycle subcommand: the verb that does the work
// `status` reports, and the entrypoint a cron or launchd entry names
// with --auto.
func Cycle() *cobra.Command {
	var keepMerged, superseded, reclaimOrphans bool
	c := &cobra.Command{
		Use:   "cycle",
		Short: "Do what status only reports: retire merged branches, start deferred runs",
		Long: `Act on what ` + "`dockhand status`" + ` only reports.

The pass is the one status runs — every dockhand branch and every other
branch carrying a verify note is observed, finished runs are settled —
and then it does what status names: a branch whose pull request merged
is retired, locally and on your fork, and the runs that were deferred
for want of a slot are started. Merged-ness is GitHub's own word and
never sha ancestry, the fork copy goes with the branch because dockhand
put it there, and everything kept says why it was kept. Only a branch
dockhand minted (dockhand/*) is ever deleted; a hand-made branch with a
verify note is shown and settled and left alone, whatever its pull
request did.

--keep-merged withholds the deletion: the branch stands and its line
says so.

--superseded adds the branches a newer sibling replaced. It is opt-in
because a supersede is dockhand's own inference — two branches about
one port, the newer one minted second — and nothing else in the tool
removes a branch on the strength of it. Their fork copies are left
alone, since one of them may back a pull request somebody is reading,
and a held branch is kept and says so.

--reclaim-orphans releases the workers this machine is running that no
note in this checkout claims — its own and the unattributed. A worker
another checkout started is named and left to that checkout's own
cycle. Opt-in, because it destroys environments nobody has
characterised.

Run as the machine — ` + "`dockhand cycle --auto`" + `, or ` + autoEnv + `=1 in a
launchd plist — the pass also walks the publish slot, which is refused
on this build: no machine may spend a reviewer's attention until the
trust ladder has been ruled on. A person's cycle publishes nothing.`,
		Args: noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return cycleAction{keepMerged: keepMerged, superseded: superseded, reclaimOrphans: reclaimOrphans}, nil
		}),
	}
	c.Flags().BoolVar(&keepMerged, "keep-merged", false,
		"keep the branches of merged pull requests rather than retiring them")
	c.Flags().BoolVar(&superseded, "superseded", false,
		"also remove branches a newer sibling replaced")
	c.Flags().BoolVar(&reclaimOrphans, "reclaim-orphans", false,
		"also release the untracked workers this checkout may claim")
	return c
}
