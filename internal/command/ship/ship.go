package ship

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/author"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/outdated"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/submit"
	"github.com/herbygillot/dockhand/internal/command/tidyrun"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

// shipCommand carries a change as far as --to says, from wherever it is
// now (the command-line UX second look's §6): a port, updated from master
// in a branch of its own, or a branch, resumed where it stands.
func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var version author.VersionUpdate
	var where branchflags.BranchFlags
	var to string
	var yes, passing, plan bool
	var selectors outdated.Selectors
	cmd := &cobra.Command{
		Use:   "ship [<port>...]",
		Short: "Update, commit, check, and submit changes",
		Long: `Takes each selected change through update, commit, check, and submit.

Select ports or a branch. A port name starts a version update from the
latest master in a branch of its own, or continues the open branch that
already changes the port. -b, -p, and --pr select a branch; with none, the
branch checked out here is used. --mine, --maintainer, --category, and
--not-maintainer select the ports with newer releases as outdated finds
them, as ship --maintainer nomaintainer --category devel --to check.

--to stops each change at commit, check, or submit (the default). Running
ship again resumes: a stage already completed is not repeated, and a check
still queued or running for the same files is followed rather than
started again.

Every change's check is queued before any check is followed, and each
change is submitted as soon as its check passes. If serve is running, the
checks run side by side; otherwise they run here one at a time.

ship shows one plan, with a row for each change, and asks once on a
terminal. --yes skips the question; running without a terminal is the
same decision. --plan shows the plan and changes nothing. With --yes or
without a terminal, ship runs unattended and applies the same holds serve
does: a new major version is held before anything is prepared, and a
commit that isn't Dockhand's own edits, or anything that would hold a
submission, is held for review. Unattended checks build where
build.unattended's profile says, else where build.default's does; --build
or --on selects another.

Each change stops at the stage that holds it, and the others continue. The
exit code is 0 when changes went through, 1 when none did, and 3 when some
need review.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stage := engine.ShipStage(to)
			switch stage {
			case engine.ShipCommit, engine.ShipCheck, engine.ShipSubmit:
			default:
				return fmt.Errorf("--to %s: commit, check, or submit", to)
			}
			if err := version.StageReady(); err != nil {
				return err
			}
			mine := selectors.Chosen()
			if chosen := len(args) > 0 || mine || passing; chosen && where.Explicit() || len(args) > 0 && (mine || passing) || mine && passing {
				return errors.New("choose one target source: port names, a selection (--mine, --maintainer, --category), --passing, or a branch")
			}
			whole := &view.BatchJSON{}
			streams.LinkSteps(whole)
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			var targets []shipTarget
			switch {
			case mine:
				targets, err = mineTargets(ctx, s, e, streams, selectors)
			case passing:
				targets, err = passingTargets(ctx, e)
			default:
				targets, err = ShipTargets(ctx, e, streams, args, where)
			}
			if err == nil && len(targets) > 0 {
				WriteShipPlan(ctx, e, streams.Out, targets, stage)
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
			version.Linked.NoteSet = cmd.Flags().Changed("note")
			return ShipAll(ctx, s, streams, whole, targets, stage, answered, version)
		},
	}
	where.Register(cmd, s, "ship")
	version.Flags(cmd)
	version.StageFlags(cmd)
	cmd.Flags().StringVar(&to, "to", string(engine.ShipSubmit), "how far to carry each change: commit, check, or submit")
	selectors.Register(cmd, "ship, with newer releases,")
	settings.OverAllowanceFlag(cmd, s)
	cmd.Flags().BoolVar(&passing, "passing", false, "the branches whose check passed for their commits, with no pull request of those commits yet")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "ship without asking, holding where serve would")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the plan and change nothing")
	cmd.MarkFlagsMutuallyExclusive("plan", "yes")
	settings.SupportsJSON(cmd)
	settings.KeepModelineFlag(cmd, s)
	return cmd
}

// shipTarget is one change ship carries on: a port to update from master,
// or a branch to resume.
type shipTarget struct {
	Port   string
	Branch *model.Branch
	// version and release are the newest release outdated found, which
	// the update takes rather than asking upstream again.
	Version string
	Release *model.Release
	// row is the change's row in a batch's table, where the batch says one
	// (batchRows).
	row *shipRow
}

func (t shipTarget) Name() string {
	if t.Branch != nil {
		return t.Branch.ShortName()
	}
	return t.Port
}

// shipTargets are the changes the arguments name: each port, as the open
// branch changing it where one does, which ship resumes; else the branch
// the flags name, or the one checked out here.
func ShipTargets(ctx context.Context, e *engine.Engine, streams term.Streams, ports []string, where branchflags.BranchFlags) ([]shipTarget, error) {
	if len(ports) == 0 {
		branch, err := where.Resolve(ctx, e, streams)
		if err != nil {
			return nil, err
		}
		return []shipTarget{{Branch: &branch}}, nil
	}
	var targets []shipTarget
	for _, port := range ports {
		// The port's own open branch, as Select finds it, one asked about
		// where several change it; none is a branch the update starts (the
		// architecture review of 5897a82, R3-3: the 0, 1, or many rule was
		// written here again the day after Select took it).
		selection, err := e.Select(ctx, engine.Selector{Port: port, Policy: engine.SelectExisting}, engine.WorkUpdate)
		if errors.Is(err, engine.ErrNoBranch) {
			targets = append(targets, shipTarget{Port: port})
			continue
		}
		if err != nil {
			return nil, err
		}
		chosen, err := branchflags.SettleSelection(ctx, e, streams, selection, port, engine.WorkUpdate, false)
		switch {
		case err != nil:
			return nil, err
		case chosen.ForVersion:
			targets = append(targets, shipTarget{Port: port})
		default:
			targets = append(targets, shipTarget{Port: port, Branch: &chosen.Branch})
		}
	}
	return targets, nil
}

// mineTargets are the selection's ports with newer releases, as outdated
// finds them, each at the release it found.
func mineTargets(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, selectors outdated.Selectors) ([]shipTarget, error) {
	plan, err := outdated.Batch(ctx, s, e, streams, selectors)
	if err != nil {
		return nil, err
	}
	var targets []shipTarget
	for _, update := range plan.Updates {
		targets = append(targets, shipTarget{Port: update.Port.Port, Version: update.Port.Newest, Release: update.Port.Release})
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
		targets = append(targets, shipTarget{Branch: &branch})
	}
	return targets, nil
}

// writeShipPlan is ship's plan, a row a change: where it starts, and how
// far it goes; under a branch going on to its pull request, its
// submission as it stands, and what holds it, as submit --passing showed
// each (decision 2: one plan listing every branch with its holds).
func WriteShipPlan(ctx context.Context, e *engine.Engine, out io.Writer, targets []shipTarget, stage engine.ShipStage) {
	width := 0
	for _, target := range targets {
		width = max(width, len(target.Name()))
	}
	for _, target := range targets {
		from := "a version update from master"
		switch {
		case target.Branch != nil && target.Port != "":
			// A resumed branch is brought to the release asked for, the
			// pin or the newest, where it isn't there: neither submitting
			// an older version nor refusing (the roadmap's rule for ship).
			from = "existing branch; update if needed to " + cmp.Or(target.Version, "the newest release")
		case target.Branch != nil:
			from = "its branch, where it stands"
		}
		fmt.Fprintf(out, "  %-*s  %s, to %s\n", width, target.Name(), from, stage)
		if target.Branch != nil && stage == engine.ShipSubmit {
			writeSubmitRow(ctx, e, out, strings.Repeat(" ", width+4), *target.Branch)
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
	fmt.Fprintf(out, "%s%s · %s · %s\n", indent, plan.Title, view.CheckWords(plan), view.PullRequestWords(plan))
	// Under a label of their own, the lines need no "upstream:" each (the
	// ov run's finding 5).
	if lines := view.UpstreamLines(plan); len(lines) > 0 {
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
func ShipAll(ctx context.Context, s *settings.Settings, streams term.Streams, whole *view.BatchJSON, targets []shipTarget, stage engine.ShipStage, answered model.Answered, version author.VersionUpdate) error {
	unattended := branchstate.Mode{Answered: answered}.Unattended()
	if unattended {
		streams = streams.Unattended()
	}
	var failed []string
	rows := batchRows(streams, targets)
	stopped := func(target shipTarget, err error) error {
		if len(targets) == 1 {
			// One change refused is the command's error alone, as a
			// command refusing before it does anything reports.
			return err
		}
		// In a batch, each change that stopped is in the result with why,
		// where only the error said it (the rc1 full stage, S2).
		streams.Emit([]view.SkippedJSON{{Port: target.Name(), Reason: err.Error()}})
		failed = append(failed, target.Name())
		if target.row != nil {
			target.row.stopped(err)
			return nil
		}
		fmt.Fprintf(streams.Err, "%s: %v\n", target.Name(), view.SayNext(err))
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
	writeRows(streams, rows)
	return view.BatchExit(failed, len(targets), "shipped")
}

// shipRow is a change's row in a batch's table at the default level: a
// cell a stage, and why it stopped, where it did (part 5, item 3: ship
// --mine's row a port, then "Needs you").
type shipRow struct {
	name  string
	cells []string
	// why is what stopped it, which "Needs you" says.
	why error
}

// stopped marks the stage the change stopped at: what wants a look, or
// what failed.
func (r *shipRow) stopped(err error) {
	r.why = err
	mark := view.Broken
	if refusal.KindOf(err) == refusal.Attention || term.ExitCode(err) == term.ExitAttention {
		mark = view.Look
	}
	r.cells = append(r.cells, mark)
}

// batchRows are a batch's rows, one a change, where its stages are cells
// of one table: several changes, at the default level, and no --json.
func batchRows(streams term.Streams, targets []shipTarget) []*shipRow {
	if len(targets) < 2 || streams.Verbose() > 0 || streams.JSON() {
		return nil
	}
	var rows []*shipRow
	for i := range targets {
		row := &shipRow{name: targets[i].Name()}
		targets[i].row = row
		rows = append(rows, row)
	}
	return rows
}

// writeRows says a batch's table, and under "Needs you" each change that
// stopped, with why.
func writeRows(streams term.Streams, rows []*shipRow) {
	width := 0
	for _, row := range rows {
		width = max(width, len(row.name))
	}
	var stopped []*shipRow
	for _, row := range rows {
		fmt.Fprintf(streams.Out, "  %-*s  %s\n", width, row.name, strings.Join(row.cells, " "))
		if row.why != nil {
			stopped = append(stopped, row)
		}
	}
	if len(stopped) == 0 {
		return
	}
	fmt.Fprintln(streams.Out, "Needs attention")
	for _, row := range stopped {
		fmt.Fprintf(streams.Out, "  %-*s  %v\n", width, row.name, view.SayNext(row.why))
	}
}

// carried is a change ship carried as far as its check: the check queued,
// none where one passed already, and the submission that follows it.
type carried struct {
	target shipTarget
	run    *model.Run
	submit *pendingSubmit
	// lines are the change's stage lines; queued is its check's queueing,
	// the report held back, said where the check doesn't pass.
	lines  *stageLines
	queued *term.Stage
}

// stageLines are a change's line a stage at the default level, under its
// heading, said with the first (term.Staged).
type stageLines struct {
	heading string
	// row is the batch's row the stages are cells of, instead.
	row *shipRow
}

// done says a stage that went through: a line, or in a batch's row, its
// cell, a mark or what the person would follow up, "#34901".
func (l *stageLines) done(at *term.Stage, name, words, cell string) {
	if l.row != nil {
		l.row.cells = append(l.row.cells, cmp.Or(cell, view.Done))
		at.Drop()
		return
	}
	text := view.StageLine(view.Done, name, words)
	if l.heading != "" {
		text, l.heading = l.heading+"\n"+text, ""
	}
	at.Done("%s", text)
}

// shipOne carries one change to its check: a port's update, from master or
// in the branch already changing it, then the branch's edits committed,
// and its check queued where the stage goes on to one. Nil where there's
// nothing more to do.
func shipOne(ctx context.Context, s *settings.Settings, streams term.Streams, target shipTarget, stage engine.ShipStage, unattended, batch bool, version author.VersionUpdate) (*carried, error) {
	linked := version.Linked
	linked.Unattended, linked.GoesOn = unattended, true
	// Each stage is a line at the default level, under the change's
	// heading, said with its first; the stage that holds or fails says its
	// report (term.Staged).
	lines := &stageLines{heading: target.Name(), row: target.row}
	if target.Port != "" {
		if stage != engine.ShipCommit {
			// Where to check is settled before anything is edited.
			if err := whereReady(ctx, s, linked); err != nil {
				return nil, err
			}
		}
		request, err := version.Request([]string{target.Port}, false)
		if err != nil {
			return nil, err
		}
		// A batch, or a ship nobody looks over, starts no branch for an
		// edit dockhand can't make (UpdateRequest.HandBranch).
		request.Version, request.Release, request.Unattended, request.Batch = target.Version, target.Release, unattended, batch
		where := branchflags.BranchChoice{New: branchflags.NewNamed}
		if target.Branch != nil {
			where = branchflags.BranchChoice{Branch: target.Branch.ShortName()}
		}
		edits, edit := streams.Staged("editing " + target.Port)
		updated, update, err := author.Author(ctx, s, edits, where, "update", request, linked)
		if err != nil || !update.Applied {
			edit.Say()
		}
		if err != nil || target.Branch == nil && !update.Applied {
			return nil, err
		}
		target.Branch = &updated
		if update.Applied {
			lines.heading = view.UpdateHeading(target.Port, update, updated)
			lines.done(edit, "edited", strings.Join(update.Files, ", "), "")
		} else {
			lines.heading = updated.ShortName()
		}
	}
	branch := *target.Branch
	e, err := s.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	commits, commit := streams.Staged("committing " + branch.ShortName())
	tidied, err := tidyrun.Stage(ctx, e, commits, branch, "nothing was checked or submitted")
	if err != nil || !tidied.Applied {
		commit.Say()
		return nil, err
	}
	lines.done(commit, "committed", strings.Join(tidied.Subjects, "; "), "")
	if stage == engine.ShipCommit {
		return nil, nil
	}
	branch = tidied.Branch
	place := engine.WhereRequest{On: linked.On, Build: linked.Build, Unattended: linked.Unattended}
	queues, queued := streams.Staged("queueing " + branch.ShortName() + "'s check")
	done := &carried{target: target, lines: lines, queued: queued}
	if batch && target.row == nil {
		// A batch's checks are seen through after every change is queued,
		// so each says its own heading again.
		done.lines = &stageLines{heading: branch.ShortName()}
	}
	if stage == engine.ShipCheck {
		run, err := checkHead(ctx, s, e, queues, branch, place, linked.Tests)
		if err != nil || run == nil {
			queued.Say()
			return nil, err
		}
		done.run = run
		return done, nil
	}
	fmt.Fprintln(queues.Out)
	request := engine.SubmitRequest{Branch: branch, TestedBinaries: linked.TestedBinaries, TestedVariants: linked.TestedVariants, Title: linked.Title, SkipNotification: linked.SkipNotification, Types: linked.Types, Draft: linked.Draft}
	if linked.NoteSet {
		request.Note = &linked.Note
	}
	pending, run, err := prepareSubmit(ctx, s, e, queues, request, place, linked.Tests)
	if err != nil {
		queued.Say()
		return nil, err
	}
	done.run, done.submit = run, &pending
	return done, nil
}

// finishCarried sees a change's check through, run here where no serve
// leads, and submits it where it passed and ship goes on to submit.
func finishCarried(ctx context.Context, s *settings.Settings, streams term.Streams, done carried) error {
	e, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	var checked error
	if done.run != nil {
		checks, check := streams.Staged("checking " + done.target.Name())
		checked = checkrun.RunQueued(ctx, e, *done.run, checks, false, s.Places(), s.Drain)
		if checked != nil {
			// The check's report alone expands: the plan it was queued
			// with is -v's (part 5, item 3).
			check.Say()
		} else {
			words := "passed"
			if checkedBy, err := e.RunEvidence(ctx, done.run.ID); err == nil {
				words = view.PassedOn(checkedBy)
			}
			done.lines.done(check, "checked", words, "")
		}
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
	if done.run == nil {
		done.lines.done(done.queued, "checked", "passed for this commit already", "")
	}
	submits, submission := streams.Staged("submitting " + done.target.Name())
	submitted, err := submitPassed(ctx, s, e, submits, *done.submit)
	if err != nil {
		submission.Say()
		return err
	}
	done.lines.done(submission, "submitted", submit.StageWords(submitted, done.submit.request.Draft), fmt.Sprintf("#%d", submitted.PullRequest.Ref.Number))
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
func prepareSubmit(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, request engine.SubmitRequest, place engine.WhereRequest, tests string) (pendingSubmit, *model.Run, error) {
	request.PendingCheck, request.Head, request.Where, request.Tests = true, true, place, model.TestPolicy(tests)
	plan, err := e.PlanSubmit(ctx, request)
	if err != nil {
		return pendingSubmit{}, nil, err
	}
	streams.Emit(view.SubmitView(plan))
	view.WriteSubmitPlan(streams.Out, plan, streams.Terminal())
	if len(plan.Blocking) > 0 {
		return pendingSubmit{}, nil, errors.New("nothing was checked or submitted")
	}
	if streams.Terminal() {
		if err := submit.AskTested(streams, &plan); err != nil {
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
func submitPassed(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, pending pendingSubmit) (engine.Submitted, error) {
	plan, err := e.PlanSubmit(ctx, pending.request)
	if err != nil {
		return engine.Submitted{}, err
	}
	streams.Emit(view.SubmitView(plan))
	fmt.Fprintln(streams.Out)
	submitted, err := submit.Apply(ctx, e, streams, plan, s.File.Submit.RerequestReview, pending.answered)
	// The gate is the engine's (branchstate.Publish); ship words what it
	// refused as ship says it.
	refused := refusal.Of(err)
	switch {
	case refused == nil:
		return submitted, err
	case len(plan.Blocks) > 0:
		view.WriteSubmitPlan(streams.Out, plan, false)
		return submitted, errors.New("nothing was submitted")
	case refused.Kind() == refusal.Attention:
		held := engine.HoldLines(plan.Held())
		viewed := view.SubmitView(plan)
		viewed.Held = held
		streams.Emit(viewed)
		name := plan.Branch.ShortName()
		return submitted, term.Exitf(term.ExitAttention, "%s passed its check and needs review, so nothing was submitted: %s\nAfter review: %s", name, strings.Join(held, "; "), strings.Join(view.RenderSteps(refused.Next), "; then "))
	}
	return submitted, err
}

// whereReady settles where a change's check builds before anything is
// edited, so stopping there changes nothing.
func whereReady(ctx context.Context, s *settings.Settings, linked author.LinkedOptions) error {
	e, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	if _, err := e.Where(ctx, engine.WhereRequest{On: linked.On, Build: linked.Build, Unattended: linked.Unattended}); err != nil {
		return fmt.Errorf("%w; nothing was changed", err)
	}
	return nil
}

// checkHead queues a check of a branch's committed head where place says,
// as check --head would; nil where checks that passed for it already stand
// for this one (queueHead).
func checkHead(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, branch model.Branch, place engine.WhereRequest, tests string) (*model.Run, error) {
	return queueHead(ctx, s, e, streams, branch, place, tests, "")
}

// queueHead is ship's check of a branch's committed head: proposed, shown
// as check shows it, and queued; a check already queued or running for the
// same files is the one followed, so a rerun resumes rather than refusing
// (decision 4). then says what follows it, as ", then submitting it if it
// passes". tests is --tests, check.tests where it's empty.
func queueHead(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, branch model.Branch, place engine.WhereRequest, tests, then string) (*model.Run, error) {
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
	view.WritePlan(streams.Out, proposal.Plan, e.PlanNotes(ctx, proposal.Plan), e.PlanSiblings(ctx, proposal.Plan), e.Remedy, view.ListShown(streams))
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
