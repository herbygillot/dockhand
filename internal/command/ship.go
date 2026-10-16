package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// shipCommand carries a change as far as --to says, from wherever it is
// now (the command-line UX second look's §6): a port, updated from master
// in a branch of its own, or a branch, resumed where it stands.
func shipCommand(s *settings, streams Streams) *cobra.Command {
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
already past --to is left as it is.

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
			if chosen := len(args) > 0 || mine || passing; chosen && where.explicit() || len(args) > 0 && (mine || passing) || mine && passing {
				return errors.New("name ports, --mine, --passing, or a branch: one of them")
			}
			whole := &batchJSON{}
			streams.linkSteps(whole)
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
			case streams.terminal():
				ok, _, err := streams.decide(false, decision{question: fmt.Sprintf("? Ship %s? [y/N] ", map[bool]string{true: "it", false: "these"}[len(targets) == 1]), undone: "nothing was shipped", byYes: "--yes ships them"})
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
	cmd.Flags().BoolVar(&passing, "passing", false, "the branches whose check passed for their commits, with no pull request of those commits yet")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "ship without asking, holding where serve would")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the plan and change nothing")
	cmd.MarkFlagsMutuallyExclusive("plan", "yes")
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
func shipTargets(ctx context.Context, e *engine.Engine, streams Streams, ports []string, where branchFlags) ([]shipTarget, error) {
	if len(ports) == 0 {
		branch, err := where.resolve(ctx, e, streams)
		if err != nil {
			return nil, err
		}
		return []shipTarget{{branch: &branch}}, nil
	}
	var targets []shipTarget
	for _, port := range ports {
		changing, err := e.BranchesChanging(ctx, port)
		if err != nil {
			return nil, err
		}
		switch len(changing) {
		case 0:
			targets = append(targets, shipTarget{port: port})
		case 1:
			targets = append(targets, shipTarget{port: port, branch: &changing[0]})
		default:
			var names []string
			for _, branch := range changing {
				names = append(names, branch.ShortName())
			}
			return nil, fmt.Errorf("%s is changed in %s; ship -b <branch> names which to carry on", port, strings.Join(names, ", "))
		}
	}
	return targets, nil
}

// mineTargets are the person's ports with newer releases, as outdated
// --mine finds them, each at the release it found.
func mineTargets(ctx context.Context, s *settings, e *engine.Engine, streams Streams) ([]shipTarget, error) {
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
			from = "its branch, updated again where it isn't at " + cmp.Or(target.version, "the newest release")
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
	fmt.Fprintf(out, "%s%s · %s · %s\n", indent, plan.Title, checkWords(plan), pullRequestWords(plan))
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
// §10): 1 where none went through, 3 where some need a look.
func shipAll(ctx context.Context, s *settings, streams Streams, whole *batchJSON, targets []shipTarget, stage engine.ShipStage, answered model.Answered, version versionUpdate) error {
	unattended := answered != model.AnsweredAsked
	if unattended {
		streams = streams.unattended()
	}
	var failed []string
	for _, target := range targets {
		whole.next()
		err := shipOne(ctx, s, streams, target, stage, unattended, version)
		if err == nil {
			continue
		}
		if len(targets) == 1 {
			return err
		}
		fmt.Fprintf(streams.Err, "%s: %v\n", target.name(), err)
		failed = append(failed, target.name())
	}
	return batchExit(failed, len(targets), "shipped")
}

// shipOne carries one change on: a port's update, committed, then as far
// as stage says; a branch's edits committed, then checked or submitted.
func shipOne(ctx context.Context, s *settings, streams Streams, target shipTarget, stage engine.ShipStage, unattended bool, version versionUpdate) error {
	linked := version.linked
	linked.unattended, linked.yes = unattended, true
	if target.branch == nil {
		request, err := version.request([]string{target.port}, false)
		if err != nil {
			return err
		}
		if target.version != "" {
			request.Version, request.Release = target.version, target.release
		}
		if stage != engine.ShipSubmit {
			// Committed, and checked where --to says, but not submitted.
			request.Unattended = unattended
			branch, update, err := author(ctx, s, streams, branchChoice{new: newNamed}, "update", request, linked)
			if err != nil || !update.Applied {
				return err
			}
			return carryBranch(ctx, s, streams, branch, stage, linked)
		}
		linked.submit = true
		version.linked = linked
		return version.run(ctx, s, streams, branchChoice{new: newNamed}, request)
	}
	branch := *target.branch
	if target.port != "" {
		request, err := version.request([]string{target.port}, false)
		if err != nil {
			return err
		}
		request.Version, request.Release, request.Unattended = target.version, target.release, unattended
		if branch, _, err = author(ctx, s, streams, branchChoice{branch: branch.ShortName()}, "update", request, linked); err != nil {
			return err
		}
	}
	return carryBranch(ctx, s, streams, branch, stage, linked)
}

// carryBranch carries a branch on from where it stands: its edits
// committed, then checked, or submitted, which checks it first where its
// files have no check yet.
func carryBranch(ctx context.Context, s *settings, streams Streams, branch model.Branch, stage engine.ShipStage, linked linkedOptions) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	branch, applied, err := tidyBranch(ctx, e, streams, branch, linked.yes, "nothing was checked or submitted")
	if err != nil || !applied || stage == engine.ShipCommit {
		return err
	}
	if stage == engine.ShipCheck {
		return checkHead(ctx, s, e, streams, branch, engine.WhereRequest{On: linked.on, Build: linked.build, Unattended: linked.unattended})
	}
	fmt.Fprintln(streams.Out)
	request := engine.SubmitRequest{Branch: branch, TestedBinaries: linked.testedBinaries, TestedVariants: linked.testedVariants, Title: linked.title, SkipNotification: linked.skipNotification}
	if linked.noteSet {
		request.Note = &linked.note
	}
	return submitChecked(ctx, s, e, streams, request, engine.WhereRequest{On: linked.on, Build: linked.build, Unattended: linked.unattended})
}

// checkHead checks a branch's committed head where place says, as check
// --head would: a head a check already passed for is left as it is.
func checkHead(ctx context.Context, s *settings, e *engine.Engine, streams Streams, branch model.Branch, place engine.WhereRequest) error {
	where, err := e.Where(ctx, place)
	if err != nil {
		return err
	}
	capture, err := e.Capture(ctx, engine.CaptureRequest{Branch: branch, Mode: engine.CaptureHead})
	if err != nil {
		return err
	}
	if status, err := e.BranchStatus(ctx, branch); err == nil && status.CheckStanding() == engine.CheckPassed && len(status.Edited) == 0 {
		fmt.Fprintf(streams.Out, "%s: %s passed for this commit already; nothing to check.\n", branch.ShortName(), status.Latest.Name())
		return nil
	}
	proposed, err := e.PlanCheck(ctx, engine.PlanRequest{Revision: capture.Revision, Environments: where.Environments, Span: where.Span, Profile: where.Profile, Tests: model.TestPolicy(s.file.Check.Tests)})
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "\n%s · checking %s\n", branch.ShortName(), engine.Describe(capture.Revision))
	writePlan(streams.Out, proposed, e.PlanNotes(ctx, proposed), e.Remedy)
	writePushes(streams.Out, proposed)
	if !proposed.Runnable() {
		return errors.New("nothing was checked: " + unrunnable(proposed))
	}
	if _, err := replaceActive(ctx, e, streams, branch, capture.Revision, false); err != nil {
		return err
	}
	run, err := e.Enqueue(ctx, branch, proposed, model.OriginPerson)
	if err != nil {
		return err
	}
	return runQueued(ctx, e, run, streams, false)
}
