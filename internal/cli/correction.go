package cli

import (
	"crypto/rand"
	"fmt"
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) correctionCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []record.Action{record.Amend, record.Rebase} {
		var branch, title string
		var diff, noPublish, skipVerify, detach, trace bool
		var build buildOptions
		var destination publish.Options
		short := "Replace a contribution's commit with your checkout's changes and verify it"
		long := "Amend captures the tracked checkout of the contribution's branch as its new commit; stage intended additions and deletions first, since the capture takes what Git tracks. --branch selects committed contents of a branch instead. --title replaces the commit title and keeps its body. The replacement is verified and its PR updated, in the foreground through both; --no-publish (-P) stops after verification, --skip-verify (-V) updates the PR without building and says so in its body, both together stop at the branch, and --detach returns once the work is accepted. --diff previews without accepting work or moving branches."
		if action == record.Rebase {
			short = "Reapply a contribution onto fresh MacPorts master and verify it"
			long = "Rebase fetches MacPorts master and reapplies the contribution as one commit in a disposable workspace, then moves the branch to the result. Switch away from the branch before rebasing it. A conflict preserves that workspace and leaves the original branch intact. The replacement is verified and its PR updated, in the foreground through both; --no-publish (-P) stops after verification, --skip-verify (-V) updates the PR without building and says so in its body, both together stop at the branch, and --detach returns once the work is accepted. --diff previews without accepting work or moving branches."
		}
		command := &cobra.Command{Use: string(action), Short: short, Args: cobra.NoArgs,
			Long: long,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
					return fmt.Errorf("branch must name a literal local branch")
				}
				if diff && (noPublish || skipVerify || detach || trace || build.dependents) {
					return fmt.Errorf("--diff cannot be combined with publication or verification attachment flags")
				}
				if build.dependents && skipVerify {
					return fmt.Errorf("--dependents requires verification; omit --skip-verify")
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
						return r.emit(bound)
					}
					_, err = fmt.Fprint(cmd.OutOrStdout(), bound.Diff)
					return err
				}
				services, err := r.build(cmd.Context(), config)
				if err != nil {
					return err
				}
				defer services.Close()
				input := workflow.CorrectionRequest{KeepFailed: build.keepFailed, ID: record.RequestID("request_" + rand.Text()), Action: action, Title: title, Branch: branch, Preview: diff, IncludeDependents: build.dependents, SkipVerify: skipVerify}
				if !noPublish {
					input.Publication = &destination
				}
				bound, err := services.BindCorrection(cmd.Context(), input, record.TestPolicy(build.tests), build.fromSource)
				if err != nil {
					return publicationIntakeHint(err, !noPublish)
				}
				receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
				if err != nil {
					return err
				}
				progress.VerboseReport(cmd.Context(), "Accepted correction %s for %s", receipt.JobID, bound.Branch)
				milestone := workflow.Completion
				if detach {
					milestone = workflow.Admission
				}
				return r.attach(cmd, services, receipt.JobID, milestone, trace, false, &receipt)
			},
		}
		command.Flags().StringVar(&branch, "branch", "", "Select a tracked local contribution branch")
		command.Flags().StringVar(&title, "title", "", "Replace the contribution commit title, preserving its body")
		command.Flags().BoolVar(&diff, "diff", false, "Preview without moving branches or accepting work")
		command.Flags().BoolVarP(&noPublish, "no-publish", "P", false, "Leave the PR untouched; with --skip-verify, stop at the replaced branch")
		command.Flags().BoolVarP(&skipVerify, "skip-verify", "V", false, "Update the PR without a local build; the PR body discloses it")
		command.Flags().BoolVar(&detach, "detach", false, "Return once the correction is accepted and admitted; wait or start finishes it")
		command.Flags().BoolVar(&trace, "trace", false, "Follow build logs on stderr through completion; implies --debug")
		command.MarkFlagsMutuallyExclusive("detach", "trace")
		command.MarkFlagsMutuallyExclusive("trace", "skip-verify")
		build.flags(command, r.config)
		publicationFlags(command, &destination)
		commands = append(commands, command)
	}
	var branch string
	command := &cobra.Command{Use: "reassociate <change_id> --branch NAME", Short: "Associate a renamed branch with its existing contribution", Long: "Tell the contribution that its local branch now has another name. The commit the contribution tracks must be the new branch's head; the recorded published head branch on the fork is unchanged, so publication still updates the same PR, and the local name only locates the commit.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
			return r.emit(change)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s now follows %s; revision %s. Existing PR identity is preserved.\n", change.ID, change.Branch, change.CurrentRevision)
		return err
	}}
	command.Flags().StringVar(&branch, "branch", "", "New local branch name")
	return append(commands, command)
}
