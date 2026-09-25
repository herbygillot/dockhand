package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/version"
)

func checkCommand(s *settings, streams Streams) *cobra.Command {
	var selector, tests string
	var plan, head, staged, workingTree, enqueue bool
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

Without dockhand serve, the check runs here and says so; Ctrl-C stops it
and keeps what finished. With serve running, it is handed to serve and
followed here; Ctrl-C then only stops following. -d queues it and returns.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			mode, err := captureMode(ctx, e, branch, selector, head, staged, workingTree)
			if err != nil {
				return err
			}
			environments, err := environmentsFor(e, firstNonEmpty(on, s.file.Check.On))
			if err != nil {
				return err
			}
			capture, err := e.Capture(ctx, engine.CaptureRequest{Branch: branch, Mode: mode, Include: include})
			if err != nil {
				return err
			}
			if tests == "" {
				tests = s.file.Check.Tests
			}
			proposed, err := e.PlanCheck(ctx, engine.PlanRequest{Revision: capture.Revision, Environments: environments, Only: only, Also: also, Tests: model.TestPolicy(tests)})
			if err != nil {
				return err
			}
			out := streams.Out
			what := "checking " + engine.Describe(capture.Revision)
			if capture.Revision.Kind == model.RevisionSnapshot && !capture.Reused {
				what = "captured working files as " + engine.Describe(capture.Revision)
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), what)
			if len(capture.Untracked) > 0 {
				fmt.Fprintf(out, "Left out, not tracked: %s (--include adds one)\n", strings.Join(capture.Untracked, ", "))
			}
			writePlan(out, proposed)
			if !proposed.Runnable() {
				return errors.New("nothing was checked: the plan is unresolved")
			}
			if plan {
				return nil
			}
			run, err := e.Enqueue(ctx, branch, proposed, model.OriginPerson)
			if err != nil {
				return err
			}
			session, err := startSession(ctx, e, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			leader, err := session.Holder(ctx, coord.LeaderResource)
			if err != nil {
				return err
			}
			switch {
			case enqueue && leader == nil:
				fmt.Fprintf(out, "%s queued; nothing is running it: dockhand serve\n", run.Name())
				return nil
			case enqueue:
				fmt.Fprintf(out, "%s queued; serve (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), leader.PID, run.Name())
				return nil
			case leader != nil:
				fmt.Fprintf(streams.Err, "%s handed to serve (pid %d); following it. Ctrl-C stops following, not the check.\n", run.Name(), leader.PID)
				return follow(ctx, e, session, run, streams, false)
			}
			fmt.Fprintf(streams.Err, "%s runs here, since no dockhand serve is running. Ctrl-C stops it and keeps what finished.\n", run.Name())
			return follow(ctx, e, session, run, streams, true)
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
	cmd.MarkFlagsMutuallyExclusive("head", "staged", "working-tree")
	return cmd
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
	if len(edits) > 0 {
		return "", fmt.Errorf("%s's worktree has edits (%s); choose --head for the committed tip or --working-tree for the files", branch.ShortName(), strings.Join(edits, ", "))
	}
	return engine.CaptureHead, nil
}

// environmentsFor turns --on values into environments.
func environmentsFor(e *engine.Engine, values []string) ([]model.Environment, error) {
	if len(values) == 0 {
		if _, ok := e.Providers["command"]; ok {
			return []model.Environment{{Provider: "command"}}, nil
		}
		return nil, errors.New("a check needs somewhere to build, and none is set up. Set up your own script as [providers.command] run = \"...\" in ~/.dockhand/config.toml; tart, prefix, and github arrive with the rest of v3")
	}
	var environments []model.Environment
	for _, value := range values {
		name, releases, _ := strings.Cut(value, ":")
		if _, ok := e.Providers[name]; !ok {
			switch name {
			case "tart", "prefix", "github":
				return nil, fmt.Errorf("--on %s: the %s provider is not in v3 yet; use your own script (--on command) meanwhile", value, name)
			}
			return nil, fmt.Errorf("--on %s: no provider %q is set up", value, name)
		}
		if releases != "" {
			return nil, fmt.Errorf("--on %s: the command provider builds wherever its script does, so it takes no releases", value)
		}
		environment := model.Environment{Provider: name}
		if !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	return environments, nil
}

func writePlan(out io.Writer, plan model.Plan) {
	var changed, extra []string
	for _, target := range plan.Targets {
		name := target.Target.Name
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
	var on []string
	for _, environment := range plan.Environments {
		on = append(on, environmentWords(environment))
	}
	fmt.Fprintf(out, "Provider    %s · tests %s\n", strings.Join(on, "; "), plan.Tests)
	if len(plan.Targets) > 1 {
		var order []string
		for _, target := range plan.Targets {
			order = append(order, target.Target.Name)
		}
		fmt.Fprintf(out, "Order       %s\n", strings.Join(order, " → "))
	}
	for _, exclusion := range plan.Exclusions {
		fmt.Fprintf(out, "Excluded    %s: %s\n", exclusion.Target.Name, exclusion.Reason)
	}
	for _, unresolved := range plan.Unresolved {
		fmt.Fprintf(out, "✗ %s can't be planned: %s\n", unresolved.Target.Name, unresolved.Reason)
	}
}

func environmentWords(environment model.Environment) string {
	words := environment.Provider
	if p := environment.Platform; p.Version != "" || p.Architecture != "" {
		words += strings.TrimRight(" macOS "+p.Version+" "+p.Architecture, " ")
	}
	return words
}

// startSession records this process's session and keeps its heartbeat.
func startSession(ctx context.Context, e *engine.Engine, kind model.SessionKind) (*coord.Session, error) {
	c := &coord.Coordinator{Store: e.Store, Repository: e.Repository}
	session, err := c.Start(ctx, kind, version.Current().String())
	if err != nil {
		return nil, err
	}
	go func() { _ = session.KeepAlive(ctx) }()
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
		driven, err = e.Drive(ctx, session, run.ID)
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
	return report(context.WithoutCancel(ctx), e, run, streams.Out)
}

// journal prints a run's progress events as they are appended.
type journal struct {
	e    *engine.Engine
	run  model.RunID
	out  io.Writer
	last int64
}

func (j *journal) show(ctx context.Context) {
	_ = j.e.Store.View(ctx, j.e.Repository, func(r store.Reader) error {
		for {
			events, err := r.Events(j.last, 500)
			if err != nil || len(events) == 0 {
				return err
			}
			for _, event := range events {
				j.last = event.Sequence
				if event.Run != j.run {
					continue
				}
				switch event.Kind {
				case "progress", "target.result", "execution.retry", "execution.state":
					fmt.Fprintf(j.out, "  %s\n", event.Message)
				}
			}
		}
	})
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
func report(ctx context.Context, e *engine.Engine, run model.Run, out io.Writer) error {
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	width := 0
	for _, target := range evidence.Targets {
		width = max(width, len(target.Target.Target.Name))
	}
	fmt.Fprintln(out)
	for _, target := range evidence.Targets {
		var cells []string
		for i, result := range target.Outcomes {
			environment := evidence.Plan.Environments[i]
			cell := resultWords(evidence.Plan, target.Target, environment, result)
			if len(evidence.Plan.Environments) > 1 {
				cell = environmentWords(environment) + " " + cell
			}
			cells = append(cells, cell)
		}
		fmt.Fprintf(out, "  %-*s  %s\n", width, target.Target.Target.Name, strings.Join(cells, "   "))
	}
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
		return exitf(2, "%s failed for %s: %s. Logs: dockhand logs %s", run.Name(), engine.Describe(revision), run.Detail, run.Name())
	case model.RunCanceled:
		return exitf(130, "%s stopped; finished results are kept", run.Name())
	}
	return exitf(3, "%s needs attention: %s", run.Name(), run.Detail)
}

func resultWords(plan model.Plan, target model.PlanTarget, environment model.Environment, result model.TargetResult) string {
	if engine.Excluded(plan, target, environment.Platform) {
		return "— excluded"
	}
	switch result.Outcome {
	case model.OutcomePassed:
		if result.Tests == model.TestsFailed {
			return "✓ built; tests failed (advisory)"
		}
		return "✓"
	case model.OutcomeFailed:
		return "✗ failed at " + string(result.Phase)
	case model.OutcomeBlocked:
		return "✗ blocked by a failed dependency"
	}
	return "· " + string(result.Outcome)
}
