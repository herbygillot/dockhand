package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	ErrJobFailed      = errors.New("dockhand: requested work failed")
	ErrNeedsAttention = errors.New("dockhand: requested work needs attention")
	ErrJobCanceled    = errors.New("dockhand: requested work was canceled")
)

func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled), errors.Is(err, ErrJobCanceled):
		return 130
	case errors.Is(err, ErrNeedsAttention):
		return 3
	case errors.Is(err, ErrJobFailed):
		return 2
	default:
		return 1
	}
}

type ActionResult struct {
	JobID       record.JobID `json:",omitempty"`
	Receipt     *workflow.Receipt
	Status      workflow.Status
	Interrupted bool
}

func (r *runtime) verifyCommand() *cobra.Command {
	var branch, subport string
	var build buildOptions
	var variants []string
	var wait, trace, fresh bool
	command := &cobra.Command{
		Use: "verify <port>", Short: "Verify a port from the current checkout or a committed branch",
		Long: "Verify one snapshot-relative port directory or unique directory name. By default, capture tracked working-tree contents, including staged additions and deletions. Stage new files with git add to include them. An explicit --branch selects committed contents. The captured snapshot stays fixed while you continue editing. Matching passing evidence is reused unless --fresh is supplied. The command waits for provider admission; --wait follows completion. Ctrl-C detaches without canceling accepted work.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
				return fmt.Errorf("branch must name a literal local branch")
			}
			choices, err := parseVariants(variants)
			if err != nil {
				return err
			}
			config, err := build.config(cmd, r.config)
			if err != nil {
				return err
			}
			if config.Tart.Image == "" {
				return fmt.Errorf("select a prepared local Tart image with --image")
			}
			services, err := app.Build(cmd.Context(), config)
			if err != nil {
				return err
			}
			defer services.Close()
			if branch == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Capturing working-tree source and checking the prepared image...")
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Binding committed source from %s and checking the prepared image...\n", branch)
			}
			bound, err := services.BindVerification(cmd.Context(), app.Verification{ID: record.RequestID("request_" + rand.Text()), Branch: branch, Selection: macports.Selection{Selector: args[0], Subport: subport, Variants: choices}, Tests: record.TestPolicy(build.tests), FromSource: build.fromSource, Fresh: fresh})
			if err != nil {
				return err
			}
			if err := renderVerificationSource(cmd.ErrOrStderr(), bound); err != nil {
				return err
			}
			receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
			if err != nil {
				return fmt.Errorf("accepting request %s: %w", bound.Request.ID, err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Accepted job %s; source tree %s.\n", receipt.JobID, bound.Request.Spec.Source.Tree)
			milestone := workflow.Admission
			if wait || trace {
				milestone = workflow.Completion
			}
			return r.attach(cmd, services, receipt.JobID, milestone, trace, false, &receipt)
		},
	}
	command.Flags().StringVar(&branch, "branch", "", "Verify committed contents of this local branch instead of the working tree")
	command.Flags().StringVar(&subport, "subport", "", "Select one subport from the Portfile")
	command.Flags().StringArrayVar(&variants, "variant", nil, "Explicit variant choice, such as +ssl or -x11 (repeatable)")
	build.flags(command, r.config)
	command.Flags().BoolVar(&fresh, "fresh", false, "Run a new build even when previous passing evidence applies")
	command.Flags().BoolVar(&wait, "wait", false, "Stay until verification completes")
	command.Flags().BoolVar(&trace, "trace", false, "Stream build logs to stderr and wait for completion")
	return command
}
func parseVariants(values []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, value := range values {
		if len(value) < 2 || value[0] != '+' && value[0] != '-' {
			return nil, fmt.Errorf("variant %q must start with + or -", value)
		}
		name := value[1:]
		for _, c := range name {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.') {
				return nil, fmt.Errorf("invalid variant %q", value)
			}
		}
		enabled := value[0] == '+'
		if previous, ok := result[name]; ok && previous != enabled {
			return nil, fmt.Errorf("conflicting choices for variant %s", name)
		}
		result[name] = enabled
	}
	return result, nil
}
func (r *runtime) waitCommand() *cobra.Command {
	var trace bool
	command := &cobra.Command{Use: "wait <job_id>", Short: "Resume an existing job through completion", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		services, err := app.Build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		return r.attach(cmd, services, record.JobID(args[0]), workflow.Completion, trace, false, nil)
	}}
	command.Flags().BoolVar(&trace, "trace", false, "Stream build logs to stderr")
	return command
}
func (r *runtime) cancelCommand() *cobra.Command {
	var wait bool
	var reason string
	command := &cobra.Command{Use: "cancel <job_id>", Short: "Request cancellation while preserving the branch and evidence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		services, err := app.Build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		id := record.JobID(args[0])
		err = services.Workflow.Control(cmd.Context(), record.ControlRequest{ID: record.RequestID("control_" + rand.Text()), Kind: record.Cancel, Jobs: []record.JobID{id}, Reason: reason})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Cancellation requested for %s.\n", id)
		if wait {
			return r.attach(cmd, services, id, workflow.Completion, false, true, nil)
		}
		scope := workflow.Scope{Jobs: []record.JobID{id}}
		_, cycleErr := services.Workflow.Cycle(cmd.Context(), scope)
		status, statusErr := services.Workflow.Status(cmd.Context(), scope)
		return errors.Join(cycleErr, statusErr, r.result(cmd.OutOrStdout(), ActionResult{JobID: id, Status: status, Interrupted: cmd.Context().Err() != nil}))
	}}
	command.Flags().BoolVar(&wait, "wait", false, "Remain attached until the job settles")
	command.Flags().StringVar(&reason, "reason", "", "Record a cancellation reason")
	return command
}
func (r *runtime) startCommand() *cobra.Command {
	return &cobra.Command{Use: "start", Short: "Advance this repository's work until interrupted", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		services, err := app.Build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		fmt.Fprintln(cmd.ErrOrStderr(), "Driver running for this repository. Ctrl-C stops the driver; accepted work remains recorded.")
		reporter := newReporter(cmd.ErrOrStderr(), services.Workflow.Provider, false)
		services.Processes.OnCycle = reporter.cycle
		err = services.Processes.Run(cmd.Context(), services.Workflow, workflow.Scope{All: true})
		if r.json {
			encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Stopped     bool
				Interrupted bool
			}{true, cmd.Context().Err() != nil})
			err = errors.Join(err, encodeErr)
		}
		return err
	}}
}
func (r *runtime) attach(cmd *cobra.Command, services *app.Services, id record.JobID, milestone workflow.Milestone, trace, canceling bool, receipt *workflow.Receipt) error {
	reporter := newReporter(cmd.ErrOrStderr(), services.Workflow.Provider, trace)
	services.Processes.OnCycle = reporter.cycle
	status, err := services.Processes.Attach(cmd.Context(), services.Workflow, workflow.Scope{Jobs: []record.JobID{id}}, milestone, func(status workflow.Status) error { return reporter.status(cmd.Context(), status) })
	if len(status.Jobs) == 0 {
		if receipt == nil {
			return err
		}
		status = workflow.EmptyStatus(time.Time{})
	}
	outputErr := r.result(cmd.OutOrStdout(), ActionResult{JobID: id, Receipt: receipt, Status: status, Interrupted: cmd.Context().Err() != nil})
	if err != nil {
		return errors.Join(err, outputErr)
	}
	return errors.Join(outcome(status, canceling), outputErr)
}
func outcome(status workflow.Status, canceling bool) error {
	var result error
	for _, entry := range status.Jobs {
		switch entry.Job.State {
		case record.JobFailed:
			result = errors.Join(result, ErrJobFailed)
		case record.JobNeedsAttention, record.JobSuperseded:
			result = errors.Join(result, ErrNeedsAttention)
		case record.JobCanceled:
			if !canceling {
				result = errors.Join(result, ErrJobCanceled)
			}
		}
	}
	return result
}
func (r *runtime) result(out io.Writer, result ActionResult) error {
	if r.json {
		return json.NewEncoder(out).Encode(result)
	}
	if err := renderStatus(out, result.Status); err != nil {
		return err
	}
	for _, entry := range result.Status.Jobs {
		if entry.Job.State == record.JobActive || entry.Job.State == record.JobQueued {
			if _, err := fmt.Fprintf(out, "\nWork remains pending. Resume with dockhand wait %s or run dockhand start.\n", entry.Job.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderVerificationSource(out io.Writer, bound workflow.BoundVerification) error {
	spec := bound.Request.Spec
	if c := spec.Checkout; c != nil {
		branch := c.Branch
		if branch == "" {
			branch = "detached HEAD"
		}
		if _, err := fmt.Fprintf(out, "Source: working tree (%s); HEAD %s; %d modified files captured.\n", branch, c.Head, c.ModifiedFiles); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "Source: committed branch %s; commit %s.\n", bound.Request.Branch.Name, spec.Source.Commit); err != nil {
			return err
		}
	}
	for _, target := range spec.Targets {
		if _, err := fmt.Fprintf(out, "Target: %s (%s)\n", target.Name, target.Portfile); err != nil {
			return err
		}
	}
	return nil
}
