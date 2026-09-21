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
		var dryRun, detach, trace bool
		var build buildOptions
		var destination destinationFlags
		var publication publish.Options
		short := "Replace a contribution's commit with your checkout's changes and verify it"
		long := "Amend captures the tracked checkout of the contribution's branch as its new commit; stage intended additions and deletions first, since the capture takes what Git tracks. A target, or --branch, selects the contribution instead of the current branch. --title replaces the commit title and keeps its body. The replacement is verified and its PR updated, in the foreground through both; --to verified (-P) stops after verification, --unverified (-V) updates the PR without building and says so in its body, both together stop at the branch, and --detach returns once the work is accepted. --dry-run previews without accepting work or moving branches."
		if action == record.Rebase {
			short = "Reapply a contribution onto fresh MacPorts master and verify it"
			long = "Rebase fetches MacPorts master and reapplies the contribution as one commit in a disposable workspace, then moves the branch to the result. Switch away from the branch before rebasing it. A conflict preserves that workspace and leaves the original branch intact. The replacement is verified and its PR updated, in the foreground through both; --to verified (-P) stops after verification, --unverified (-V) updates the PR without building and says so in its body, both together stop at the branch, and --detach returns once the work is accepted. --dry-run previews without accepting work or moving branches."
		}
		command := &cobra.Command{Use: string(action) + " [target]", Short: short, Args: cobra.MaximumNArgs(1),
			Long: long,
			RunE: func(cmd *cobra.Command, args []string) error {
				if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
					return fmt.Errorf("branch must name a literal local branch")
				}
				var target string
				if len(args) == 1 {
					var err error
					if target, err = portName(args[0]); err != nil {
						return err
					}
					if branch != "" {
						return fmt.Errorf("select the contribution by target or by --branch, not both")
					}
				}
				to, publishing, skipVerify, err := destination.resolve()
				if err != nil {
					return err
				}
				if dryRun && (cmd.Flags().Changed("to") || destination.unverified || detach || trace || build.dependents) {
					return fmt.Errorf("--dry-run previews only; it does not go with --to, --unverified, --detach, --trace, or --dependents")
				}
				if build.dependents && skipVerify {
					return fmt.Errorf("--dependents requires a build; it does not go with --to branch or --unverified")
				}
				if trace && to == toBranch {
					return fmt.Errorf("--to branch builds nothing to trace")
				}
				config, err := build.config(cmd, r.config)
				if err != nil {
					return err
				}
				if dryRun {
					bound, err := app.PreviewCorrection(cmd.Context(), config, workflow.CorrectionRequest{Action: action, Title: title, Target: target, Branch: branch, Preview: true})
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
				input := workflow.CorrectionRequest{KeepFailed: build.keepFailed, ID: record.RequestID("request_" + rand.Text()), Action: action, Title: title, Target: target, Branch: branch, Preview: dryRun, IncludeDependents: build.dependents, SkipVerify: skipVerify}
				if publishing {
					input.Publication = &publication
				}
				bound, err := services.BindCorrection(cmd.Context(), input, record.TestPolicy(build.tests), build.fromSource)
				if err != nil {
					return publicationIntakeHint(err, publishing)
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
		command.Flags().StringVar(&branch, "branch", "", "Select a tracked contribution branch (default: the current branch, or the target's)")
		command.Flags().StringVar(&title, "title", "", "Replace the contribution commit title, preserving its body")
		command.Flags().BoolVar(&dryRun, "dry-run", false, "Print the replacement's diff without moving branches or accepting work")
		destination.add(command, "updates")
		command.Flags().BoolVar(&detach, "detach", false, "Return once the correction is accepted and admitted; wait or serve finishes it")
		command.Flags().BoolVar(&trace, "trace", false, "Follow build logs on stderr through completion; implies --debug")
		command.MarkFlagsMutuallyExclusive("detach", "trace")
		command.MarkFlagsMutuallyExclusive("trace", "unverified")
		build.flags(command, r.config)
		publicationFlags(command, &publication)
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
