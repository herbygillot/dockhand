package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

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
			if streams.json() {
				result := queueJSON{Serve: serveLine(ctx, e), Runs: []queuedRunJSON{}}
				for i := len(runs) - 1; i >= 0; i-- {
					queued, err := checkResult(ctx, e, runs[i])
					if err != nil {
						return err
					}
					result.Runs = append(result.Runs, queuedRunJSON{runJSON: *queued.Run, Branch: queued.Branch, Revision: queued.Revision.Description, Environments: queued.Plan.Environments})
				}
				streams.emit(result)
			}
			fmt.Fprintln(streams.Out, serveLine(ctx, e))
			if len(runs) == 0 {
				return nil
			}
			fmt.Fprintln(streams.Out)
			table := tabwriter.NewWriter(streams.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(table, "RUN\tBRANCH\tSOURCE\tON\tSTATE\tDETAIL")
			for i := len(runs) - 1; i >= 0; i-- {
				run := runs[i]
				row, err := queueRow(ctx, e, run)
				if err != nil {
					return err
				}
				fmt.Fprintln(table, row)
			}
			return table.Flush()
		},
	}
}

func queueRow(ctx context.Context, e *engine.Engine, run model.Run) (string, error) {
	var branch model.Branch
	var revision model.Revision
	var plan model.Plan
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if branch, err = r.Branch(run.Branch); err != nil {
			return err
		}
		if revision, err = r.Revision(run.Revision); err != nil {
			return err
		}
		plan, err = r.Plan(run.Plan)
		return err
	})
	if err != nil {
		return "", err
	}
	var on []string
	for _, environment := range plan.Environments {
		on = append(on, environmentWords(environment))
	}
	detail := run.Detail
	if run.Origin == model.OriginServe && detail == "" {
		detail = "started by serve"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", run.Name(), branch.ShortName(), engine.Describe(revision), strings.Join(on, ", "), run.State, detail), nil
}

func waitCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "wait <run>",
		Short: "Follow a check until it ends",
		Long: `Follows a check, such as check-42, until it ends, and reports it. With
serve running, this only observes, and Ctrl-C stops following; with no
serve, the check runs here, as a foreground check would.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := e.RunNamed(ctx, args[0])
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
		Use:   "cancel <run>",
		Short: "Stop a check, keeping what finished",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := e.RunNamed(ctx, args[0])
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
			run, err = e.RequestCancel(ctx, session, run.ID)
			if err != nil {
				return err
			}
			if run.State == model.RunCanceled {
				fmt.Fprintf(streams.Out, "%s canceled; finished results are kept.\n", run.Name())
				return nil
			}
			fmt.Fprintf(streams.Out, "%s: cancel requested; the process running it stops it at its next step.\n", run.Name())
			return nil
		},
	}
}

func logsCommand(s *settings, streams Streams) *cobra.Command {
	var port string
	cmd := &cobra.Command{
		Use:   "logs <run>",
		Short: "Show where a check's logs are, or one port's log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return err
			}
			logs, err := e.Logs(ctx, run.ID)
			if err != nil {
				return err
			}
			if port == "" {
				return writeLogs(streams.Out, logs)
			}
			var found string
			for _, execution := range logs.Executions {
				for _, result := range execution.Results {
					if string(result.Target) == port && result.Log != "" {
						found = result.Log
					}
				}
			}
			if found == "" {
				return fmt.Errorf("%s recorded no log for %s", run.Name(), port)
			}
			data, err := os.ReadFile(found)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s's log for %s, %s, is gone", run.Name(), port, found)
			}
			if err != nil {
				return err
			}
			_, err = streams.Out.Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&port, "port", "", "print this port's log")
	return cmd
}

func writeLogs(out io.Writer, logs engine.RunLogs) error {
	fmt.Fprintf(out, "%s · %s", logs.Run.Name(), logs.Run.State)
	if logs.Run.Detail != "" {
		fmt.Fprintf(out, ": %s", logs.Run.Detail)
	}
	fmt.Fprintln(out)
	for _, execution := range logs.Executions {
		x := execution.Execution
		fmt.Fprintf(out, "  %s, attempt %d: %s", environmentWords(x.Environment), x.Attempt, x.State)
		if x.Detail != "" {
			fmt.Fprintf(out, " (%s)", x.Detail)
		}
		fmt.Fprintln(out)
		for _, result := range execution.Results {
			line := fmt.Sprintf("    %s %s", result.Target, result.Outcome)
			if result.Phase != "" {
				line += " at " + string(result.Phase)
			}
			if result.Log != "" {
				line += "  " + tilde(result.Log)
			}
			fmt.Fprintln(out, line)
		}
	}
	return nil
}
