package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func queueCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "List the checks queued and running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			runs, err := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
			if err != nil {
				return err
			}
			ctx, session, end, err := observing(ctx, e)
			if err != nil {
				return err
			}
			defer end()
			serve, err := readServe(ctx, e)
			if err != nil {
				return err
			}
			if streams.json() {
				result := queueJSON{Serve: serveWords(serve), ServeState: serveView(serve), Runs: []queuedRunJSON{}}
				for i := len(runs) - 1; i >= 0; i-- {
					queued, err := checkResult(ctx, e, runs[i])
					if err != nil {
						return err
					}
					row := queuedRunJSON{runJSON: *queued.Run, Branch: queued.Branch, Revision: queued.Revision.Description, Environments: queued.Plan.Environments}
					if row.Stopped, err = engine.Stopped(ctx, session, runs[i]); err != nil {
						return err
					}
					result.Runs = append(result.Runs, row)
				}
				streams.emit(result)
			}
			fmt.Fprintln(streams.Out, serveWords(serve))
			if len(runs) == 0 {
				return nil
			}
			fmt.Fprintln(streams.Out)
			table := tabwriter.NewWriter(streams.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(table, "RUN\tBRANCH\tSOURCE\tON\tSTATE\tDETAIL")
			for i := len(runs) - 1; i >= 0; i-- {
				run := runs[i]
				row, err := queueRow(ctx, e, session, run)
				if err != nil {
					return err
				}
				fmt.Fprintln(table, row)
			}
			return table.Flush()
		},
	}
}

func queueRow(ctx context.Context, e *engine.Engine, session *coord.Session, run model.Run) (string, error) {
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return "", err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return "", err
	}
	plan, err := e.Plan(ctx, run.Plan)
	if err != nil {
		return "", err
	}
	var on []string
	for _, environment := range plan.Environments {
		on = append(on, environmentWords(environment))
	}
	state, detail := string(run.State), run.Detail
	if run.Origin == model.OriginServe && detail == "" {
		detail = "started by serve"
	}
	stopped, err := engine.Stopped(ctx, session, run)
	if err != nil {
		return "", err
	}
	if stopped {
		state, detail = "stopped", "the process running it ended; dockhand wait "+run.Name()+" resumes it, cancel ends it"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", run.Name(), branch.ShortName(), engine.Describe(revision), strings.Join(on, ", "), state, detail), nil
}

func waitCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "wait [check]",
		Short: "Follow a check until it ends",
		Long: `Follows a check, such as check-42, until it ends, and reports it; in a
branch's worktree, with no check named, the branch's latest. With serve
running, this only observes, and Ctrl-C stops following; with no serve,
the check runs here, as a foreground check would.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := namedOrLatest(ctx, e, args, "name a check, such as check-42; in a branch's worktree, wait follows the branch's latest check")
			if err != nil {
				return err
			}
			if run.State.Terminal() {
				return report(ctx, e, run, streams)
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
			holder, err := session.Holder(ctx, engine.RunResource(run.ID))
			if err != nil {
				return err
			}
			if leader != nil || holder != nil {
				fmt.Fprintf(streams.Err, "following %s; Ctrl-C stops following, not the check.\n", run.Name())
				return follow(ctx, e, session, run, streams, false)
			}
			fmt.Fprintf(streams.Err, "%s runs here, since no dockhand serve is running. Ctrl-C stops it and keeps what finished.\n", run.Name())
			return follow(ctx, e, session, run, streams, true)
		},
	}
}

func cancelCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [check]",
		Short: "Stop a check, keeping what finished",
		Long: `Stops a check, such as check-42, keeping what it finished; in a branch's
worktree, with no check named, the branch's latest.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := namedOrLatest(ctx, e, args, "name a check, such as check-42; in a branch's worktree, cancel stops the branch's latest check")
			if err != nil {
				return err
			}
			if run.State.Terminal() {
				return fmt.Errorf("%s already %s", run.Name(), run.State)
			}
			session, err := startSession(ctx, e, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			wasQueued := run.State == model.RunQueued
			run, err = e.RequestCancel(ctx, session, run.ID)
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"run": runView(run), "canceled": run.State == model.RunCanceled})
			if run.State == model.RunCanceled {
				fmt.Fprintln(streams.Out, stopWords(run, wasQueued))
				return nil
			}
			fmt.Fprintf(streams.Out, "%s: cancel requested; the process running it stops it at its next step.\n", run.Name())
			return nil
		},
	}
}

