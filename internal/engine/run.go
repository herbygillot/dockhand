package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// InFlight is what a realization does about a branch already standing
// for this port. Refusing is the default because the standing branch
// may carry work; replacing is the user asking for it by name.
//
// Advance and Supersede are the sweep road's two answers, and they are
// two because a sweep meets a standing branch in two different
// situations. Neither ever demolishes anything: a selector that
// destroyed in-flight work at scale would be the one mistake nobody
// could undo.
type InFlight int

const (
	Refuse InFlight = iota
	Replace
	// Advance leaves a standing branch alone: the change it carries is
	// the change being planned, so there is nothing to mint and nothing
	// wrong. It is what makes resume-by-rerun work — rerunning an
	// interrupted sweep meets its own branches and steps over them,
	// which is why a resume needs no journal.
	Advance
	// Supersede mints beside a standing branch rather than over it. The
	// older branch keeps everything it learned and gains the field
	// saying a newer sibling has replaced it; it is an end state of its
	// own, and nothing discards it. A sweep that auto-discarded here
	// would throw away a verdict somebody may still be reading.
	Supersede
)

// sweeping reports whether this policy came from a selector, which is
// the whole of what mint needs to know about the two above: a standing
// branch is never a refusal and never a demolition.
func (o Policy) sweeping() bool {
	return o.OnInFlight == Advance || o.OnInFlight == Supersede
}

// Realization is what a run did with a plan, in a word its caller can
// act on without reading the prose it printed.
//
// It exists for the sweep. One port's realization is reported to a
// person in sentences on two streams, and reporting a thousand needs
// the same facts as data — which of the two is on stdout is the
// caller's business, but neither may be recovered by scraping the
// other.
type Realization int

const (
	// NotRealized is the zero value, and it means the run did not get
	// as far as a realization: the error beside it says why. It is
	// first so that the zero Realized cannot be mistaken for a plan
	// that had nothing to do — a mint that failed and a mint that was
	// unnecessary are opposite answers and must not share a value.
	NotRealized Realization = iota
	// NothingRealized is a plan with no edits: nothing was written and
	// nothing was refused.
	NothingRealized
	// PlanShown is --plan or --diff: the document is the whole act.
	PlanShown
	// EditApplied is --in-place: the Portfile on disk changed, and
	// nothing was committed.
	EditApplied
	// BranchMinted is the default road: a branch exists that did not
	// before.
	BranchMinted
	// BranchStood is a sweep meeting the branch it would have minted.
	// Nothing was written, and that is the right answer rather than a
	// refusal.
	BranchStood
)

// Realized is what a realization did. The error a run returns says how
// to talk about it — a deferred verification and an errored submit are
// both a minted branch — so the two are returned together and neither
// stands in for the other.
type Realized struct {
	Realization Realization
	// Branch is the branch minted, or the one already standing under
	// BranchStood. Empty for the realizations that mint nothing.
	Branch string
	// Sha is the minted commit, when there is one.
	Sha string
	// Superseded names the branches this mint pushed aside: other
	// in-flight branches for the same port, now recorded as replaced by
	// this one. They are not discarded, and naming them is the only way
	// a sweep can say a port's older change was set down.
	Superseded []string
}

