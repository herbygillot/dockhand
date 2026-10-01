package command

import (
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
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/version"
)

func checkCommand(s *settings, streams Streams) *cobra.Command {
	var selector, tests, variants string
	var plan, head, staged, workingTree, enqueue, baseline, replace, fresh, yes bool
	var include, only, also, on []string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Build and test what you have, committed or not",
		Long: `Captures the branch's files, the tracked ones as they are on disk unless
--staged or --head says otherwise, as a numbered snapshot, and builds every
port the branch changes by MacPorts CI's rule: lint, fetch and checksum,
install, and declared tests (advisory unless --tests required).

--on names where it builds; every one named must pass. --only narrows the
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
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			// The result is the check's, with a baseline check.baseline runs
			// inside it.
			streams.linkSteps(&checkJSON{})
			if baseline && plan {
				return previewBaseline(ctx, e, streams, branch, only)
			}
			if baseline {
				return runBaseline(ctx, e, streams, branch, only, enqueue)
			}
			mode, err := captureMode(ctx, e, branch, selector, head, staged, workingTree)
			if err != nil {
				return err
			}
			environments, err := e.Environments(ctx, firstNonEmpty(on, s.file.Check.On))
			if err != nil {
				return err
			}
			capture, err := e.Capture(ctx, engine.CaptureRequest{Branch: branch, Mode: mode, Include: include, Plan: plan})
			if err != nil {
				return err
			}
			if tests == "" {
				tests = s.file.Check.Tests
			}
			chosen, each, err := engine.VariantsFlag(variants)
			if err != nil {
				return err
			}
			proposed, err := e.PlanCheck(ctx, engine.PlanRequest{Revision: capture.Revision, Environments: environments, Only: only, Also: also, Tests: model.TestPolicy(tests), Fresh: fresh,
				Variants: chosen, EachVariant: each})
			if err != nil {
				return err
			}
			out := streams.Out
			what := "checking " + engine.Describe(capture.Revision)
			if capture.Revision.Kind == model.RevisionSnapshot && !capture.Reused {
				what = "captured working files as " + engine.Describe(capture.Revision)
				if plan {
					what = "would capture the working files as " + engine.Describe(capture.Revision)
				}
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), what)
			if len(capture.Untracked) > 0 {
				fmt.Fprintf(out, "Left out, not tracked: %s (--include adds one)\n", strings.Join(capture.Untracked, ", "))
			}
			writePlan(out, proposed, e.PolicyNotes(proposed), e.Remedy)
			writePushes(out, proposed)
			revision, planned := revisionView(capture.Revision), planView(proposed)
			streams.emit(checkJSON{Branch: branch.ShortName(), Revision: &revision, Plan: &planned, Targets: []targetJSON{}})
			if !proposed.Runnable() {
				return errors.New("nothing was checked: " + unrunnable(proposed))
			}
			if plan {
				return nil
			}
			if err := confirmVariantBuilds(streams, proposed, yes); err != nil {
				return err
			}
			replaced, err := replaceActive(ctx, e, streams, branch, capture.Revision, replace)
			if err != nil {
				return err
			}
			run, err := e.Enqueue(ctx, branch, proposed, model.OriginPerson)
			if err != nil {
				return err
			}
			if len(replaced) > 0 {
				fmt.Fprintf(streams.Out, "%s replaces %s.\n", run.Name(), strings.Join(replaced, ", "))
			}
			err = runQueued(ctx, e, run, streams, enqueue)
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
			if exit := new(ExitError); errors.As(err, &exit) && exit.Code == 2 && s.file.Check.Baseline && !enqueue {
				finished, _ := e.RunNamed(ctx, run.Name())
				if ports, _, _ := e.BaselineCandidates(ctx, finished); len(ports) > 0 {
					fmt.Fprintln(streams.Out, "check.baseline runs it now:")
					if baselineErr := runBaseline(ctx, e, streams, branch, nil, false); baselineErr != nil {
						fmt.Fprintf(streams.Err, "baseline: %v\n", baselineErr)
					}
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "check this tracked branch")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what would be built and change nothing")
	cmd.Flags().BoolVar(&head, "head", false, "check the committed tip")
	cmd.Flags().BoolVar(&staged, "staged", false, "check the index")
	cmd.Flags().BoolVar(&workingTree, "working-tree", false, "check the working files, for a --branch checked out elsewhere")
	cmd.Flags().StringSliceVar(&include, "include", nil, "add an untracked file to the capture without staging it")
	cmd.Flags().StringSliceVar(&only, "only", nil, "check only these changed ports, and the changed ports they need")
	cmd.Flags().StringSliceVar(&also, "also", nil, "also build these unchanged ports against the branch")
	cmd.Flags().StringArrayVar(&on, "on", nil, "where to build; repeat for several, all of which must pass (default check.on)")
	cmd.Flags().StringVar(&tests, "tests", "", "declared (advisory), required, or skip")
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue the check and return")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the branch's queued or running check, keeping what it finished, and check this instead")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "build what failed in the latest check, or --only ports, at the master it started from")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "build every port, reusing no earlier build's result")
	cmd.Flags().StringVar(&variants, "variants", "", "build the one port checked with these variants, +name -name, or each: its defaults, then each variant it declares")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --variants each, make its builds without asking")
	cmd.MarkFlagsMutuallyExclusive("baseline", "also")
	cmd.MarkFlagsMutuallyExclusive("head", "staged", "working-tree")
	return cmd
}

// runQueued sees a queued run through (Design v3 §11): handed to serve and
// followed when serve leads, otherwise run here; with enqueue, only
// queued, saying whether anything will run it.
func runQueued(ctx context.Context, e *engine.Engine, run model.Run, streams Streams, enqueue bool) error {
	session, err := startSession(ctx, e, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	leader, err := session.Holder(ctx, coord.LeaderResource)
	if err != nil {
		return err
	}
	if enqueue && streams.json() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		streams.emit(result)
	}
	switch {
	case enqueue && leader == nil:
		fmt.Fprintf(streams.Out, "%s queued; nothing is running it: dockhand serve\n", run.Name())
		return nil
	case enqueue:
		fmt.Fprintf(streams.Out, "%s queued; serve (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), leader.PID, run.Name())
		return nil
	case leader != nil:
		fmt.Fprintf(streams.Err, "%s handed to serve (pid %d); following it. Ctrl-C stops following, not the check.\n", run.Name(), leader.PID)
		return follow(ctx, e, session, run, streams, false)
	}
	fmt.Fprintf(streams.Err, "%s runs here, since no dockhand serve is running. Ctrl-C stops it and keeps what finished.\n", run.Name())
	return follow(ctx, e, session, run, streams, true)
}

func firstNonEmpty(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

// captureMode is what a check captures: what was asked for; else the
// working files in the branch's own worktree; else, for a --branch checked
// out elsewhere, its committed tip when that worktree has no edits.
func captureMode(ctx context.Context, e *engine.Engine, branch model.Branch, selector string, head, staged, workingTree bool) (engine.CaptureMode, error) {
	switch {
	case head:
		return engine.CaptureHead, nil
	case staged:
		return engine.CaptureStaged, nil
	case workingTree || selector == "":
		return engine.CaptureWorking, nil
	}
	here, err := e.Current(ctx)
	if err == nil && here.ID == branch.ID {
		return engine.CaptureWorking, nil
	}
	edits, err := e.Edited(ctx, branch)
	if err != nil {
		return "", err
	}
	if len(edits) == 0 {
		return engine.CaptureHead, nil
	}
	// A branch with no commits has nothing at its head but its base, so
	// its working files are the only thing of it to check (the txt run's
	// finding 2).
	commits, err := e.CommitsAhead(ctx, branch)
	if err != nil {
		return "", err
	}
	if commits == 0 {
		return engine.CaptureWorking, nil
	}
	return "", fmt.Errorf("%s's worktree has edits (%s); choose --head for the committed tip or --working-tree for the files", branch.ShortName(), strings.Join(edits, ", "))
}

// writePushes says where a check pushes: "only checking" never hides a
// write to your fork (Design v3 §9).
func writePushes(out io.Writer, plan model.Plan) {
	if slices.ContainsFunc(plan.Environments, func(e model.Environment) bool { return e.Provider == buildenv.GitHub }) {
		fmt.Fprintf(out, "Pushes      the revision to a %s branch of your fork, where MacPorts' workflow builds it, and removes the branch once the run is read\n", ghactions.BranchPrefix)
	}
}

// unrunnable says why a plan can't be checked.
func unrunnable(plan model.Plan) string {
	if len(plan.Unresolved) > 0 {
		return "the plan is unresolved"
	}
	return "nothing in it can be built where it asks; see Not built"
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
		fmt.Fprintf(out, "Changed     %s\n", strings.Join(changed, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(out, "Also        %s\n", strings.Join(extra, ", "))
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
		on = append(on, environmentWords(environment))
	}
	fmt.Fprintf(out, "Provider    %s · tests %s\n", strings.Join(on, "; "), plan.Tests)
	writeVariants(out, plan)
	// Tests required ask nothing of a port that declares none, which
	// passes under any policy: said, so its ✓ isn't read as tests passed.
	if plan.Tests == model.TestsRequired {
		var untested []string
		for _, build := range plan.Builds {
			for _, id := range build.Untested {
				if target, ok := plan.Target(id); ok && !slices.Contains(untested, string(target.ID)) {
					untested = append(untested, string(target.ID))
				}
			}
		}
		switch len(untested) {
		case 0:
		case 1:
			fmt.Fprintf(out, "No tests    %s declares none, so requiring them asks nothing of it\n", untested[0])
		default:
			fmt.Fprintf(out, "No tests    %s declare none, so requiring them asks nothing of them\n", strings.Join(untested, ", "))
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
			fmt.Fprintf(out, "Not built   %s on %s: %s\n", unmet.Target, environmentWords(unmet.Environment), engine.UnmetWords(unmet))
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
		return strings.Join(words, " → ")
	}
	var order []model.TargetID
	for _, target := range plan.Targets {
		order = append(order, target.ID)
	}
	agree := true
	for _, planned := range plan.Builds {
		own := slices.DeleteFunc(slices.Clone(order), func(id model.TargetID) bool { return !planned.Builds(id) })
		agree = agree && slices.Equal(own, planned.Order)
	}
	if agree {
		fmt.Fprintf(out, "Order       %s\n", names(order))
		return
	}
	for i, planned := range plan.Builds {
		label := "Order      "
		if i > 0 {
			label = "           "
		}
		fmt.Fprintf(out, "%s on %s: %s\n", label, environmentWords(planned.Environment), names(planned.Order))
	}
}

// writeExclusions shows what the plan doesn't build where, and why: once
// for a port every environment excludes for the same reason, and with the
// environments where only some do.
func writeExclusions(out io.Writer, plan model.Plan) {
	type excluded struct {
		name, reason string
		where        []string
	}
	var all []excluded
	for _, planned := range plan.Builds {
		for _, exclusion := range planned.Exclusions {
			i := slices.IndexFunc(all, func(x excluded) bool { return x.name == string(exclusion.Target.ID()) && x.reason == exclusion.Reason })
			if i < 0 {
				all = append(all, excluded{name: string(exclusion.Target.ID()), reason: exclusion.Reason})
				i = len(all) - 1
			}
			all[i].where = append(all[i].where, environmentWords(planned.Environment))
		}
	}
	for _, x := range all {
		if len(x.where) == len(plan.Builds) {
			fmt.Fprintf(out, "Excluded    %s: %s\n", x.name, x.reason)
			continue
		}
		fmt.Fprintf(out, "Excluded    %s on %s: %s\n", x.name, strings.Join(x.where, "; "), x.reason)
	}
}

func environmentWords(environment model.Environment) string {
	return engine.DescribeEnvironment(environment)
}

// startSession records this process's session and keeps its heartbeat.
func startSession(ctx context.Context, e *engine.Engine, kind model.SessionKind) (*coord.Session, error) {
	c := &coord.Coordinator{Store: e.Store, Repository: e.Repository}
	session, err := c.Start(ctx, kind, version.Current().String())
	if err != nil {
		return nil, err
	}
	go session.KeepAlive(ctx)
	return session, nil
}

// follow shows a run's progress until it ends, driving it here when drive
// is set, and reports its outcome.
func follow(ctx context.Context, e *engine.Engine, session *coord.Session, run model.Run, streams Streams, drive bool) error {
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
	var err error
	if drive {
		var driven model.Run
		// Driven only to see its own run through, the engine's and the
		// providers' reports are the work behind the scenes here: the
		// journal and the report say what matters (-v shows them).
		driven, err = e.Drive(progress.Quiet(ctx), session, run.ID)
		// A serve that started meanwhile may have taken the run; then it
		// is followed instead.
		if held := new(coord.HeldError); errors.As(err, &held) {
			fmt.Fprintf(streams.Err, "%s was taken by serve (pid %d); following it.\n", run.Name(), held.Holder.PID)
			driven, err = waitFor(ctx, e, run.ID)
		}
		run = driven
	} else {
		run, err = waitFor(ctx, e, run.ID)
	}
	stop()
	<-done
	journal.show(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return report(context.WithoutCancel(ctx), e, run, streams)
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
		case "progress", "target.result", "execution.retry", "execution.state":
			fmt.Fprintf(j.out, "  %s\n", event.Message)
		}
	}
}

// waitFor polls a run until it ends or ctx does.
func waitFor(ctx context.Context, e *engine.Engine, id model.RunID) (model.Run, error) {
	for {
		run, err := e.Run(ctx, id)
		if err != nil || run.State.Terminal() {
			return run, err
		}
		select {
		case <-ctx.Done():
			return run, &ExitError{Code: 130, Message: fmt.Sprintf("stopped following %s; it continues (dockhand wait %s)", run.Name(), run.Name())}
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// report prints a finished run's results and returns the exit its outcome
// calls for.
func report(ctx context.Context, e *engine.Engine, run model.Run, streams Streams) error {
	if run.BaselineOf != "" {
		return reportBaseline(ctx, e, run, streams)
	}
	out := streams.Out
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	if streams.json() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = evidenceView(evidence)
		streams.emit(result)
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
		fmt.Fprintf(out, "Passed for %s.\n", engine.Describe(revision))
		return nil
	case model.RunFailed:
		if err := hintBaseline(ctx, e, out, run); err != nil {
			fmt.Fprintf(streams.Err, "baseline: %v\n", err)
		}
		return exitf(2, "%s failed for %s: %s. Logs: dockhand logs %s", run.Name(), engine.Describe(revision), run.Detail, run.Name())
	case model.RunCanceled:
		return stoppedExit(run, evidence)
	}
	return exitf(3, "%s needs attention: %s", run.Name(), run.Detail)
}

// stoppedExit says what a check that was stopped leaves: what it finished,
// or nothing, when it finished nothing, as one cancelled while queued.
func stoppedExit(run model.Run, evidence engine.Evidence) error {
	if !evidence.Recorded() {
		return exitf(130, "%s stopped before anything finished", run.Name())
	}
	return exitf(130, "%s stopped; finished results are kept", run.Name())
}

// stopWords say what cancelling a check did: one only queued is cancelled
// before it started, and one running keeps what it finished.
func stopWords(run model.Run, wasQueued bool) string {
	if wasQueued {
		return fmt.Sprintf("Canceled %s before it started.", run.Name())
	}
	return fmt.Sprintf("Stopped %s; what it finished is kept.", run.Name())
}

// hintBaseline points a failed check to check --baseline when a baseline
// can say something about what failed: whether master fails the same way
// (Design v3 §6.8). That is a port master has that failed at install or
// test; a lint, fetch, or checksum failure is the branch's own.
func hintBaseline(ctx context.Context, e *engine.Engine, out io.Writer, run model.Run) error {
	ports, base, err := e.BaselineCandidates(ctx, run)
	if err != nil || len(ports) == 0 {
		return err
	}
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return err
	}
	fails := "fails"
	if len(ports) > 1 {
		fails = "fail"
	}
	fmt.Fprintf(out, "To see whether %s %s at master %s too: dockhand check --baseline --branch %s\n", strings.Join(ports, ", "), fails, engine.Short(base), branch.ShortName())
	return nil
}

// writeResults shows each target's result, indented. On one environment
// it is a line each, "jq  ✓", the environment named when where says so.
// On several it is a grid, a row per target and a column per environment
// headed by its release, macOS 26, since the plan named the environments
// in full.
func writeResults(out io.Writer, indent string, evidence engine.Evidence, where bool) {
	environments := evidence.Plan.Environments
	if len(environments) <= 1 {
		width := 0
		for _, target := range evidence.Targets {
			width = max(width, len(target.Target.ID))
		}
		for _, target := range evidence.Targets {
			var cells []string
			for i := range target.Outcomes {
				cell := evidence.Words(target, i, false)
				if where {
					cell = environmentWords(environments[i]) + " " + cell
				}
				cells = append(cells, cell)
			}
			fmt.Fprintf(out, "%s%-*s  %s\n", indent, width, target.Target.ID, strings.Join(cells, "   "))
		}
		return
	}
	grid := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintf(grid, "%sPORT", indent)
	for _, environment := range environments {
		fmt.Fprintf(grid, "\t%s", engine.EnvironmentHeading(environment, environments))
	}
	fmt.Fprintln(grid)
	for _, target := range evidence.Targets {
		fmt.Fprintf(grid, "%s%s", indent, target.Target.ID)
		for i := range target.Outcomes {
			fmt.Fprintf(grid, "\t%s", evidence.Words(target, i, false))
		}
		fmt.Fprintln(grid)
	}
	_ = grid.Flush()
}

// checkResult is a run's --json result, without its targets' results.
func checkResult(ctx context.Context, e *engine.Engine, run model.Run) (checkJSON, error) {
	result := checkJSON{Targets: []targetJSON{}}
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return result, err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return result, err
	}
	plan, err := e.Plan(ctx, run.Plan)
	if err != nil {
		return result, err
	}
	revisionResult, planResult, runResult := revisionView(revision), planView(plan), runView(run)
	result.Branch, result.Revision, result.Plan, result.Run = branch.ShortName(), &revisionResult, &planResult, &runResult
	return result, nil
}

// previewBaseline shows the baseline check --baseline would run, and
// runs nothing.
func previewBaseline(ctx context.Context, e *engine.Engine, streams Streams, branch model.Branch, only []string) error {
	baseline, err := e.PreviewBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
	writePlan(streams.Out, baseline.Plan, e.PolicyNotes(baseline.Plan), e.Remedy)
	planned := planView(baseline.Plan)
	streams.emit(checkJSON{Branch: branch.ShortName(), Plan: &planned, Targets: []targetJSON{}})
	return nil
}

// runBaseline plans a baseline of the branch's latest check and runs it,
// here or through serve.
func runBaseline(ctx context.Context, e *engine.Engine, streams Streams, branch model.Branch, only []string, enqueue bool) error {
	baseline, err := e.PlanBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
	run, err := e.EnqueueBaseline(ctx, branch, baseline, model.OriginPerson)
	if err != nil {
		return err
	}
	return runQueued(ctx, e, run, streams, enqueue)
}

// writeBaselineHeader says what a baseline builds, and what it leaves out.
func writeBaselineHeader(out io.Writer, branch model.Branch, baseline engine.Baseline) {
	var names []string
	for _, target := range baseline.Plan.Targets {
		names = append(names, string(target.ID))
	}
	fmt.Fprintf(out, "%s · baseline of %s: %s at master %s\n", branch.ShortName(), baseline.Of.Name(), strings.Join(names, ", "), engine.Short(baseline.Revision.Source.Commit))
	if len(baseline.New) > 0 {
		fmt.Fprintf(out, "  · left out: %s, which the branch adds, so master has nothing to compare\n", strings.Join(baseline.New, ", "))
	}
	if len(baseline.Skipped) > 0 {
		fmt.Fprintf(out, "  · left out: %s, which failed before building, at lint, fetch, or checksum; --only builds them anyway\n", strings.Join(baseline.Skipped, ", "))
	}
}

// reportBaseline sets each port's result at the base beside its result in
// the check the baseline looks into. It says what happened in each run and
// nothing more: a baseline never establishes a cause, and never fails.
func reportBaseline(ctx context.Context, e *engine.Engine, run model.Run, streams Streams) error {
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
	if streams.json() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = evidenceView(base)
		streams.emit(result)
	}
	fmt.Fprintln(out)
	writeBaselineResults(out, base, branch, engine.Short(revision.Source.Commit))
	switch run.State {
	case model.RunCanceled:
		return stoppedExit(run, base)
	case model.RunAttention:
		return exitf(3, "%s needs attention: %s", run.Name(), run.Detail)
	}
	return nil
}

// writeBaselineResults sets each port's result at the base beside the
// branch's, in each environment where the baseline built it, and where
// the check failed it but master doesn't build it. A baseline rebuilds a
// port only where it failed, so elsewhere there is nothing to set beside.
func writeBaselineResults(out io.Writer, base, branch engine.Evidence, master string) {
	for _, target := range base.Targets {
		i := slices.IndexFunc(branch.Targets, func(t engine.TargetEvidence) bool { return t.Target.ID == target.Target.ID })
		for n, result := range target.Outcomes {
			environment := base.Plan.Environments[n]
			theirs := model.TargetResult{Outcome: model.OutcomeNotRun}
			if j := slices.Index(branch.Plan.Environments, environment); i >= 0 && j >= 0 {
				theirs = branch.Targets[i].Outcomes[j].TargetResult
			}
			planned, _ := base.Plan.In(environment)
			if !planned.Builds(target.Target.ID) && theirs.Outcome != model.OutcomeFailed {
				continue
			}
			fmt.Fprintf(out, "%s at master %s · %s\n", target.Target.ID, master, environmentWords(environment))
			if exclusion, excluded := base.Plan.ExclusionIn(environment, target.Target.ID); excluded {
				fmt.Fprintf(out, "  · not built at the base: %s\n", exclusion.Reason)
				continue
			}
			fmt.Fprintf(out, "  %s\n", baselineWords(result.TargetResult, theirs, branch.Run.Name()))
		}
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
		return fmt.Sprintf("its tests %s at the base too, as in %s, so they did before this branch.", verb(base), check)
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

// replaceActive refuses a second check of a branch while one is queued or
// running, unless replace says to stop it (decision 29). A running check is
// stopped only after asking, on a terminal; without one, --replace is the
// consent. Finished results are kept. It returns the runs it stopped.
func replaceActive(ctx context.Context, e *engine.Engine, streams Streams, branch model.Branch, revision model.Revision, replace bool) ([]string, error) {
	runs, err := e.Runs(ctx, store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunQueued, model.RunRunning}})
	if err != nil {
		return nil, err
	}
	// A baseline looks beside the check it explains, and a run asked to
	// stop is on its way out: neither is the branch's one check.
	runs = slices.DeleteFunc(runs, func(run model.Run) bool { return run.BaselineOf != "" || run.CancelRequested != nil })
	if len(runs) == 0 {
		return nil, nil
	}
	current := runs[0]
	if !replace {
		if current.Revision == revision.ID {
			return nil, fmt.Errorf("%s is already %s for these files; dockhand wait %s follows it", current.Name(), current.State, current.Name())
		}
		checking, err := e.Revision(ctx, current.Revision)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s is %s for %s; --replace stops it, keeping what it finished, and checks %s instead", current.Name(), current.State, engine.Describe(checking), engine.Describe(revision))
	}
	session, err := startSession(ctx, e, model.SessionForeground)
	if err != nil {
		return nil, err
	}
	defer session.End(context.WithoutCancel(ctx))
	var stopped []string
	for _, run := range runs {
		if run.State == model.RunRunning && streams.terminal() {
			ok, err := confirm(streams, fmt.Sprintf("? stop %s, which is running? [y/N] ", run.Name()))
			if err != nil {
				return stopped, err
			}
			if !ok {
				return stopped, fmt.Errorf("%s keeps running; nothing new was queued", run.Name())
			}
		}
		wasQueued := run.State == model.RunQueued
		run, err = e.RequestCancel(ctx, session, run.ID)
		if err != nil {
			return stopped, err
		}
		if run.State == model.RunCanceled {
			fmt.Fprintln(streams.Out, stopWords(run, wasQueued))
		} else {
			fmt.Fprintf(streams.Out, "%s: stop requested; the process running it stops it at its next step.\n", run.Name())
		}
		stopped = append(stopped, run.Name())
	}
	return stopped, nil
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
		fmt.Fprintf(out, "Variants    %s with its defaults, then with each of %s over them (universal left out)\n", port, strings.Join(builds, ", "))
	case plan.Variants != "" && len(builds) > 0:
		fmt.Fprintf(out, "Variants    %s %s, in place of its defaults\n", port, plan.Variants)
	}
}

// variantBuildsToAsk is how many builds --variants each makes, targets
// times environments, before check asks first: each is a whole build.
const variantBuildsToAsk = 12

// confirmVariantBuilds asks before a --variants each check makes more
// than variantBuildsToAsk builds; without a terminal, --yes goes ahead.
func confirmVariantBuilds(streams Streams, plan model.Plan, yes bool) error {
	builds := 0
	for _, planned := range plan.Builds {
		builds += len(planned.Order)
	}
	if !plan.EachVariant || builds <= variantBuildsToAsk || yes {
		return nil
	}
	if !streams.terminal() {
		return fmt.Errorf("nothing was checked: --variants each makes %d builds here; without a terminal, --yes builds them", builds)
	}
	ok, err := confirm(streams, fmt.Sprintf("? --variants each makes %d builds, each a whole build; go ahead? [y/N] ", builds))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nothing was checked")
	}
	return nil
}
