package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) publishCommand() *cobra.Command {
	var branch, change string
	var options publish.Options
	var dryRun, wait bool
	cmd := &cobra.Command{Use: "publish [target]", Short: "Publish a verified, committed contribution to GitHub", Args: cobra.MaximumNArgs(1),
		Long: "Publish the unique open contribution for a target. Omit the target to use the current branch, or select --branch or --change explicitly. A verified user-created branch becomes a tracked contribution when publication is accepted. Uses the latest terminal verification for the committed tree and selected port; it must have passed. Its recorded build configuration is preserved. The contribution must contain one commit in one verified port directory. Existing PR bodies are preserved. Without --wait, return after driver pickup; --wait follows confirmation of the pushed head and PR metadata. Ctrl-C detaches, and wait or start resumes the durable job. GH_TOKEN, GITHUB_TOKEN, or an authenticated GitHub CLI supplies GitHub API authentication. Git uses its configured credentials.",
		RunE: func(cmd *cobra.Command, args []string) error {
			services, err := r.build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			var target string
			if len(args) == 1 {
				target = args[0]
				if target == "" {
					return fmt.Errorf("target must not be empty")
				}
			}
			input := workflow.PublicationRequest{Target: target, ChangeID: record.ChangeID(change), ID: record.RequestID("request_" + rand.Text()), Branch: branch, Options: options}
			var request workflow.Request
			if dryRun {
				request, err = services.Workflow.PlanPublication(cmd.Context(), input)
			} else {
				request, err = services.Workflow.BindPublication(cmd.Context(), input)
			}
			if err != nil {
				return err
			}
			spec := request.Spec.Publication
			if dryRun {
				if r.json {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(request.Spec)
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "Publish %s at %s\n  %s:%s -> %s:%s\n  title: %s\n  target: %s\n  verification: %s (%s %s %s; tests %s; from source %t)\n", plain(spec.HeadBranch), spec.Desired.Head, plain(spec.HeadRepository), plain(spec.HeadBranch), plain(spec.Repository), plain(spec.BaseBranch), plain(spec.Desired.Title), plain(targetLabel(request.Spec.Targets[0])), spec.EvidenceAttempt, request.Spec.Build.Platform.OS, request.Spec.Build.Platform.Version, request.Spec.Build.Platform.Architecture, request.Spec.Build.Tests, request.Spec.Build.FromSource)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "\n%s", spec.Desired.Body)
				return err
			}
			receipt, err := services.Workflow.Submit(cmd.Context(), request)
			if err != nil {
				return fmt.Errorf("accepting request %s: %w", request.ID, err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Accepted publication job %s; branch %s at %s; verification %s.\n", receipt.JobID, plain(spec.HeadBranch), spec.Desired.Head, spec.EvidenceAttempt)
			milestone := workflow.Admission
			if wait {
				milestone = workflow.Completion
			}
			return r.attach(cmd, services, receipt.JobID, milestone, false, false, &receipt)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "Publish committed contents of this local branch")
	cmd.Flags().StringVar(&change, "change", "", "Select one tracked contribution when the target is ambiguous")
	publicationFlags(cmd, &options)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the publication plan without accepting a job or writing remotely")
	cmd.Flags().BoolVar(&wait, "wait", false, "Stay until the branch and PR are confirmed")
	return cmd
}

func publicationFlags(cmd *cobra.Command, options *publish.Options) {
	cmd.Flags().StringVar(&options.Remote, "remote", "origin", "Git remote whose push URL receives the contribution")
	cmd.Flags().StringVar(&options.Upstream, "upstream", "", "Upstream Git remote (defaults to upstream, then the fork parent)")
	cmd.Flags().StringVar(&options.Base, "base", "", "PR base branch (defaults to the upstream default branch)")
}
