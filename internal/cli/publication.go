package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) publishCommand() *cobra.Command {
	var branch string
	var options publish.Options
	var dryRun, wait bool
	cmd := &cobra.Command{Use: "publish", Short: "Publish a verified, committed contribution to GitHub", Args: cobra.NoArgs,
		Long: "Publish the current tracked branch, or select one with --branch. Uses the latest passing verification for the complete committed tree and its recorded build configuration. The contribution must contain one commit in one tracked port directory. Existing PR bodies are preserved. Without --wait, return after driver pickup; --wait follows confirmation of the pushed head and PR metadata. Ctrl-C detaches, and wait or start resumes the durable job. GH_TOKEN or GITHUB_TOKEN supplies GitHub API authentication. Git uses its configured credentials.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			services, err := app.Build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			request, err := services.Workflow.BindPublication(cmd.Context(), workflow.PublicationRequest{ID: record.RequestID("request_" + rand.Text()), Branch: branch, Options: options})
			if err != nil {
				return err
			}
			spec := request.Spec.Publication
			if dryRun {
				if r.json {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(request.Spec)
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "Publish %s at %s\n  %s:%s -> %s:%s\n  title: %s\n  verification: %s (%s %s %s; tests %s; from source %t)\n", plain(spec.HeadBranch), spec.Desired.Head, plain(spec.HeadRepository), plain(spec.HeadBranch), plain(spec.Repository), plain(spec.BaseBranch), plain(spec.Desired.Title), spec.EvidenceAttempt, request.Spec.Build.Platform.OS, request.Spec.Build.Platform.Version, request.Spec.Build.Platform.Architecture, request.Spec.Build.Tests, request.Spec.Build.FromSource)
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
	cmd.Flags().StringVar(&branch, "branch", "", "Publish committed contents of this tracked local branch")
	cmd.Flags().StringVar(&options.Remote, "remote", "origin", "Git remote whose push URL receives the contribution")
	cmd.Flags().StringVar(&options.Upstream, "upstream", "", "Upstream Git remote (defaults to upstream, then the fork parent)")
	cmd.Flags().StringVar(&options.Base, "base", "", "PR base branch (defaults to the upstream default branch)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the publication plan without accepting a job or writing remotely")
	cmd.Flags().BoolVar(&wait, "wait", false, "Stay until the branch and PR are confirmed")
	return cmd
}
