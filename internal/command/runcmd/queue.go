package runcmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func WaitCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "wait [check]",
		Short: "Follow a check until it ends",
		Long: `Follows a check, such as check-42, until it ends, and reports it; in a
branch's worktree, with no check named, the branch's latest; -b, -p, or
--pr names a branch, for its latest check from anywhere. With serve
running, this only observes, and Ctrl-C stops following; with no serve,
the check runs here, as a foreground check would.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := branchflags.NamedOrLatest(ctx, e, streams, where, args, "name a check, such as check-42, or a branch with -b or -p; in a branch's worktree, wait follows the branch's latest check")
			if err != nil {
				return err
			}
			if run.State.Terminal() {
				return checkrun.Report(ctx, e, run, streams, s.Places(), false)
			}
			session, err := e.Session(ctx, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			return checkrun.Follow(ctx, e, session, run, streams, s.Places(), false, func(lead engine.Lead) {
				if lead.Here() {
					fmt.Fprintf(streams.Err, "No dockhand serve is running, so %s will run here. Press Ctrl-C to stop it; completed results will be saved.\n", run.Name())
					return
				}
				fmt.Fprintf(streams.Err, "Following %s. Press Ctrl-C to stop watching; the check will continue.\n", run.Name())
			})
		},
	}
	where.Register(cmd, s, "follow the latest check of")
	settings.SupportsJSON(cmd)
	return cmd
}

func CancelCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "cancel [check]",
		Short: "Stop a check, keeping what finished",
		Long: `Stops a check, such as check-42, keeping what it finished; in a branch's
worktree, with no check named, the branch's latest; -b, -p, or --pr names a
branch, for its latest check from anywhere.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			run, err := branchflags.NamedOrLatest(ctx, e, streams, where, args, "name a check, such as check-42, or a branch with -b or -p; in a branch's worktree, cancel stops the branch's latest check")
			if err != nil {
				return err
			}
			if run.State.Terminal() {
				// One that finished at once, building nothing, says whose
				// results it took (the Vx port's field testing).
				if evidence, err := e.RunEvidence(ctx, run.ID); err == nil && len(evidence.ReusedAll()) > 0 {
					return fmt.Errorf("%s already %s, reusing %s's results (the same inputs)", run.Name(), run.State, prose.And(evidence.ReusedAll()))
				}
				return fmt.Errorf("%s already %s", run.Name(), run.State)
			}
			session, err := e.Session(ctx, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			wasQueued := run.State == model.RunQueued
			run, err = e.RequestCancel(ctx, session, run.ID)
			if err != nil {
				return err
			}
			streams.Emit(map[string]any{"run": view.RunView(run), "canceled": run.State == model.RunCanceled})
			if run.State == model.RunCanceled {
				fmt.Fprintln(streams.Out, checkrun.StopWords(run, wasQueued))
				return nil
			}
			fmt.Fprintf(streams.Out, "%s: cancellation requested; the check will stop at the next step.\n", run.Name())
			return nil
		},
	}
	where.Register(cmd, s, "stop the latest check of")
	settings.SupportsJSON(cmd)
	return cmd
}

func LogsCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var port, phase string
	var full, follow bool
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "logs [check or provider run] [port]",
		Short: "Show where a check's logs are, or one port's log",
		Long: `Shows a check's provider runs, check-42's, and where each port's log is, or
one provider run's alone, by the ID a pull request's Tested on names,
tart_7y62p4sigena6xlr, or by its provider's own reference, such as a workflow
run's URL. In a branch's worktree, with none named, it shows the branch's
latest check; -b, -p, or --pr names a branch, for its latest check from
anywhere.

A port, named after the check or alone, prints that port's log: logs
check-42 semgrep, or logs semgrep for the branch's latest check; logs -p
semgrep prints it from the latest check of semgrep's branch. Where its
provider recorded where each step of the build began, as Tart's does, it
lists them with their lines first, and prints from the port's own build on,
leaving out its dependencies' installs before it; --full prints the whole
log. --phase prints one step's lines alone, logs check-42 jq --phase fetch,
from where it began to where the next began. --follow prints a port's log
as its build writes it, and the rest once the build ends, where its provider
copies it as it grows, as Tart's does.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A log is read as it is (term.Streams.Unwrapped).
			streams.Unwrapped()
			// A port named as an argument (field testing, 2026-10-02: logs
			// semgrep, and logs check-76 semgrep); with a branch named, the
			// one argument is the port, and -p's port is the default.
			// logs check-42 -p jq is check-42's jq log, as it always was:
			// a check named, -p names only the port.
			if where.Port != "" && len(args) == 1 && checkOrRunName(cmd.Context(), s, args[0]) {
				port, where = where.Port, branchflags.BranchFlags{}
			}
			if where.Explicit() {
				if len(args) == 2 {
					return errors.New("name a check or a branch, not both")
				}
				port = where.Port
				if len(args) == 1 {
					port, args = args[0], nil
				}
			}
			if len(args) == 2 {
				port, args = args[1], args[:1]
			}
			if full && port == "" {
				return errors.New("--full requires a port name, for example: dockhand logs check-42 jq --full")
			}
			if phase != "" && port == "" {
				return errors.New("--phase requires a port name, for example: dockhand logs check-42 jq --phase fetch")
			}
			if phase != "" && !model.Step(phase).Valid() {
				return fmt.Errorf("--phase takes one of the steps a build takes: %s", stepNames(model.Steps))
			}
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var run model.Run
			var only model.ExecutionID
			if len(args) == 0 {
				run, err = branchflags.LatestOf(ctx, e, streams, where, "name a check, such as check-42, a provider run, or a branch with -b or -p; in a branch's worktree, logs shows the branch's latest check")
			} else {
				run, only, err = e.RunOrExecution(ctx, args[0])
				if err != nil && port == "" && !engine.IsCheckName(args[0]) {
					// Not a check or a provider run: a port, in the
					// branch's latest check, where there's one here.
					if latest, ok, unchecked := latestLogging(ctx, e, args[0]); ok {
						run, only, port, err = latest, "", args[0], nil
					} else if unchecked != "" {
						err = fmt.Errorf("%s has no check yet, so no log of %s: dockhand check runs one", unchecked, args[0])
					} else {
						err = fmt.Errorf("%w; for a port's log outside its branch's worktree, name its check too: logs check-42 %s", err, args[0])
					}
				}
			}
			if err != nil {
				return err
			}
			if follow {
				if port == "" {
					return errors.New("--follow follows one port's log: name it, as dockhand logs check-42 jq --follow")
				}
				return followPortLog(ctx, e, streams, run, port, s.Host.Timings.FollowPoll)
			}
			logs, err := e.Logs(ctx, run.ID)
			if err != nil {
				return err
			}
			if only != "" {
				logs.Executions = slices.DeleteFunc(logs.Executions, func(execution engine.ExecutionLogs) bool { return execution.Execution.ID != only })
			}
			if port == "" {
				streams.Emit(view.LogsView(logs))
				return view.WriteLogs(streams.Out, s.Places(), logs, stoppedRun(ctx, e, logs.Run))
			}
			chosen, others, ok := logs.ForPort(port)
			if !ok {
				// A running target's log comes back when it ends; that it
				// has none yet isn't a failure.
				if run.State == model.RunRunning && !stoppedRun(ctx, e, run) {
					if phase, building := logs.Building(port); building {
						if phase != "" {
							phase = " (" + phase + ")"
						}
						return fmt.Errorf("%s is building in %s%s; its log comes back when it ends, and dockhand wait %s follows it", port, run.Name(), phase, run.Name())
					}
					return fmt.Errorf("%s is running and hasn't recorded a log for %s yet; dockhand wait %s follows it", run.Name(), port, run.Name())
				}
				return fmt.Errorf("%s recorded no log for %s", run.Name(), port)
			}
			found := chosen.Result
			data, err := buildlog.ReadFile(found.Log)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s: the log for %s is missing: %s. Cleanup removes logs %s after their results stop counting or their branch ends", run.Name(), port, found.Log, engine.AgeWords(s.File.Cleanup.Age()))
			}
			if err != nil {
				return err
			}
			viewed := view.PortLogView(run, found, data)
			viewed.Execution = string(chosen.Execution.ID)
			for _, other := range others {
				viewed.Elsewhere = append(viewed.Elsewhere, view.PortLogElsewhereJSON{Execution: string(other.Execution.ID), Environment: view.EnvironmentView(other.Execution.Environment), Outcome: string(other.Result.Outcome), Tests: string(other.Result.Tests)})
			}
			// Which build's log this is goes with the steps, or beside a log
			// printed alone or a step of it, so the log stays as it is: its
			// environment, attempt, run, and path, and what ended an earlier
			// attempt there, where exiv2's said none of it (the exiv2 field
			// notes, 2026-10-19).
			if phase != "" {
				from, to, ok := logs.StepLines(chosen, model.Step(phase))
				if !ok {
					return view.PhaseMissing(port, phase, found)
				}
				lines, ok := buildlog.Between(data, from, to)
				if !ok {
					return fmt.Errorf("%s's log ends before line %d, where its %s step was recorded to begin", port, from, phase)
				}
				viewed.Text, viewed.Phase, viewed.From = string(lines), phase, from
				streams.Emit(viewed)
				writePortLogHeading(streams.Err, s.Places(), logs, port, chosen, others)
				_, err := streams.Out.Write(lines)
				return err
			}
			where := streams.Out
			if full || len(found.Steps) == 0 {
				where = streams.Err
			}
			streams.Emit(viewed)
			writePortLogHeading(where, s.Places(), logs, port, chosen, others)
			return view.WritePortLog(streams.Out, s.Places(), found, data, full)
		},
	}
	where.Register(cmd, s, "show the latest check of")
	cmd.Flags().BoolVar(&full, "full", false, "with a port, print the whole log")
	cmd.Flags().BoolVar(&follow, "follow", false, "with a port, print its log as its build writes it, until the build ends")
	cmd.Flags().StringVar(&phase, "phase", "", "with a port, print one step of its build alone, such as fetch or install")
	cmd.MarkFlagsMutuallyExclusive("full", "follow", "phase")
	settings.SupportsJSON(cmd)
	return cmd
}

// followPortLog prints a port's log as its build writes it, from the copy
// the run's driver keeps of it (Engine.LiveLog), in any process, and the
// rest once the build's result is recorded, where logs of a running
// target said it recorded no log until the target ended (the gnome-voice
// session's notes, 2026-10-20). A check on GitHub Actions, whose log
// comes back only with its run, says so, with the run's page.
func followPortLog(ctx context.Context, e *engine.Engine, streams term.Streams, run model.Run, port string, poll time.Duration) error {
	out := streams.Out
	var printed int
	rest := func(data []byte) {
		if printed < len(data) {
			_, _ = out.Write(data[printed:])
			printed = len(data)
		}
	}
	said := false
	for {
		logs, err := e.Logs(ctx, run.ID)
		if err != nil {
			return err
		}
		if chosen, _, ok := logs.ForPort(port); ok {
			data, err := buildlog.ReadFile(chosen.Result.Log)
			if err != nil {
				return err
			}
			rest(data)
			return nil
		}
		if logs.Run.State.Terminal() {
			return fmt.Errorf("%s ended without a log of %s", logs.Run.Name(), port)
		}
		if stoppedRun(ctx, e, logs.Run) {
			return fmt.Errorf("%s stopped before %s's build ended; dockhand wait %s resumes it", logs.Run.Name(), port, logs.Run.Name())
		}
		path, err := e.LiveLog(ctx, run.ID, port)
		if err != nil {
			return err
		}
		switch {
		case path != "":
			if data, err := os.ReadFile(path); err == nil {
				rest(data)
			}
		case !said:
			providers, err := e.RunProviders(ctx, logs.Run)
			if err != nil {
				return err
			}
			if len(providers) > 0 && !slices.ContainsFunc(providers, func(p string) bool { return p != buildenv.GitHub }) {
				where := ""
				for _, execution := range logs.Executions {
					if ref := execution.Execution.ProviderRef; ref != "" {
						where = "; its run: " + ref
					}
				}
				return fmt.Errorf("%s builds on GitHub Actions, whose log comes back only when its run ends, so it can't be followed here%s", logs.Run.Name(), where)
			}
			fmt.Fprintf(streams.Err, "%s hasn't reached %s yet; following once it does.\n", logs.Run.Name(), port)
			said = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// writePortLogHeading says which build's log of a port this is: the
// environment, the attempt of how many, the provider run, how it went,
// and where its log is, .gz once compressed; what ended each earlier
// attempt in that environment, which a retry's log doesn't say; and the
// port's other builds, each with how to see its log.
func writePortLogHeading(out io.Writer, places config.Places, logs engine.RunLogs, port string, chosen engine.PortLog, others []engine.PortLog) {
	execution := chosen.Execution
	fmt.Fprintf(out, "%s's log on %s, attempt %d of %d, run %s: %s; %s\n", port, engine.DescribeEnvironment(execution.Environment), execution.Attempt, model.MaxAttempts, execution.ID,
		view.PortBuildWords(chosen.Result), places.Tilde(buildlog.Where(chosen.Result.Log)))
	for _, earlier := range logs.Executions {
		if earlier.Execution.Environment == execution.Environment && earlier.Execution.Attempt < execution.Attempt {
			ended := cmp.Or(earlier.Execution.Detail, string(earlier.Execution.State))
			fmt.Fprintf(out, "  attempt %d ended: %s\n", earlier.Execution.Attempt, ended)
		}
	}
	for _, other := range others {
		fmt.Fprintf(out, "  also on %s, attempt %d, run %s: %s; dockhand logs %s %s\n", engine.DescribeEnvironment(other.Execution.Environment), other.Execution.Attempt, other.Execution.ID, view.PortBuildWords(other.Result), other.Execution.ID, port)
	}
}

// stepNames lists steps by name.
func stepNames(steps []model.Step) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = string(step)
	}
	return strings.Join(names, ", ")
}

// latestLogging is the latest check of the branch checked out here, where
// it recorded a log for port. unchecked is that branch where it has no
// check yet, for logs to say so rather than to name a check "outside its
// branch's worktree" (the rc5 rerun's A10).
func latestLogging(ctx context.Context, e *engine.Engine, port string) (run model.Run, ok bool, unchecked string) {
	branch, err := e.Current(ctx)
	if err != nil {
		return model.Run{}, false, ""
	}
	run, found, err := e.LatestCheck(ctx, branch.ID)
	if err != nil {
		return model.Run{}, false, ""
	}
	if !found {
		return model.Run{}, false, branch.ShortName()
	}
	logs, err := e.Logs(ctx, run.ID)
	if err != nil {
		return model.Run{}, false, ""
	}
	_, _, ok = logs.ForPort(port)
	return run, ok, ""
}

// checkOrRunName reports whether name is a check's or a provider run's.
func checkOrRunName(ctx context.Context, s *settings.Settings, name string) bool {
	if engine.IsCheckName(name) {
		return true
	}
	e, err := s.Open(ctx)
	if err != nil {
		return false
	}
	defer e.Close()
	_, _, err = e.RunOrExecution(ctx, name)
	return err == nil
}

// stoppedRun reports whether a run recorded as running stopped when the
// process running it ended, which dockhand wait resumes.
func stoppedRun(ctx context.Context, e *engine.Engine, run model.Run) bool {
	if run.State != model.RunRunning {
		return false
	}
	ctx, session, end, err := e.Observing(ctx)
	if err != nil {
		return false
	}
	defer end()
	stopped, err := engine.Stopped(ctx, session, run)
	return err == nil && stopped
}
