package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
)

func checkCommand(s *settings, streams term.Streams) *cobra.Command {
	var where branchFlags
	var tests, variants, build string
	var plan, head, staged, workingTree, enqueue, baseline, replace, fresh, keep, yes bool
	var include, only, also, on []string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Build and test what you have, committed or not",
		Long: `Captures the branch's files, the tracked ones as they are on disk unless
--staged or --head says otherwise, as a numbered snapshot, and builds every
port the branch changes by MacPorts CI's rule: lint, fetch and checksum,
install, and declared tests (advisory unless --tests required).

It builds where a build profile says: --build names one, and build.default
is the profile otherwise, host, this Mac's macOS, unless set. dockhand's
own are host, minmax (each port on its oldest and newest release dockhand
runs, macOS 12 at the earliest), macports-ci (the releases MacPorts' CI
builds on), and all; yours go under [build.profiles]. --on names explicit
environments instead, tart:15,26, github, or command. Every environment
must pass. --only narrows the
check to some changed ports and adds back the changed ones they need;
--also builds unchanged ports against the branch. --plan shows what would
be built and changes nothing.

Where every port an environment would build reads what an earlier build of
it read, recorded with its result, that result is reused and nothing is
built there; --fresh builds everything.

Without dockhand serve, the check runs here and says so; Ctrl-C stops it
and keeps what finished. With serve running, it is handed to serve and
followed here; Ctrl-C then only stops following. -d queues it and returns.

One check of a branch runs at a time: while one is queued or running,
check refuses, and --replace stops it, keeping what it finished, and checks
the files now, on what --on names (decision 29: switching is explicit).

--baseline builds the ports that failed at install or test in the branch's
latest check, or the --only ones, at the master that check started from,
planned there as a check would be, and reports each beside the branch's
result. It says what happened in each run and nothing more. A failed check
points to it when a baseline can answer something; with check.baseline =
true, it runs that baseline by itself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// A misspelled policy is refused before anything is captured.
			if tests != "" && !model.TestPolicy(tests).Valid() {
				return fmt.Errorf("--tests %q is not declared, required, or skip", tests)
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := where.resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			// The result is the check's, with a baseline check.baseline runs
			// inside it.
			streams.LinkSteps(&checkJSON{})
			if baseline && plan {
				return previewBaseline(ctx, e, streams, branch, only)
			}
			if baseline {
				return runBaseline(ctx, e, streams, branch, only, enqueue, s)
			}
			mode := captureMode(head, staged, workingTree)
			place, err := e.Where(ctx, engine.WhereRequest{On: on, Build: build})
			if err != nil {
				return err
			}
			chosen, each, err := engine.VariantsFlag(variants)
			if err != nil {
				return err
			}
			proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: mode, Named: where.explicit(), Include: include, Plan: plan}, Where: place,
				Tests: model.TestPolicy(tests), Only: only, Also: also, Fresh: fresh, Keep: keep, Variants: chosen, EachVariant: each})
			if err != nil {
				return err
			}
			capture, proposed := proposal.Capture, proposal.Plan
			out := streams.Out
			what := "checking " + engine.Describe(capture.Revision)
			if capture.Revision.Kind == model.RevisionSnapshot && !capture.Reused {
				what = "captured working files as " + engine.Describe(capture.Revision)
				if plan {
					what = "would capture the working files as " + engine.Describe(capture.Revision)
				}
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), what)
			// A file the check leaves out in a port it builds is said with
			// a mark, and how to take it in (Codex's gcc15 group update).
			for _, path := range engine.UntrackedInChanged(capture, proposed) {
				fmt.Fprintf(out, "! %s is untracked and excluded from this check. Use --include %s for this check only, or git add %s to track it.\n", path, path, path)
			}
			if len(capture.Untracked) > 0 {
				fmt.Fprintf(out, "Left out, not tracked: %s (--include adds one)\n", strings.Join(capture.Untracked, ", "))
				// A Portfile written by hand in a new port's directory is
				// one Git doesn't track yet, and the worktree's sparse
				// checkout keeps git add from taking it: the commands are
				// said whole (the Vx port's field testing, 2026-10-03).
				for _, directory := range capture.NewPorts {
					fmt.Fprintf(out, "  New port directory %s: run dockhand check --include %s/Portfile to build it. Run git sparse-checkout add %s before staging its files.\n", directory, directory, directory)
				}
			}
			writePlan(out, proposed, e.PlanNotes(ctx, proposed), e.Remedy)
			writePushes(out, proposed)
			revision, planned := revisionView(capture.Revision), planView(proposed)
			streams.Emit(checkJSON{Branch: branch.ShortName(), Revision: &revision, Plan: &planned, Targets: []targetJSON{}})
			if err := engine.Runnable(proposed); err != nil {
				return fmt.Errorf("nothing was checked: %w", err)
			}
			if plan {
				// Reuse is chosen when the check runs, from what each earlier
				// build read, so a plan can't say which ports reuse; that
				// some may, it says, since a plan after a rebase read as a
				// full build where its check reused one (the rc6 full stage,
				// B5).
				if passed, err := e.Runs(ctx, store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunPassed}, Limit: 1}); err == nil && len(passed) > 0 && !proposed.Fresh {
					fmt.Fprintln(out, "Reuse       when this check runs, it may reuse passing builds with identical inputs; --fresh rebuilds every port")
				}
				return nil
			}
			if err := confirmVariantBuilds(streams, proposed, yes); err != nil {
				return err
			}
			beside := engine.BesideRefuse
			if replace {
				beside = engine.BesideReplace
			}
			queued, err := e.QueueCheck(ctx, engine.QueueRequest{Branch: branch, Revision: capture.Revision, Plan: proposed, Origin: model.OriginPerson, Beside: beside,
				// A script that asked for a new check replaces a running one;
				// a person is asked.
				Stop: func(run model.Run) (bool, error) {
					ok, _, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? stop %s, which is running? [y/N] ", run.Name()), Nobody: term.Answer(true)})
					return ok, err
				}})
			replaced := sayReplaced(streams.Out, queued)
			if err != nil {
				return besideWords(err, capture.Revision)
			}
			run := queued.Run
			if len(replaced) > 0 {
				fmt.Fprintf(streams.Out, "%s replaces %s.\n", run.Name(), strings.Join(replaced, ", "))
			}
			err = runQueued(ctx, e, run, streams, enqueue, s)
			// A check that passed says what moves the branch on now, as
			// status would; one run for submit leaves that to submit.
			if err == nil && !enqueue {
				if nextErr := writeNextFor(ctx, e, streams.Out, branch.ID); nextErr != nil {
					fmt.Fprintf(streams.Err, "next: %v\n", nextErr)
				}
			}
			// check.baseline runs the baseline the failed check pointed to,
			// by itself, when it pointed to one. The report has said why
			// if it couldn't tell.
			// The engine says whether one would answer anything, from the
			// finished run, where this read the check's exit code.
			if s.file.Check.Baseline && !enqueue {
				finished, _ := e.RunNamed(ctx, run.Name())
				if offer, _ := e.BaselineCandidates(ctx, finished); len(offer.Ports) > 0 {
					fmt.Fprintln(streams.Out, "Running a baseline comparison because check.baseline is enabled:")
					if baselineErr := runBaseline(ctx, e, streams, branch, nil, false, s); baselineErr != nil {
						fmt.Fprintf(streams.Err, "baseline: %v\n", baselineErr)
					}
				}
			}
			return err
		},
	}
	where.register(cmd, s, "check")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what would be built and change nothing")
	cmd.Flags().BoolVar(&head, "head", false, "check the committed tip")
	cmd.Flags().BoolVar(&staged, "staged", false, "check the index")
	cmd.Flags().BoolVar(&workingTree, "working-tree", false, "check the working files, for a --branch checked out elsewhere")
	cmd.Flags().StringSliceVar(&include, "include", nil, "add an untracked file to the capture without staging it")
	cmd.Flags().StringSliceVar(&only, "only", nil, "check only these changed ports, and the changed ports they need")
	cmd.Flags().StringSliceVar(&also, "also", nil, "also build these unchanged ports against the branch")
	cmd.Flags().StringVar(&build, "build", "", "where to build, by build profile: host, minmax, macports-ci, all, or yours (default build.default)")
	cmd.Flags().StringArrayVar(&on, "on", nil, "where to build, explicit environments, as tart:15,26, github, or command; repeat for several, all of which must pass")
	cmd.Flags().StringVar(&tests, "tests", "", "declared (advisory), required, or skip")
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue the check and return")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the branch's queued or running check, keeping what it finished, and check this instead")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "build what failed in the latest check, or --only ports, at the master it started from")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "build every port, reusing no earlier build's result")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep each Tart environment's clone, stopped, where its guest finishes, for dockhand shell")
	cmd.Flags().StringVar(&variants, "variants", "", "build the one port checked with these variants, +name -name, or each: its defaults, then each variant it declares")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --variants each, make its builds without asking")
	cmd.MarkFlagsMutuallyExclusive("baseline", "also")
	cmd.MarkFlagsMutuallyExclusive("baseline", "keep")
	cmd.MarkFlagsMutuallyExclusive("build", "on")
	cmd.MarkFlagsMutuallyExclusive("head", "staged", "working-tree")
	supportsJSON(cmd)
	return cmd
}

// runQueued sees a queued run through (Design v3 §11): handed to serve and
// followed when serve leads, otherwise run here; with enqueue, only
// queued, saying whether anything will run it.
func runQueued(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, enqueue bool, s *settings) error {
	places := s.places()
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	leader, err := e.Leader(ctx, session)
	if err != nil {
		return err
	}
	if enqueue && streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		streams.Emit(result)
	}
	switch {
	case enqueue && leader == nil:
		// Nothing leads, so a drain of its own runs it, outliving this
		// command as automatic cleanup does: an agent's check outlived
		// its tool's timeout, and a session's end killed one run here.
		pid, err := s.host.startDrain(s.openedWith)
		if err != nil {
			fmt.Fprintf(streams.Out, "%s queued, but a background drain couldn't start (%v). Start dockhand serve to run it.\n", run.Name(), err)
			return nil
		}
		fmt.Fprintf(streams.Out, "%s queued; a background drain (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), pid, run.Name())
		return nil
	case enqueue:
		fmt.Fprintf(streams.Out, "%s queued; serve (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), leader.PID, run.Name())
		return nil
	}
	return follow(ctx, e, session, run, streams, places, func(lead engine.Lead) {
		if lead.Here() {
			fmt.Fprintf(streams.Err, "%s runs here, since no dockhand serve is running. Ctrl-C stops it and keeps what finished.\n", run.Name())
			return
		}
		fmt.Fprintf(streams.Err, "%s handed to serve (pid %d); following it. Ctrl-C stops following, not the check.\n", run.Name(), cmp.Or(lead.Serve, lead.Holder).PID)
	})
}

// captureMode is the files check's flags ask for, or CaptureAuto where
// none does.
func captureMode(head, staged, workingTree bool) engine.CaptureMode {
	switch {
	case head:
		return engine.CaptureHead
	case staged:
		return engine.CaptureStaged
	case workingTree:
		return engine.CaptureWorking
	}
	return engine.CaptureAuto
}

// writePushes says where a check pushes: "only checking" never hides a
// write to your fork (Design v3 §9).
func writePushes(out io.Writer, plan model.Plan) {
	if slices.ContainsFunc(plan.Environments, func(e model.Environment) bool { return e.Provider == buildenv.GitHub }) {
		fmt.Fprintf(out, "Pushes      this revision to a %s branch in your fork for the MacPorts workflow; removes that branch after reading the run's results\n", ghactions.BranchPrefix)
	}
	if plan.Keep {
		fmt.Fprintln(out, "Keeps       each Tart environment's clone, stopped, where its guest finishes, for dockhand shell; clean or the branch's next check removes it")
	}
}

// writePlan shows a plan, with remedy saying how to give an environment
// what a target it can't build needs.
func writePlan(out io.Writer, plan model.Plan, notes []string, remedy func(model.Unmet) string) {
	var changed, extra []string
	for _, target := range plan.Targets {
		name := string(target.ID)
		switch target.Role {
		case model.Also:
			extra = append(extra, name)
		case model.Prerequisite:
			changed = append(changed, name+" (needed by --only)")
		default:
			if target.Kind == model.RevisionOnly {
				name += " (revision only)"
			}
			changed = append(changed, name)
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(out, "Changed     %s\n", prose.Few(changed, listShown, "port"))
	}
	if len(extra) > 0 {
		fmt.Fprintf(out, "Also        %s\n", prose.Few(extra, listShown, "port"))
	}
	if len(plan.Omitted) > 0 {
		var omitted []string
		for _, target := range plan.Omitted {
			omitted = append(omitted, string(target.ID))
		}
		fmt.Fprintf(out, "Left out    %s, by --only; submit still needs them checked\n", strings.Join(omitted, ", "))
	}
	var on []string
	for _, environment := range plan.Environments {
		on = append(on, engine.DescribeEnvironment(environment))
	}
	profile := ""
	// The command profile is its provider, said already.
	if plan.Profile != "" && plan.Profile != "command" {
		profile = " · build profile " + plan.Profile
	}
	fmt.Fprintf(out, "Provider    %s%s · tests %s\n", strings.Join(on, "; "), profile, plan.Tests)
	writeVariants(out, plan)
	// Tests required ask nothing of a port that declares none, which
	// passes under any policy: said, so its ✓ isn't read as tests passed.
	if plan.Tests == model.TestsRequired {
		tested, untested := plan.Untested()
		// Which do declare them is said too, beside those that don't, so
		// "asks nothing" of a stub isn't read as of the port (field
		// testing's py-mlx-vlm, 2026-10-04).
		if len(untested) > 0 && len(tested) > 0 {
			fmt.Fprintf(out, "Tests       %s, which must pass\n", prose.And(tested))
		}
		switch len(untested) {
		case 0:
		case 1:
			fmt.Fprintf(out, "No tests    %s declares no tests; --tests required adds no test requirement for this port\n", untested[0])
		default:
			fmt.Fprintf(out, "No tests    %s declare no tests; --tests required adds no test requirement for these ports\n", strings.Join(untested, ", "))
		}
	}
	for _, note := range notes {
		fmt.Fprintf(out, "            %s\n", note)
	}
	writeOrder(out, plan)
	writeGitSources(out, plan)
	writeExclusions(out, plan)
	// Each environment's remedy follows its last target it can't build.
	for _, planned := range plan.Builds {
		for i, unmet := range planned.Unmet {
			fmt.Fprintf(out, "Not built   %s on %s: %s\n", unmet.Target, engine.DescribeEnvironment(unmet.Environment), engine.UnmetWords(unmet))
			if i == len(planned.Unmet)-1 && remedy != nil {
				if words := remedy(unmet); words != "" {
					fmt.Fprintf(out, "            %s\n", words)
				}
			}
		}
	}
	for _, unresolved := range plan.Unresolved {
		fmt.Fprintf(out, "✗ %s can't be planned: %s\n", unresolved.Target.Name, unresolved.Reason)
	}
}

// writeGitSources says what each Git-fetched target's build must fetch:
// the commit its git.branch names as the check is planned, which its build
// is checked against. A target is said once where its environments expect
// the same, as they do but where one evaluates another git.branch. Those
// --only left out follow, marked so: the check doesn't build them, but an
// earlier check's result of one stands only where it fetched that commit.
func writeGitSources(out io.Writer, plan model.Plan) {
	var lines []string
	add := func(line string) {
		if !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	for _, planned := range plan.Builds {
		for _, id := range planned.Order {
			if source, ok := planned.Git[id]; ok {
				add(fmt.Sprintf("%s: %s", id, engine.GitSourceWords(source)))
			}
		}
	}
	for _, planned := range plan.Builds {
		for _, target := range plan.Omitted {
			if source, ok := planned.Git[target.ID]; ok {
				add(fmt.Sprintf("%s (left out): %s", target.ID, engine.LeftOutSourceWords(source)))
			}
		}
	}
	for i, line := range lines {
		label := ""
		if i == 0 {
			label = "Git"
		}
		fmt.Fprintf(out, "%-12s%s\n", label, line)
	}
}

// listShown is how many of a long list a line names before it counts the
// rest; --json carries every one (the command-line UX review's §10).
const listShown = 8

// writeOrder shows the order the plan builds in: one line where every
// environment builds in the plan's order, and a line for each where
// their dependencies put them in different orders.
func writeOrder(out io.Writer, plan model.Plan) {
	if len(plan.Targets) < 2 {
		return
	}
	names := func(ids []model.TargetID) string {
		var words []string
		for _, id := range ids {
			words = append(words, string(id))
		}
		return prose.FewJoined(words, listShown, "port", " → ")
	}
	if plan.SameOrder() {
		var order []model.TargetID
		for _, target := range plan.Targets {
			order = append(order, target.ID)
		}
		fmt.Fprintf(out, "Order       %s\n", names(order))
		return
	}
	for i, planned := range plan.Builds {
		label := "Order      "
		if i > 0 {
			label = "           "
		}
		fmt.Fprintf(out, "%s on %s: %s\n", label, engine.DescribeEnvironment(planned.Environment), names(planned.Order))
	}
}

// writeExclusions shows what the plan doesn't build where, and why: once
// for a port every environment excludes for the same reason, and with the
// environments where only some do.
func writeExclusions(out io.Writer, plan model.Plan) {
	for _, x := range plan.GroupedExclusions() {
		if x.Everywhere(plan) {
			fmt.Fprintf(out, "Excluded    %s: %s\n", x.Target, x.Reason)
			continue
		}
		var where []string
		for _, environment := range x.Environments {
			where = append(where, engine.DescribeEnvironment(environment))
		}
		fmt.Fprintf(out, "Excluded    %s on %s: %s\n", x.Target, strings.Join(where, "; "), x.Reason)
	}
}

// follow shows a run's progress until it ends, driving it here where
// nobody else sees it through, after before says who does, and reports its
// outcome.
func follow(ctx context.Context, e *engine.Engine, session *coord.Session, run model.Run, streams term.Streams, places config.Places, before func(engine.Lead)) error {
	journal := &journal{e: e, run: run.ID, out: streams.Err}
	watching, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			journal.show(watching)
			select {
			case <-watching.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()
	// A serve that started meanwhile may have taken the run; then it is
	// followed instead, and said.
	run, taker, err := e.SeeThrough(ctx, session, run.ID, before)
	if taker != nil {
		fmt.Fprintf(streams.Err, "%s was taken by serve (pid %d); following it.\n", run.Name(), taker.PID)
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		err = &term.ExitError{Code: term.ExitInterrupted, Message: fmt.Sprintf("stopped following %s; it continues (dockhand wait %s)", run.Name(), run.Name())}
	}
	stop()
	<-done
	journal.show(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return report(context.WithoutCancel(ctx), e, run, streams, places)
}

// journal prints a run's progress events as they are appended.
type journal struct {
	e    *engine.Engine
	run  model.RunID
	out  io.Writer
	last int64
}

func (j *journal) show(ctx context.Context) {
	events, _ := j.e.RunEvents(ctx, j.run, j.last)
	for _, event := range events {
		j.last = event.Sequence
		switch event.Kind {
		case model.EventProgress, model.EventTargetResult, model.EventExecutionRetry, model.EventExecutionState:
			fmt.Fprintf(j.out, "  %s\n", event.Message)
		case model.EventArchiveAltered, model.EventArchiveGone:
			// A kept archive found altered, or gone, is marked, where it
			// was in the journal alone (the rc8 full stage's D-T1).
			fmt.Fprintf(j.out, "  ! %s\n", event.Message)
		}
	}
}

// report prints a finished run's results and returns the exit its outcome
// calls for.
func report(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places) error {
	if run.BaselineOf != "" {
		return reportBaseline(ctx, e, run, streams)
	}
	out := streams.Out
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = evidenceView(evidence)
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	writeResults(out, "  ", evidence, false)
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	fmt.Fprintln(out)
	switch run.State {
	case model.RunPassed:
		// A check of files an earlier check already built finishes at
		// once: which check's results it took is said up front, so the
		// instant pass isn't a surprise (the Vx port's field testing).
		if from := evidence.ReusedAll(); len(from) > 0 {
			fmt.Fprintf(out, "Passed for %s, reusing %s's results: the same inputs, so nothing was built (--fresh builds again).\n", engine.Describe(revision), prose.And(from))
			return nil
		}
		fmt.Fprintf(out, "Passed for %s.\n", engine.Describe(revision))
		return nil
	case model.RunFailed:
		writeFailureLines(out, evidence)
		if logs, err := e.Logs(ctx, run.ID); err == nil {
			writeFailureLogs(out, places, logs)
		}
		if err := hintBaseline(ctx, e, out, run); err != nil {
			fmt.Fprintf(streams.Err, "baseline: %v\n", err)
		}
		return term.Exitf(term.ExitCheckFailed, "%s failed for %s: %s. Logs: dockhand logs %s", run.Name(), engine.Describe(revision), run.Detail, run.Name())
	case model.RunCanceled:
		return stoppedExit(run, evidence)
	}
	return term.Exitf(term.ExitAttention, "%s needs attention: %s", run.Name(), run.Detail)
}

// failureLines is how many of a failed step's last lines a check's
// report shows.
const failureLines = 15

// writeFailureLines shows, for each failure no reading of its log
// explained, the last lines its failed step printed.
func writeFailureLines(out io.Writer, evidence engine.Evidence) {
	for _, target := range evidence.Targets {
		for i := range target.Outcomes {
			// A dependency's failure is said with MacPorts' line naming the
			// dependency, which the log's last lines may not hold.
			if cell := target.Outcomes[i]; cell.Kind == engine.CellRecorded && cell.Outcome == model.OutcomeFailed && cell.Phase == model.PhaseDependencies {
				fmt.Fprintf(out, "%s: a dependency failed on %s: %s\n\n", target.Target.ID, engine.DescribeEnvironment(evidence.Plan.Environments[i]), cell.Detail)
				continue
			}
			lines := engine.FailureLines(evidence, target, i, failureLines)
			if len(lines) == 0 {
				continue
			}
			where := ""
			if phase := target.Outcomes[i].Phase; phase != "" {
				where = " at " + string(phase)
			}
			if len(evidence.Plan.Environments) > 1 {
				where += " on " + engine.DescribeEnvironment(evidence.Plan.Environments[i])
			}
			fmt.Fprintf(out, "%s failed%s; the last it printed:\n", target.Target.ID, where)
			for _, line := range lines {
				fmt.Fprintf(out, "    %s\n", line)
			}
			fmt.Fprintln(out)
		}
	}
}

// stoppedExit says what a check that was stopped leaves: what it finished,
// or nothing, when it finished nothing, as one cancelled while queued.
func stoppedExit(run model.Run, evidence engine.Evidence) error {
	if !evidence.Recorded() {
		return term.Exitf(term.ExitInterrupted, "%s stopped before anything finished", run.Name())
	}
	return term.Exitf(term.ExitInterrupted, "%s stopped; finished results are kept", run.Name())
}

// stopWords say what cancelling a check did: one only queued is cancelled
// before it started, and one running keeps what it finished.
func stopWords(run model.Run, wasQueued bool) string {
	if wasQueued {
		return fmt.Sprintf("Canceled %s before it started.", run.Name())
	}
	return fmt.Sprintf("Stopped %s; what it finished is kept.", run.Name())
}

// writeFailureLogs lists where each failure's log is, by environment and
// attempt, .gz once compressed, and why an attempt was tried again, where
// a failed check said only "dockhand logs check-N" (the exiv2 field notes,
// 2026-10-19).
func writeFailureLogs(out io.Writer, places config.Places, logs engine.RunLogs) {
	var lines []string
	for _, execution := range logs.Executions {
		x := execution.Execution
		where := fmt.Sprintf("%s, attempt %d", engine.DescribeEnvironment(x.Environment), x.Attempt)
		if x.State == model.ExecutionInfrastructure && x.Detail != "" {
			lines = append(lines, fmt.Sprintf("  %s was tried again: %s", where, x.Detail))
		}
		for _, result := range execution.Results {
			if result.Log != "" && (result.Outcome == model.OutcomeFailed || result.Outcome == model.OutcomeInterrupted || result.Tests.Failed()) {
				lines = append(lines, fmt.Sprintf("  %s on %s: %s", result.Target, where, places.Tilde(buildlog.Where(result.Log))))
			}
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, "Logs:")
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out)
}

// hintBaseline points a failed check to check --baseline when a baseline
// can say something about what failed: whether master fails the same way
// (Design v3 §6.8). That is a port master has that failed at install or
// test; a lint, fetch, or checksum failure is the branch's own.
func hintBaseline(ctx context.Context, e *engine.Engine, out io.Writer, run model.Run) error {
	offer, err := e.BaselineCandidates(ctx, run)
	if err != nil {
		return err
	}
	// A port whose tests are new here is said, where a baseline would
	// compare tests master doesn't run (the exiv2 field notes, 2026-10-19).
	if len(offer.NewTests) > 0 {
		fmt.Fprintln(out, engine.NewTestsWords(offer.NewTests)+".")
	}
	ports, base := offer.Ports, offer.Base
	if len(ports) == 0 {
		return nil
	}
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return err
	}
	fails := "fails"
	if len(ports) > 1 {
		fails = "fail"
	}
	fmt.Fprintf(out, "To see whether %s %s at master %s too: dockhand check --baseline --branch %s\n", strings.Join(ports, ", "), fails, base.Short(), branch.ShortName())
	return nil
}

// writeResults shows each target's result, indented. On one environment
// it is a line each, "jq  ✓", the environment named when where says so.
// On several it is a grid, a row per target and a column per environment
// headed by its release, macOS 26, since the plan named the environments
// in full.
func writeResults(out io.Writer, indent string, evidence engine.Evidence, where bool) {
	environments := evidence.Plan.Environments
	// The releases a profile chose not to build on are left out, as the
	// pull request leaves them out, and so are those build.default
	// requires that no check planned: a check on 12 and 27 is of those
	// two, and which releases nothing covered is submit's to say.
	shown := evidence.Checked()
	if len(shown) <= 1 {
		width := 0
		for _, target := range evidence.Targets {
			width = max(width, len(target.Target.ID))
		}
		for _, target := range evidence.Targets {
			var cells []string
			for _, i := range shown {
				cell := resultWords(evidence, target, i)
				if where {
					cell = engine.DescribeEnvironment(environments[i]) + " " + cell
				}
				cells = append(cells, cell)
			}
			fmt.Fprintf(out, "%s%-*s  %s\n", indent, width, target.Target.ID, strings.Join(cells, "   "))
		}
		return
	}
	grid := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintf(grid, "%sPORT", indent)
	for _, i := range shown {
		fmt.Fprintf(grid, "\t%s", engine.EnvironmentHeading(environments[i], environments))
	}
	fmt.Fprintln(grid)
	for _, target := range evidence.Targets {
		fmt.Fprintf(grid, "%s%s", indent, target.Target.ID)
		for _, i := range shown {
			fmt.Fprintf(grid, "\t%s", resultWords(evidence, target, i))
		}
		fmt.Fprintln(grid)
	}
	_ = grid.Flush()
}

// resultWords are a target's result in one environment, as the pull
// request words it, and on the terminal, why no later check can reuse a
// passed one, where its rebuild was silent (the architecture review's
// L3a).
func resultWords(evidence engine.Evidence, target engine.TargetEvidence, environment int) string {
	words := engine.EvidenceWords(evidence, target, environment, engine.AcceptedFailure{})
	// A cell another check gave names it, unless its tests' reading did.
	if check := evidence.CellCheck(target.Outcomes[environment]); check != "" && !strings.Contains(words, check) {
		words += " (" + check + ")"
	}
	if reason := target.Outcomes[environment].NotReusable; reason != "" {
		words += "; not reusable: " + reason
	}
	return words
}

// checkResult is a run's --json result, without its targets' results.
func checkResult(ctx context.Context, e *engine.Engine, run model.Run) (checkJSON, error) {
	result := checkJSON{Targets: []targetJSON{}}
	summary, err := e.CheckSummary(ctx, run)
	if err != nil {
		return result, err
	}
	revisionResult, planResult, runResult := revisionView(summary.Revision), planView(summary.Plan), runView(run)
	result.Branch, result.Revision, result.Plan, result.Run = summary.Branch.ShortName(), &revisionResult, &planResult, &runResult
	result.Altered = summary.Altered
	return result, nil
}

// previewBaseline shows the baseline check --baseline would run, and
// runs nothing.
func previewBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch, only []string) error {
	baseline, err := e.PreviewBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
	writePlan(streams.Out, baseline.Plan, e.PlanNotes(ctx, baseline.Plan), e.Remedy)
	planned := planView(baseline.Plan)
	streams.Emit(checkJSON{Branch: branch.ShortName(), Plan: &planned, Targets: []targetJSON{}})
	return nil
}

// runBaseline plans a baseline of the branch's latest check and runs it,
// here or through serve.
func runBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch, only []string, enqueue bool, s *settings) error {
	baseline, err := e.PlanBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
	run, err := e.EnqueueBaseline(ctx, branch, baseline, model.OriginPerson)
	if err != nil {
		return err
	}
	return runQueued(ctx, e, run, streams, enqueue, s)
}

// writeBaselineHeader says what a baseline builds, and what it leaves out.
func writeBaselineHeader(out io.Writer, branch model.Branch, baseline engine.Baseline) {
	var names []string
	for _, target := range baseline.Plan.Targets {
		names = append(names, string(target.ID))
	}
	fmt.Fprintf(out, "%s · baseline of %s: %s at master %s\n", branch.ShortName(), baseline.Of.Name(), strings.Join(names, ", "), baseline.Revision.Source.Commit.Short())
	if len(baseline.New) > 0 {
		fmt.Fprintf(out, "  · left out: %s, which the branch adds, so master has nothing to compare\n", strings.Join(baseline.New, ", "))
	}
	if len(baseline.Unchanged) > 0 {
		fmt.Fprintf(out, "  · %s. Use --only to include them.\n", engine.UnchangedWords(baseline.Unchanged, baseline.Of))
	}
	if len(baseline.Skipped) > 0 {
		fmt.Fprintf(out, "  · skipped in this baseline: %s failed at lint, fetch, or checksum. Use --only to include them.\n", strings.Join(baseline.Skipped, ", "))
	}
}

// reportBaseline sets each port's result at the base beside its result in
// the check the baseline looks into. It says what happened in each run and
// nothing more: a baseline never establishes a cause, and never fails.
func reportBaseline(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams) error {
	out := streams.Out
	base, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	branch, err := e.RunEvidence(ctx, run.BaselineOf)
	if err != nil {
		return err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = evidenceView(base)
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	writeBaselineResults(out, base, branch, revision.Source.Commit.Short())
	switch run.State {
	case model.RunCanceled:
		return stoppedExit(run, base)
	case model.RunAttention:
		return term.Exitf(term.ExitAttention, "%s needs attention: %s", run.Name(), run.Detail)
	}
	return nil
}

// writeBaselineResults sets each port's result at the base beside the
// branch's, in each environment where the baseline built it, and where
// the check failed it but master doesn't build it. A baseline rebuilds a
// port only where it failed, so elsewhere there is nothing to set beside.
func writeBaselineResults(out io.Writer, base, branch engine.Evidence, master string) {
	for _, row := range engine.BaselineComparison(base, branch) {
		fmt.Fprintf(out, "%s at master %s · %s\n", row.Target, master, engine.DescribeEnvironment(row.Environment))
		if row.Excluded != "" {
			fmt.Fprintf(out, "  · not built at the base: %s\n", row.Excluded)
			continue
		}
		fmt.Fprintf(out, "  %s\n", baselineWords(row.Base, row.Branch, branch.Run.Name()))
	}
}

// baselineTestWords sets a port's tests at the base beside its tests on
// the branch, where either failed though the policy let the port pass: what
// a baseline run for advisory test failures is for, as uvw's failed at the
// base too, and uvw2's passed there.
func baselineTestWords(base, branch model.TestOutcome, check string) string {
	verb := func(tests model.TestOutcome) string {
		if tests == model.TestsTimedOut {
			return "time out"
		}
		return "fail"
	}
	switch {
	case base.Failed() && branch.Failed():
		return fmt.Sprintf("tests %s at the base and in %s; the failure also occurs without this branch's changes.", verb(base), check)
	case branch.Failed() && base == model.TestsPassed:
		return fmt.Sprintf("its tests pass at the base, and %s in %s; the cause isn't established.", verb(branch), check)
	case branch.Failed():
		return fmt.Sprintf("its tests %s in %s, and weren't run at the base (%s), so there's nothing to set beside them.", verb(branch), check, base)
	case branch == model.TestsPassed:
		return fmt.Sprintf("its tests %s at the base, and pass in %s.", verb(base), check)
	}
	return fmt.Sprintf("its tests %s at the base, and weren't run in %s (%s).", verb(base), check, branch)
}

func baselineWords(base, branch model.TargetResult, check string) string {
	passed := func(r model.TargetResult) bool { return r.Outcome == model.OutcomePassed }
	switch {
	case base.Outcome != model.OutcomePassed && base.Outcome != model.OutcomeFailed:
		return fmt.Sprintf("· not built at the base (%s)", base.Outcome)
	case passed(base) && passed(branch) && (base.Tests.Failed() || branch.Tests.Failed()):
		return "✓ builds at the base, as it does on the branch; " + baselineTestWords(base.Tests, branch.Tests, check)
	case passed(base) && passed(branch):
		return "✓ builds at the base, as it does on the branch."
	case passed(base):
		return fmt.Sprintf("✓ builds at the base. This branch's result differs (%s); the cause isn't established.", check)
	case passed(branch):
		return fmt.Sprintf("✗ fails at the base, at %s, and builds on the branch (%s).", base.Phase, check)
	}
	return fmt.Sprintf("✗ fails at the base too, at %s. Both results are kept; the cause isn't established.", base.Phase)
}

// besideWords says how to go on from a check refused beside the branch's
// one queued or running (engine.BesideRefuse): wait resumes one stopped,
// and --replace checks again, keeping what it finished.
func besideWords(err error, revision model.Revision) error {
	active := new(engine.ActiveRunError)
	if !errors.As(err, &active) {
		return err
	}
	switch {
	case active.SameRevision && active.Stopped:
		// Stopped, not running: wait resumes it, where "already running"
		// sent a person to follow a run nothing was running (field
		// testing's rust check, 2026-10-15).
		return fmt.Errorf("%v, or --replace checks again, keeping what it finished", active)
	case active.SameRevision:
		return active
	}
	return fmt.Errorf("%s is %s for %s; --replace stops it, keeping what it finished, and checks %s instead", active.Run.Name(), active.Run.State, engine.Describe(active.Checking), engine.Describe(revision))
}

// sayReplaced says each run a new check stopped, and returns their names.
func sayReplaced(out io.Writer, queued engine.Queued) []string {
	var stopped []string
	for i, run := range queued.Replaced {
		if run.State == model.RunCanceled {
			fmt.Fprintln(out, stopWords(run, queued.Was[i].State == model.RunQueued))
		} else {
			fmt.Fprintf(out, "%s: stop requested; the process running it stops it at its next step.\n", run.Name())
		}
		stopped = append(stopped, run.Name())
	}
	return stopped
}

// writeVariants says what --variants builds: the one port with its
// variants in place of its defaults, or, with each, its defaults and then
// each variant it declares.
func writeVariants(out io.Writer, plan model.Plan) {
	var builds []string
	var port string
	for _, target := range plan.Targets {
		if spec := target.Target.VariantSpec(); spec != "" {
			port = target.Target.Name
			builds = append(builds, spec)
		}
	}
	switch {
	case plan.EachVariant && len(builds) > 0:
		fmt.Fprintf(out, "Variants    %s with default variants, then each of %s added to the defaults (excluding universal)\n", port, strings.Join(builds, ", "))
	case plan.Variants != "" && len(builds) > 0:
		fmt.Fprintf(out, "Variants    %s %s, in place of its defaults\n", port, plan.Variants)
	}
}

// variantBuildsToAsk is how many builds --variants each makes, targets
// times environments, before check asks first: each is a whole build.
const variantBuildsToAsk = 12

// confirmVariantBuilds asks before a --variants each check makes more
// than variantBuildsToAsk builds; without a terminal, --yes goes ahead.
func confirmVariantBuilds(streams term.Streams, plan model.Plan, yes bool) error {
	builds := plan.BuildCount()
	if !plan.EachVariant || builds <= variantBuildsToAsk || yes {
		return nil
	}
	ok, _, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? --variants each will run %d full builds. Continue? [y/N] ", builds),
		Undone: fmt.Sprintf("nothing was checked (--variants each makes %d builds here)", builds), ByYes: "--yes builds them"})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nothing was checked")
	}
	return nil
}
