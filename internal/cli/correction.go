package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) correctionCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []record.Action{record.Amend, record.Rebase} {
		var branch, title string
		var diff, publication, wait, trace bool
		var build buildOptions
		var destination publish.Options
		command := &cobra.Command{Use: string(action), Short: "Correct a tracked contribution and verify it", Args: cobra.NoArgs,
			Long: "Amend captures the current tracked checkout (stage intended changes before branch adoption); --branch selects committed contents. Rebase fetches MacPorts master and reapplies the contribution as one commit in a disposable workspace. Switch away from the branch before rebasing it. Conflicts preserve that workspace and leave the original branch intact. Both commands verify the replacement and optionally update its existing PR. --diff previews without accepting work or moving branches.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
					return fmt.Errorf("branch must name a literal local branch")
				}
				if diff && (publication || wait || trace || build.dependents) {
					return fmt.Errorf("--diff cannot be combined with publication or verification attachment flags")
				}
				config, err := build.config(cmd, r.config)
				if err != nil {
					return err
				}
				if diff {
					bound, err := app.PreviewCorrection(cmd.Context(), config, workflow.CorrectionRequest{Action: action, Title: title, Branch: branch, Preview: true})
					if err != nil {
						return err
					}
					if r.json {
						return json.NewEncoder(cmd.OutOrStdout()).Encode(bound)
					}
					_, err = fmt.Fprint(cmd.OutOrStdout(), bound.Diff)
					return err
				}
				services, err := r.build(cmd.Context(), config)
				if err != nil {
					return err
				}
				defer services.Close()
				input := workflow.CorrectionRequest{ID: record.RequestID("request_" + rand.Text()), Action: action, Title: title, Branch: branch, Preview: diff, IncludeDependents: build.dependents}
				if publication {
					input.Publication = &destination
				}
				bound, err := services.BindCorrection(cmd.Context(), input, record.TestPolicy(build.tests), build.fromSource)
				if err != nil {
					return err
				}
				receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Accepted correction %s for %s.\n", receipt.JobID, bound.Branch)
				milestone := workflow.Admission
				if wait || trace {
					milestone = workflow.Completion
				}
				return r.attach(cmd, services, receipt.JobID, milestone, trace, false, &receipt)
			},
		}
		command.Flags().StringVar(&branch, "branch", "", "Select a tracked local contribution branch")
		command.Flags().StringVar(&title, "title", "", "Replace the contribution commit title, preserving its body")
		command.Flags().BoolVar(&diff, "diff", false, "Preview without moving branches or accepting work")
		command.Flags().BoolVar(&publication, "publish", false, "Verify and publish the corrected contribution")
		command.Flags().BoolVar(&wait, "wait", false, "Wait through the requested destination")
		command.Flags().BoolVar(&trace, "trace", false, "Stream logs and wait through completion")
		build.flags(command, r.config)
		publicationFlags(command, &destination)
		commands = append(commands, command)
	}
	var branch string
	command := &cobra.Command{Use: "reassociate <change_id> --branch NAME", Short: "Associate a renamed branch with its existing contribution", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !git.ValidBranchName(branch) {
			return fmt.Errorf("--branch must name a literal local branch")
		}
		services, err := r.build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		change, err := services.Reassociate(cmd.Context(), record.ChangeID(args[0]), branch)
		if err != nil {
			return err
		}
		if r.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(change)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s now follows %s; revision %s. Existing PR identity is preserved.\n", change.ID, change.Branch, change.CurrentRevision)
		return err
	}}
	command.Flags().StringVar(&branch, "branch", "", "New local branch name")
	return append(commands, command)
}
