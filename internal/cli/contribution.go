package cli

import (
	"encoding/json"
	"fmt"

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
		command.Long = short + ".\n\nSelect a unique open contribution by port/subport name, --branch, or the current branch. --change selects an exact contribution, including historical work. Abandon is local only and refuses pending jobs; wait or cancel them first. Refresh observes the associated PR without changing it, and preserves newer local edits. Status remains a read-only snapshot."
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
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Contribution %s: %s\n%s\n", result.Change.ID, result.Change.Disposition, result.Detail)
			return err
		}
		commands = append(commands, command)
	}
	return commands
}