// Policy is one invocation's choice of realization, shared by every
// intent that writes: print the plan, print the diff, edit in place,
// or — the default — mint the branch and submit verification.
type Policy struct {
	PlanOnly bool
	Diff     bool
	InPlace  bool
	Trace    bool
	Test     bool
	// KeepEnv is --keep-env: the environment of a PASSING run stands
	// after settle, the way a failure's does by rule (D27). It is
	// recorded on the run rather than acted on here, and recorded the
	// way FromSource is rather than the way Test is: Test lives on the
	// per-guest JobRecord and a deferred run has no JobRecord, so an ask
	// carried only there would be lost the moment a submit was queued
	// and re-tried by a later cycle. Honoured wherever release is
	// decided — verdict's JudgeRun answers KeepWorker for it.
	KeepEnv bool
	// OnInFlight is --replace: replace what is already in flight for this
	// port, rather than refusing.
	OnInFlight InFlight
	// FromSource says this invocation's own parameters make the change
	// one that must be built from source, whatever its intent would have
	// answered. bump's --recheck is the only one today.
	//
	// It only ever widens what fromSource decides, never narrows it: an
	// intent that is from source by its nature stays so however the run
	// was invoked.
	FromSource bool
	// Destination is how far this invocation's contract reaches, and it
	// is now the record's own type: the value is written onto the note
	// at mint, so an engine enum would have been a second spelling of a
	// wire word. The zero value is the default road — mint and submit —
	// which record.ToVerdict names; --no-verify asks for record.ToBranch.
	Destination record.Destination
	// On is the release to verify on, already parsed by the caller;
	// the zero value means the provider default. Flag parsing is the
	// CLI's business, not the engine's.
	On platform.Release
	// GateProof is what the synchronous --verify gate proved, carried
	// to the minted commit's note so a gate-verified tip reads exactly
	// like a background-verified one: the job it ran in, what lint
	// said, and the provider's own phrase for what the pass is worth.
	GateProof Proof
	// Verified says the synchronous --verify gate already ran and
	// passed on this plan's content, so realization records the verdict
	// instead of buying the same build twice.
	Verified bool

	// Invoker and Agent are the run's provenance, and they sit here for
	// the same reason Destination does: they are record fields the
	// invocation decides, and the realizer is where an invocation's
	// choice becomes a note.
	//
	// Both are written at mint and read by nothing else. Neither is ever
	// an input to a gate — a field that could widen what the unattended
	// road is allowed to do would be an authorization wearing
	// provenance's clothes — and neither is ever derived here: who is
	// running is DECLARED by the caller (--auto, DOCKHAND_AUTO), and an
	// engine that answered it for itself by
	// reading the environment or a terminal would be detecting what the
	// ruling says must be declared.
	Invoker record.Driver
	Agent   string
}

// askedBy is who the record will say asked for this change. The zero
// value reads as record.Human because a person typing a verb is every
// invocation that did not declare otherwise, and because an unset
// provenance is better recorded as the common case than as an empty
// string the ladder's arithmetic would have to guess about.
//
// That default is safe only because nothing gates on this value. The
// publish roads take their invoker as a parameter and have no zero
// value to fall through.
func (o Policy) askedBy() record.Driver {
	if o.Invoker == "" {
		return record.Human
	}
	return o.Invoker
}

// destination is the record's word for how far this policy reaches.
// The zero Policy asks for a verdict, which is the road every intent
// takes unless --no-verify narrows it.
func (o Policy) destination() record.Destination {
	if o.Destination == "" {
		return record.ToVerdict
	}
	return o.Destination
}

// fromSource reports whether this run's change leaves the port's binary
// archive matching bytes the change replaced.
//
// The intent answers first. A version bump does not: the new version
// names an archive that does not exist yet, so MacPorts builds from
// source without being asked. A revision bump does not either, for the
// same reason one level down — the revision is part of the archive's
// name. A checksum refresh does: the version and the revision both
// stand, the archive that matches them predates the change, and a pass
// earned by unpacking it verified nothing about the distfile the change
// is actually about.
//
// The run may then say so for itself, and only in that direction:
// bump's --recheck re-derives a port at the version it already carries,
// which is the refresh's situation reached by a different verb.
func (o Policy) fromSource(intent string) bool {
	return o.FromSource || intent == "refresh-checksums"
}

// Run carries a plan to its chosen realization. Every write intent
// arrives here, so a plan becomes a branch the same way whichever
// intent produced it (D21).
//
// What it did comes back beside the error rather than through it,
// because the two are different questions and a sweep asks both: a
// deferred verification and an errored submit are both a branch that
// now exists, and a caller reading only the error would call the first
// a failure and the second nothing at all. A single-port invocation
// still says everything it says in prose on the two streams; the
// Realized is what a thousand of them can be counted by.
func (e *Engine) Run(ctx context.Context, p *plan.Plan, o Policy) (Realized, error) {
	if o.PlanOnly {
		// A plan printed is the whole contract of --plan, so the twin it
		// carries is success: the decline that would say otherwise never
		// reaches here — a planner that declines returns before Run is
		// called, and the verb writes the decline's own document.
		return Realized{Realization: PlanShown}, p.Encode(e.Out, exitcode.Of(exitcode.OK, ""))
	}
	if o.Diff {
		return Realized{Realization: PlanShown}, e.diffFromPlan(ctx, p)
	}
	if o.InPlace {
		// The deliberate opt-out (D21): edit where the user stands,
		// uncommitted — for the user running their own workflow, and
		// the only write mode a non-git tree has.
		return Realized{Realization: EditApplied}, e.applyPlan(ctx, p)
	}
	m, err := e.mint(ctx, p, o)
	if err != nil {
		return Realized{}, err
	}
	if m == nil {
		return Realized{Realization: NothingRealized}, nil
	}
	if m.Stood {
		// A sweep meeting the branch it would have minted. Nothing was
		// written, nothing is owed, and the standing branch is the
		// answer.
		return Realized{Realization: BranchStood, Branch: m.Branch}, nil
	}
	done := Realized{Realization: BranchMinted, Branch: m.Branch, Sha: m.Sha, Superseded: m.Superseded}
	if o.Verified {
		// The --verify gate built exactly these bytes — the minted blob
		// and the gate's shadow are both the plan's Materialize over the
		// same base — so the verdict transfers to the commit by content
		// identity.
		// Recording it beats resubmitting: the same build twice proves
		// nothing the first one did not.
		return done, e.markVerified(ctx, m, p, o)
	}
	if o.destination() == record.ToBranch {
		// The contract stops at the branch, and the record says so, so
		// nothing will submit this later either: the drain reads the
		// destination and steps over a change nobody asked a verdict of.
		return done, nil
	}
	// The branch is live the moment it exists (D21): verification is
	// submitted against the tip and the guest drives its own build, so
	// this process is free to exit; status collects the verdict.
	return done, e.submit(ctx, m, submission{
		Port: p.Port, Release: o.On, Test: o.Test, KeepEnv: o.KeepEnv,
		FromSource: o.fromSource(p.Intent), Trace: o.Trace,
	})
}

