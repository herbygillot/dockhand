package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

// shipCommand carries a change as far as --to says, from wherever it is
// now (the command-line UX second look's §6): a port, updated from master
// in a branch of its own, or a branch, resumed where it stands.
func shipCommand(s *settings, streams term.Streams) *cobra.Command {
	var version versionUpdate
	var where branchFlags
	var to string
	var yes, mine, passing, plan bool
	cmd := &cobra.Command{
		Use:   "ship [<port>...]",
		Short: "Carry a change on to its pull request: update, commit, check, submit",
		Long: `Carries each change as far as --to says, commit, check, or submit, the
default, from wherever it is now. A port names a version update from fresh
master, in a branch of its own, or the open branch already changing it,
which goes on from where it stands; -b, -p, and --pr name a branch; with
neither, the branch checked out here. Run again, it resumes: a change
already past --to is left as it is, and a check still queued or running
for the same files is followed, not started again.

Each change is carried to its check, queued, before any check is seen
through, and each is submitted as its check passes: serve runs them side
by side where it runs, and they run here one by one where it doesn't.

It shows one plan, a row a change, and asks once on a terminal; --yes
skips the question, and without a terminal running it is the decision.
--plan shows the plan and changes nothing.
With --yes or without a terminal nobody looks along the way, so it holds
where serve would: a new major version before anything is prepared, a
commit that isn't dockhand's own edits, and what holds a submission. A
check builds where build.unattended's profile says then, and else where
build.default's does; --build or --on names another.

Each change stops where its stage would, and the others go on; the exit
says whether any went through: 1 where none did, 3 where some need a look.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stage := engine.ShipStage(to)
			switch stage {
			case engine.ShipCommit, engine.ShipCheck, engine.ShipSubmit:
			default:
				return fmt.Errorf("--to %s: commit, check, or submit", to)
			}
			if err := version.stageReady(); err != nil {
				return err
			}
			if chosen := len(args) > 0 || mine || passing; chosen && where.explicit() || len(args) > 0 && (mine || passing) || mine && passing {
				return errors.New("choose one target source: port names, --mine, --passing, or a branch")
			}
			whole := &view.BatchJSON{}
			streams.LinkSteps(whole)
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			var targets []shipTarget
			switch {
			case mine:
				targets, err = mineTargets(ctx, s, e, streams)
			case passing:
				targets, err = passingTargets(ctx, e)
			default:
				targets, err = shipTargets(ctx, e, streams, args, where)
			}
			if err == nil && len(targets) > 0 {
				writeShipPlan(ctx, e, streams.Out, targets, stage)
			}
			e.Close()
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				fmt.Fprintln(streams.Out, "Nothing to ship.")
				return nil
			}
			if plan {
				fmt.Fprintln(streams.Out, "Nothing was shipped (--plan).")
				return nil
			}
			answered := model.AnsweredNobody
			switch {
			case yes:
				answered = model.AnsweredYes
			case streams.Terminal():
				ok, _, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? Ship %s? [y/N] ", map[bool]string{true: "it", false: "these"}[len(targets) == 1]), Undone: "nothing was shipped", ByYes: "--yes ships them"})
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(streams.Out, "Nothing was shipped.")
					return nil
				}
				answered = model.AnsweredAsked
			}
			version.linked.noteSet = cmd.Flags().Changed("note")
			return shipAll(ctx, s, streams, whole, targets, stage, answered, version)
		},
	}
	where.register(cmd, s, "ship")
	version.flags(cmd)
	version.stageFlags(cmd)
	cmd.Flags().StringVar(&to, "to", string(engine.ShipSubmit), "how far to carry each change: commit, check, or submit")
	cmd.Flags().BoolVar(&mine, "mine", false, "your ports with newer releases, as outdated --mine finds them")
	overAllowanceFlag(cmd, s)
	cmd.Flags().BoolVar(&passing, "passing", false, "the branches whose check passed for their commits, with no pull request of those commits yet")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "ship without asking, holding where serve would")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the plan and change nothing")
	cmd.MarkFlagsMutuallyExclusive("plan", "yes")
	supportsJSON(cmd)
	return cmd
}

// shipTarget is one change ship carries on: a port to update from master,
// or a branch to resume.
type shipTarget struct {
	port   string
	branch *model.Branch
	// version and release are the newest release outdated found, which
	// the update takes rather than asking upstream again.
	version string
	release *model.Release
}

func (t shipTarget) name() string {
	if t.branch != nil {
		return t.branch.ShortName()
	}
	return t.port
}

// shipTargets are the changes the arguments name: each port, as the open
// branch changing it where one does, which ship resumes; else the branch
// the flags name, or the one checked out here.
func shipTargets(ctx context.Context, e *engine.Engine, streams term.Streams, ports []string, where branchFlags) ([]shipTarget, error) {
	if len(ports) == 0 {
		branch, err := where.resolve(ctx, e, streams)
		if err != nil {
			return nil, err
		}
		return []shipTarget{{branch: &branch}}, nil
	}
	var targets []shipTarget
	for _, port := range ports {
		// The port's own open branch, as Select finds it, one asked about
		// where several change it; none is a branch the update starts (the
		// architecture review of 5897a82, R3-3: the 0, 1, or many rule was
		// written here again the day after Select took it).
		selection, err := e.Select(ctx, engine.Selector{Port: port, Policy: engine.SelectExisting}, engine.WorkUpdate)
		if errors.Is(err, engine.ErrNoBranch) {
			targets = append(targets, shipTarget{port: port})
			continue
		}
		if err != nil {
			return nil, err
		}
		chosen, err := settleSelection(ctx, e, streams, selection, port, engine.WorkUpdate, false)
		switch {
		case err != nil:
			return nil, err
		case chosen.forVersion:
			targets = append(targets, shipTarget{port: port})
		default:
			targets = append(targets, shipTarget{port: port, branch: &chosen.branch})
		}
	}
	return targets, nil
}

// mineTargets are the person's ports with newer releases, as outdated
// --mine finds them, each at the release it found.
func mineTargets(ctx context.Context, s *settings, e *engine.Engine, streams term.Streams) ([]shipTarget, error) {
	plan, err := outdatedBatch(ctx, s, e, streams)
	if err != nil {
		return nil, err
	}
	var targets []shipTarget
	for _, update := range plan.Updates {
		targets = append(targets, shipTarget{port: update.Port.Port, version: update.Port.Newest, release: update.Port.Release})
	}
	return targets, nil
}

// passingTargets are the branches whose check passed for their commits,
// with no pull request of those commits yet.
func passingTargets(ctx context.Context, e *engine.Engine) ([]shipTarget, error) {
	passing, err := e.PassingBranches(ctx)
	if err != nil {
		return nil, err
	}
	var targets []shipTarget
	for _, status := range passing.Ready {
		branch := status.Branch
		targets = append(targets, shipTarget{branch: &branch})
	}
	return targets, nil
}

// writeShipPlan is ship's plan, a row a change: where it starts, and how
// far it goes; under a branch going on to its pull request, its
// submission as it stands, and what holds it, as submit --passing showed
// each (decision 2: one plan listing every branch with its holds).
func writeShipPlan(ctx context.Context, e *engine.Engine, out io.Writer, targets []shipTarget, stage engine.ShipStage) {
	width := 0
	for _, target := range targets {
		width = max(width, len(target.name()))
	}
	for _, target := range targets {
		from := "a version update from master"
		switch {
		case target.branch != nil && target.port != "":
			// A resumed branch is brought to the release asked for, the
			// pin or the newest, where it isn't there: neither submitting
			// an older version nor refusing (the roadmap's rule for ship).
			from = "existing branch; update if needed to " + cmp.Or(target.version, "the newest release")
		case target.branch != nil:
			from = "its branch, where it stands"
		}
		fmt.Fprintf(out, "  %-*s  %s, to %s\n", width, target.name(), from, stage)
		if target.branch != nil && stage == engine.ShipSubmit {
			writeSubmitRow(ctx, e, out, strings.Repeat(" ", width+4), *target.branch)
		}
	}
}

// writeSubmitRow is a branch's submission as it stands, under its row:
// its title, checks, and pull request, what upstream showed, and what
// holds it.
func writeSubmitRow(ctx context.Context, e *engine.Engine, out io.Writer, indent string, branch model.Branch) {
	plan, err := e.PlanSubmit(ctx, engine.SubmitRequest{Branch: branch, PendingCheck: true})
	if err != nil {
		fmt.Fprintf(out, "%s✗ %v\n", indent, err)
		return
	}
	fmt.Fprintf(out, "%s%s · %s · %s\n", indent, plan.Title, view.CheckWords(plan), pullRequestWords(plan))
	// Under a label of their own, the lines need no "upstream:" each (the
	// ov run's finding 5).
	if lines := upstreamLines(plan); len(lines) > 0 {
		fmt.Fprintf(out, "%sUpstream %s\n", indent, strings.Join(lines, "\n"+indent+"         "))
	}
	for _, blocking := range plan.Blocking {
		fmt.Fprintf(out, "%s✗ %s\n", indent, blocking)
	}
}

// shipAll carries each change on, each through its own stages, and says
// how the batch went by the batch exit rule (the command-line UX review's
// §10): 1 where none went through, 3 where some need a look. Each is
// carried to its check, queued, before any check is seen through, and
// each submitted as its check passes (decision 4): serve runs them side by
// side where it leads, and a rerun follows a check still queued or
// running for the same files rather than starting another.
func shipAll(ctx context.Context, s *settings, streams term.Streams, whole *view.BatchJSON, targets []shipTarget, stage engine.ShipStage, answered model.Answered, version versionUpdate) error {
	unattended := branchstate.Mode{Answered: answered}.Unattended()
	if unattended {
		streams = streams.Unattended()
	}
	var failed []string
	stopped := func(target shipTarget, err error) error {
		if len(targets) == 1 {
			// One change refused is the command's error alone, as a
			// command refusing before it does anything reports.
			return err
		}
		// In a batch, each change that stopped is in the result with why,
		// where only the error said it (the rc1 full stage, S2).
		streams.Emit([]view.SkippedJSON{{Port: target.name(), Reason: err.Error()}})
		fmt.Fprintf(streams.Err, "%s: %v\n", target.name(), view.SayNext(err))
		failed = append(failed, target.name())
		return nil
	}
	var carried []carried
	for _, target := range targets {
		whole.Next()
		done, err := shipOne(ctx, s, streams, target, stage, unattended, len(targets) > 1, version)
		if err != nil {
			if err := stopped(target, err); err != nil {
				return err
			}
			continue
		}
		if done != nil {
			carried = append(carried, *done)
		}
	}
	for _, done := range carried {
		if err := finishCarried(ctx, s, streams, done); err != nil {
			if err := stopped(done.target, err); err != nil {
				return err
			}
		}
	}
	return batchExit(failed, len(targets), "shipped")
}

// carried is a change ship carried as far as its check: the check queued,
// none where one passed already, and the submission that follows it.
type carried struct {
	target shipTarget
	run    *model.Run
	submit *pendingSubmit
}

// shipOne carries one change to its check: a port's update, from master or
// in the branch already changing it, then the branch's edits committed,
// and its check queued where the stage goes on to one. Nil where there's
// nothing more to do.
func shipOne(ctx context.Context, s *settings, streams term.Streams, target shipTarget, stage engine.ShipStage, unattended, batch bool, version versionUpdate) (*carried, error) {
	linked := version.linked
	linked.unattended, linked.goesOn = unattended, true
	var branch model.Branch
	if target.port != "" {
		if stage != engine.ShipCommit {
			// Where to check is settled before anything is edited.
			if err := whereReady(ctx, s, linked); err != nil {
				return nil, err
			}
		}
		request, err := version.request([]string{target.port}, false)
		if err != nil {
			return nil, err
		}
		// A batch, or a ship nobody looks over, starts no branch for an
		// edit dockhand can't make (UpdateRequest.HandBranch).
		request.Version, request.Release, request.Unattended, request.Batch = target.version, target.release, unattended, batch
		where := branchChoice{new: newNamed}
		if target.branch != nil {
			where = branchChoice{branch: target.branch.ShortName()}
		}
		updated, update, err := author(ctx, s, streams, where, "update", request, linked)
		if err != nil || target.branch == nil && !update.Applied {
			return nil, err
		}
		target.branch = &updated
	}
	branch = *target.branch
	e, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	branch, applied, err := tidyBranch(ctx, e, streams, branch, "nothing was checked or submitted")
	if err != nil || !applied || stage == engine.ShipCommit {
		return nil, err
	}
	place := engine.WhereRequest{On: linked.on, Build: linked.build, Unattended: linked.unattended}
	if stage == engine.ShipCheck {
		run, err := checkHead(ctx, s, e, streams, branch, place, linked.tests)
		if err != nil || run == nil {
			return nil, err
		}
		return &carried{target: target, run: run}, nil
	}
	fmt.Fprintln(streams.Out)
	request := engine.SubmitRequest{Branch: branch, TestedBinaries: linked.testedBinaries, TestedVariants: linked.testedVariants, Title: linked.title, SkipNotification: linked.skipNotification, Types: linked.types, Draft: linked.draft}
	if linked.noteSet {
		request.Note = &linked.note
	}
	pending, run, err := prepareSubmit(ctx, s, e, streams, request, place, linked.tests)
	if err != nil {
		return nil, err
	}
	return &carried{target: target, run: run, submit: &pending}, nil
}

// finishCarried sees a change's check through, run here where no serve
// leads, and submits it where it passed and ship goes on to submit.
func finishCarried(ctx context.Context, s *settings, streams term.Streams, done carried) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	var checked error
	if done.run != nil {
		checked = checkrun.RunQueued(ctx, e, *done.run, streams, false, s.places(), s.drain)
		draft := done.submit != nil && branchstate.SubmitsDespiteCheck(done.submit.request.Draft)
		// The check's outcome, failed, waiting for a look, or stopped, is
		// what a draft submits despite; a failure to check isn't (R3-38:
		// an exit error was read as the verdict, and its message added to).
		outcome := refusal.KindOf(checked).Outcome()
		if checked != nil && (!draft || !outcome) {
			if done.submit != nil && outcome {
				return refusal.Wrap(refusal.KindOf(checked), checked, "%s; nothing was submitted", checked).Then(refusal.Of(checked).Next...)
			}
			return checked
		}
	}
	if done.submit == nil {
		return checked
	}
	if err := submitPassed(ctx, s, e, streams, *done.submit); err != nil {
		return err
	}
	return checked
}

// pendingSubmit is a submission ship holds until its check ends: what it
// asks, bound to its commit (SubmitRequest.Bound), and who answers for
// it, nobody where nobody looks before it goes.
type pendingSubmit struct {
	request  engine.SubmitRequest
	answered model.Answered
}

// prepareSubmit previews a branch's submission of its committed head, and
// queues the check those files still need, returning it; none where one
// passed already. Running ship is the decision: the commit is bound here,
// and submitPassed submits exactly it once its check passes.
func prepareSubmit(ctx context.Context, s *settings, e *engine.Engine, streams term.Streams, request engine.SubmitRequest, place engine.WhereRequest, tests string) (pendingSubmit, *model.Run, error) {
	request.PendingCheck, request.Head, request.Where, request.Tests = true, true, place, model.TestPolicy(tests)
	plan, err := e.PlanSubmit(ctx, request)
	if err != nil {
		return pendingSubmit{}, nil, err
	}
	streams.Emit(view.SubmitView(plan))
	writeSubmitPlan(streams.Out, plan)
	if len(plan.Blocking) > 0 {
		return pendingSubmit{}, nil, errors.New("nothing was checked or submitted")
	}
	if streams.Terminal() {
		if err := askTested(streams, &plan); err != nil {
			return pendingSubmit{}, nil, err
		}
		request.TestedBinaries, request.TestedVariants = plan.Request.TestedBinaries, plan.Request.TestedVariants
	}
	request.PendingCheck = false
	request.Bound = plan.Commit
	pending := pendingSubmit{request: request, answered: model.AnsweredAsked}
	if place.Unattended {
		pending.answered = model.AnsweredNobody
	}
	// What stands for the check asked submits as it is (PlanSubmit).
	if !plan.CheckNeeded {
		return pending, nil, nil
	}
	// A check nobody looks over builds on build.unattended's profile.
	then := ", then submitting it if it passes"
	if request.Draft {
		then = ", then submitting it as a draft"
	}
	run, err := queueHead(ctx, s, e, streams, plan.Branch, place, tests, then)
	return pending, run, err
}

// submitPassed submits a pending submission once its check passed:
// exactly the commit it was bound to, and, where nobody looks, only what
// holds nothing.
func submitPassed(ctx context.Context, s *settings, e *engine.Engine, streams term.Streams, pending pendingSubmit) error {
	plan, err := e.PlanSubmit(ctx, pending.request)
	if err != nil {
		return err
	}
	streams.Emit(view.SubmitView(plan))
	fmt.Fprintln(streams.Out)
	err = applySubmit(ctx, e, streams, plan, s.file.Submit.RerequestReview, pending.answered)
	// The gate is the engine's (branchstate.Publish); ship words what it
	// refused as ship says it.
	refused := refusal.Of(err)
	switch {
	case refused == nil:
		return err
	case len(plan.Blocks) > 0:
		writeSubmitPlan(streams.Out, plan)
		return errors.New("nothing was submitted")
	case refused.Kind() == refusal.Attention:
		held := engine.HoldLines(plan.Held())
		viewed := view.SubmitView(plan)
		viewed.Held = held
		streams.Emit(viewed)
		name := plan.Branch.ShortName()
		return term.Exitf(term.ExitAttention, "%s passed its check and waits for your look, so nothing was submitted: %s\nOnce it's fine: %s", name, strings.Join(held, "; "), strings.Join(view.RenderSteps(refused.Next), "; then "))
	}
	return err
}

// whereReady settles where a change's check builds before anything is
// edited, so stopping there changes nothing.
func whereReady(ctx context.Context, s *settings, linked linkedOptions) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	if _, err := e.Where(ctx, engine.WhereRequest{On: linked.on, Build: linked.build, Unattended: linked.unattended}); err != nil {
		return fmt.Errorf("%w; nothing was changed", err)
	}
	return nil
}

// checkHead queues a check of a branch's committed head where place says,
// as check --head would; nil where checks that passed for it already stand
// for this one (queueHead).
func checkHead(ctx context.Context, s *settings, e *engine.Engine, streams term.Streams, branch model.Branch, place engine.WhereRequest, tests string) (*model.Run, error) {
	return queueHead(ctx, s, e, streams, branch, place, tests, "")
}

// queueHead is ship's check of a branch's committed head: proposed, shown
// as check shows it, and queued; a check already queued or running for the
// same files is the one followed, so a rerun resumes rather than refusing
// (decision 4). then says what follows it, as ", then submitting it if it
// passes". tests is --tests, check.tests where it's empty.
func queueHead(ctx context.Context, s *settings, e *engine.Engine, streams term.Streams, branch model.Branch, place engine.WhereRequest, tests, then string) (*model.Run, error) {
	where, err := e.Where(ctx, place)
	if err != nil {
		return nil, err
	}
	proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: engine.CaptureHead}, Where: where, Tests: model.TestPolicy(tests)})
	if err != nil {
		return nil, err
	}
	// Checks that passed for these files stand for this one only where
	// they built everywhere it builds, with tests as strict: a pass of
	// minmax didn't stand for --build all, nor declared tests for --tests
	// required (Codex's review of 832f059e, medium 4). A check of these
	// files still to finish is followed where it would stand for this one.
	queued, err := e.QueueCheck(ctx, engine.QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginPerson, Covered: true, Beside: engine.BesideFollow})
	if queued.Passed != nil {
		fmt.Fprintf(streams.Out, "%s: %s passed for this commit already; nothing to check.\n", branch.ShortName(), queued.Passed.Name())
		return nil, nil
	}
	fmt.Fprintf(streams.Out, "\n%s · checking %s%s\n", branch.ShortName(), engine.Describe(proposal.Capture.Revision), then)
	view.WritePlan(streams.Out, proposal.Plan, e.PlanNotes(ctx, proposal.Plan), e.Remedy)
	view.WritePushes(streams.Out, proposal.Plan)
	if unrunnable := new(engine.UnrunnableError); errors.As(err, &unrunnable) {
		return nil, fmt.Errorf("nothing was checked: %w", err)
	}
	if err != nil {
		return nil, err
	}
	if queued.Followed {
		fmt.Fprintf(streams.Out, "%s is already %s for these files; following it.\n", queued.Run.Name(), queued.Run.State)
	}
	return &queued.Run, nil
}
