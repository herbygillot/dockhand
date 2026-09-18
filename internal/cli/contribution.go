package cli

import (
	"fmt"
	"io"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) contributionCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []string{"abandon", "refresh"} {
		var selector workSelector
		short := "Stop pursuing a contribution, preserving its branch and history"
		if action == "refresh" {
			short = "Observe a contribution's PR and retire matching finished work"
		}
		command := &cobra.Command{Use: action + " [target]", Short: short, Args: cobra.MaximumNArgs(1)}
		detail := "Abandon is local only and refuses pending jobs; wait or cancel them first. It preserves the branch, the evidence, and any PR, and does not close the PR; a later bump starts a new contribution."
		if action == "refresh" {
			detail = "Refresh reads the PR from the forge without changing it, records its state and, while it is open, its mergeability, review, and checks, and retires a merged or closed contribution whose published revision still matches the PR head and the local branch; newer local edits keep it open. A merged contribution's branch cleanup is settled at once, and what is still owed is retried. The next periodic look is scheduled from this observation."
		}
		command.Long = short + ".\n\nSelect a unique open contribution by port/subport name, --branch, or the current branch. --change selects an exact contribution, including historical work. " + detail
		command.Flags().StringVar(&selector.branch, "branch", "", "Select a tracked contribution branch")
		command.Flags().StringVar(&selector.change, "change", "", "Select a contribution by ID, including historical work")
		command.MarkFlagsMutuallyExclusive("branch", "change")
		command.RunE = func(cmd *cobra.Command, args []string) error {
			if err := selector.validate(cmd, args); err != nil {
				return err
			}
			services, err := r.build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			selected, err := selector.contribution(cmd.Context(), services, args)
			if err != nil {
				return err
			}
			var result workflow.ContributionResult
			if action == "abandon" {
				result, err = services.Workflow.AbandonContribution(cmd.Context(), selected)
			} else {
				result, err = services.Workflow.RefreshContribution(cmd.Context(), selected)
			}
			if err != nil {
				return err
			}
			if r.json {
				return r.emit(result)
			}
			return renderContribution(cmd.OutOrStdout(), r.level(cmd), result)
		}
		commands = append(commands, command)
	}
	return commands
}

// renderContribution prints what abandon and refresh did: the port and
// branch, the PR and its observed state, and the detail; the contribution
// ID joins at -v.
func renderContribution(out io.Writer, level progress.Level, result workflow.ContributionResult) error {
	change := result.Change
	name := change.InitiatingTarget
	if name == "" && len(change.Targets) > 0 {
		name = change.Targets[0].Name
	}
	if name == "" {
		name = string(change.ID)
	}
	if _, err := fmt.Fprintf(out, "%s (%s): %s\n", plain(name), plain(change.Branch), change.Disposition); err != nil {
		return err
	}
	if level >= progress.Verbose {
		if _, err := fmt.Fprintf(out, "  contribution: %s\n", change.ID); err != nil {
			return err
		}
	}
	if pr := result.PullRequest; pr != nil && pr.Ref.URL != "" {
		text := fmt.Sprintf("  PR %s: %s", pr.Ref.URL, pr.State)
		if pr.Status != nil {
			text += "; " + pr.Status.Summary()
		}
		if _, err := fmt.Fprintln(out, plain(text)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "  %s\n", plain(result.Detail))
	return err
}