// markVerified writes the minted commit's note as passed, on the
// strength of the pre-mint gate having built identical content.
//
// It writes the job as well as the run, and the job is written
// released: the gate waited for its own answer and handed the
// environment back before returning, so a record that named no
// environment at all would say a verdict had been reached nowhere.
func (e *Engine) markVerified(ctx context.Context, m *Minted, p *plan.Plan, o Policy) error {
	release := o.On
	if release.IsZero() {
		// The gate ran, so a provider exists; its default names the run.
		prov, perr := e.Verifier(ctx)
		if perr != nil {
			return perr
		}
		var rerr error
		if release, rerr = verdict.ResolveRelease(release, prov.Capabilities().Platforms); rerr != nil {
			return rerr
		}
	}
	if err := e.Ledger(m.Repo).RecordSubmission(ctx, m.Sha, release.Name,
		record.JobRecord{Job: o.GateProof.Job, Test: o.Test, Released: true},
		[]string{p.Port},
		// One port, so there is nothing to tell members apart by: the
		// gate proved this one and the run says what it proved.
		ledger.SameRun(record.Run{
			State: record.Passed, Linted: true, Lint: o.GateProof.Lint,
			Evidence: o.GateProof.Evidence, FromSource: o.fromSource(p.Intent),
		})); err != nil {
		return err
	}
	fmt.Fprintf(e.Err, "verified before minting; the tip is recorded as passed on %s\n", release.Name)
	return nil
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
	// The plan's whole files, after the Portfile and not before: Apply
	// proves the predicted delta and restores the Portfile when the
	// proof fails, and a patch written ahead of a restored Portfile
	// would be the one edit the restore did not undo. A refreshed patch
	// moves no evaluated field, so nothing here is part of that proof.
	for _, f := range p.Files {
		if err := os.WriteFile(filepath.Join(p.Portdir, filepath.FromSlash(f.Path)), []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(e.Out, "applied: %s %s (%d edits%s, delta as predicted)\n",
		p.Intent, p.Portdir, len(p.Edits), filesClause(p))
	return err
}

// filesClause is the in-place report's word for the whole files it
// wrote, and nothing when it wrote none — so the sentence every plan
// without one prints is the sentence it always printed.
func filesClause(p *plan.Plan) string {
	if len(p.Files) == 0 {
		return ""
	}
	return fmt.Sprintf(", %d file(s) rewritten", len(p.Files))
}

// diffFromPlan renders a plan as the patch its branch would carry,
// writing nothing the workspace can see: the edited blob is grafted
// into the base tree exactly as a mint would, and the two trees are
// diffed instead of committed. Repo-relative a/ and b/ paths come out
// correct because the trees carry the full structure.
func (e *Engine) diffFromPlan(ctx context.Context, p *plan.Plan) error {
	if !writes(p) {
		fmt.Fprintln(e.Err, "no edits; nothing to diff")
		return nil
	}
	repo, primary, files, err := e.planOnBase(ctx, p)
	if err != nil {
		return err
	}
	tree, err := repo.GraftTree(ctx, primary, files)
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