func logsCommand(s *settings, streams Streams) *cobra.Command {
	var port string
	var all bool
	cmd := &cobra.Command{
		Use:   "logs [check or provider run]",
		Short: "Show where a check's logs are, or one port's log",
		Long: `Shows a check's provider runs, check-42's, and where each port's log is, or
one provider run's alone, by the ID a pull request's Tested on names,
tart_7y62p4sigena6xlr, or by its provider's own reference, such as a workflow
run's URL. In a branch's worktree, with none named, it shows the branch's
latest check.

--port prints one port's log. Where its provider recorded where each step of
the build began, as Tart's does, it lists them with their lines first, and
prints from the port's own build on, leaving out its dependencies' installs
before it; --all prints the whole log.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && port == "" {
				return errors.New("--all goes with --port")
			}
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var run model.Run
			var only model.ExecutionID
			if len(args) == 0 {
				run, err = latestCheckHere(ctx, e, "name a check, such as check-42, or a provider run; in a branch's worktree, logs shows the branch's latest check")
			} else {
				run, only, err = checkOrRun(ctx, e, args[0])
			}
			if err != nil {
				return err
			}
			logs, err := e.Logs(ctx, run.ID)
			if err != nil {
				return err
			}
			if only != "" {
				logs.Executions = slices.DeleteFunc(logs.Executions, func(execution engine.ExecutionLogs) bool { return execution.Execution.ID != only })
			}
			if port == "" {
				streams.emit(logsView(logs))
				return writeLogs(streams.Out, logs)
			}
			var found model.TargetResult
			for _, execution := range logs.Executions {
				for _, result := range execution.Results {
					if string(result.Target) == port && result.Log != "" {
						found = result
					}
				}
			}
			if found.Log == "" {
				return fmt.Errorf("%s recorded no log for %s", run.Name(), port)
			}
			data, err := os.ReadFile(found.Log)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s's log for %s, %s, is gone", run.Name(), port, found.Log)
			}
			if err != nil {
				return err
			}
			streams.emit(portLogView(run, found, data))
			return writePortLog(streams.Out, found, data, all)
		},
	}
	cmd.Flags().StringVar(&port, "port", "", "print this port's log, from its own build on where its steps were recorded")
	cmd.Flags().BoolVar(&all, "all", false, "with --port, print the whole log")
	return cmd
}

// writePortLog prints a port's log. Where its provider recorded where each
// step of the build began, it lists them first, and prints from the port's
// own build on, leaving out its dependencies' installs before it: hugo's
// own phases began near line 46,400 of 47,000 (the hugo exercise). all
// prints the whole log alone, as it is printed where no steps were
// recorded.
func writePortLog(out io.Writer, result model.TargetResult, data []byte, all bool) error {
	if all || len(result.Steps) == 0 {
		_, err := out.Write(data)
		return err
	}
	fmt.Fprintf(out, "Steps in %s:\n", tilde(result.Log))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, step := range result.Steps {
		fmt.Fprintf(tw, "  %s\tline %d\n", step.Name, step.Line)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if own, ok := result.OwnBuild(); ok && own.Line > 1 {
		if rest, ok := buildlog.From(data, own.Line); ok {
			fmt.Fprintf(out, "From line %d, where %s's own build begins; --all prints the %d lines before it too, its dependencies' installs among them.\n\n", own.Line, result.Target, own.Line-1)
			_, err := out.Write(rest)
			return err
		}
		fmt.Fprintf(out, "The log ends before line %d, where %s's own build was recorded to begin, so it's printed whole.\n", own.Line, result.Target)
	}
	fmt.Fprintln(out)
	_, err := out.Write(data)
	return err
}

// latestCheckHere is the newest check of the branch checked out here, for
// logs, wait, and cancel with none named; unnamed is what to say where no
// branch is checked out.
func latestCheckHere(ctx context.Context, e *engine.Engine, unnamed string) (model.Run, error) {
	branch, err := e.Current(ctx)
	if errors.Is(err, engine.ErrNoBranch) {
		return model.Run{}, errors.New(unnamed)
	}
	if err != nil {
		return model.Run{}, err
	}
	runs, err := e.Runs(ctx, store.RunFilter{Branch: branch.ID, Limit: 1})
	if err != nil {
		return model.Run{}, err
	}
	if len(runs) == 0 {
		return model.Run{}, fmt.Errorf("%s has no check yet: dockhand check", branch.ShortName())
	}
	return runs[0], nil
}

// namedOrLatest is the check args name, or with none, the latest check of
// the branch checked out here: a branch has one check at a time, so it is
// the one queued or running, if any is. unnamed says what to do elsewhere.
func namedOrLatest(ctx context.Context, e *engine.Engine, args []string, unnamed string) (model.Run, error) {
	if len(args) == 1 {
		return e.RunNamed(ctx, args[0])
	}
	return latestCheckHere(ctx, e, unnamed)
}

// checkName is a check's name, check-42 or 42.
var checkName = regexp.MustCompile(`^(check-)?[0-9]+$`)

// checkOrRun is the check a name means, check-42, or the provider run it
// means, with the run's check.
func checkOrRun(ctx context.Context, e *engine.Engine, name string) (model.Run, model.ExecutionID, error) {
	run, err := e.RunNamed(ctx, name)
	if err == nil || checkName.MatchString(name) {
		return run, "", err
	}
	execution, runErr := e.ExecutionNamed(ctx, name)
	if runErr != nil {
		return model.Run{}, "", fmt.Errorf("%s is neither a check, such as check-42, nor a provider run, such as tart_7y62p4sigena6xlr or a workflow run's URL", name)
	}
	run, err = e.Run(ctx, execution.Run)
	return run, execution.ID, err
}

func writeLogs(out io.Writer, logs engine.RunLogs) error {
	fmt.Fprintf(out, "%s · %s", logs.Run.Name(), logs.Run.State)
	// A passed check's detail is "passed" again.
	if logs.Run.Detail != "" && logs.Run.Detail != string(logs.Run.State) {
		fmt.Fprintf(out, ": %s", logs.Run.Detail)
	}
	fmt.Fprintln(out)
	for _, execution := range logs.Executions {
		x := execution.Execution
		fmt.Fprintf(out, "  %s, attempt %d, run %s: %s", environmentWords(x.Environment), x.Attempt, x.ID, x.State)
		if x.Detail != "" {
			fmt.Fprintf(out, " (%s)", x.Detail)
		}
		if x.Reused {
			fmt.Fprint(out, ", reusing earlier builds: nothing was built")
		}
		fmt.Fprintln(out)
		if x.ProviderRef != "" {
			fmt.Fprintf(out, "    %s\n", x.ProviderRef)
		}
		for _, result := range execution.Results {
			line := fmt.Sprintf("    %s %s", result.Target, result.Outcome)
			if result.Phase != "" {
				line += " at " + string(result.Phase)
			}
			if result.Detail != "" {
				line += ": " + result.Detail
			}
			if result.ReusedFrom != "" {
				line += ", as built by run " + string(result.ReusedFrom)
			}
			if result.Log != "" {
				line += "  " + tilde(result.Log)
			}
			fmt.Fprintln(out, line)
			// A Git-fetched target's build says what it fetched, or that
			// its provider couldn't say.
			if fetch, ok := execution.Git[result.Target]; ok {
				fmt.Fprintf(out, "      %s\n", engine.FetchedWords(fetch.Expected, fetch.Fetched))
			}
			// A result with several builders shows each one's part.
			for _, part := range result.Builders {
				line := fmt.Sprintf("      %s %s", part.Builder, part.Outcome)
				if part.Outcome == model.OutcomeNotRun {
					line = fmt.Sprintf("      %s didn't build it", part.Builder)
				}
				if part.Phase != "" {
					line += " at " + string(part.Phase)
				}
				if part.Log != "" {
					line += "  " + tilde(part.Log)
				}
				fmt.Fprintln(out, line)
			}
		}
	}
	return nil
}
