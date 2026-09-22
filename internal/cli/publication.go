package cli

import (
	"crypto/rand"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/progress"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) publishCommand() *cobra.Command {
	var branch, adopt, change string
	var options publish.Options
	var dryRun, detach, unverified bool
	cmd := &cobra.Command{Use: "publish [target]", Short: "Publish a verified, committed contribution to GitHub", Args: cobra.MaximumNArgs(1),
		Long: "Publish the unique open contribution for a target. Omit the target to use the current branch, or select --branch or --change; --adopt publishes a branch dockhand did not make. A verified branch named with --adopt becomes a tracked contribution when publication is accepted. Uses the latest terminal verification for the committed tree and selected port; it must have passed. Its recorded build configuration is preserved. --skip-verify (-V) publishes a tracked contribution without any verification and discloses in the PR body that no local build ran. The contribution must contain one commit in one verified port directory, with shared files under _resources allowed beside it. Existing PR bodies are preserved. The command stays through confirmation of the pushed head and PR metadata; --detach returns after driver pickup. Ctrl-C detaches, and wait or serve resumes the durable job. GitHub API authentication comes from GH_TOKEN or GITHUB_TOKEN when set, otherwise from the credential auth login saved, otherwise from an authenticated GitHub CLI. Git uses its configured credentials for the push.",
		RunE: func(cmd *cobra.Command, args []string) error {
			services, err := r.build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			var target string
			if len(args) == 1 {
				if args[0] == "" {
					return fmt.Errorf("target must not be empty")
				}
				if target, err = portName(args[0]); err != nil {
					return err
				}
			}
			source := branch
			if adopt != "" {
				source = adopt
			}
			input := workflow.PublicationRequest{Target: target, ChangeID: record.ChangeID(change), ID: record.RequestID("request_" + rand.Text()), Branch: source, Adopt: adopt != "", Options: options, SkipVerify: unverified}
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
					return r.emit(request.Spec)
				}
				verification := "skipped at the author's request; the PR body discloses that no local build ran"
				if build := request.Spec.Build; build != nil {
					verification = fmt.Sprintf("%s (%s; tests %s; from source %t)", spec.EvidenceAttempt, macos.Describe(build.Platform), build.Tests, build.FromSource)
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "Publish %s at %s\n  %s:%s -> %s:%s\n  title: %s\n  target: %s\n  verification: %s\n", plain(spec.HeadBranch), spec.Desired.Head, plain(spec.HeadRepository), plain(spec.HeadBranch), plain(spec.Repository), plain(spec.BaseBranch), plain(spec.Desired.Title), plain(targetLabel(request.Spec.Targets[0])), verification)
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
			cited := string(spec.EvidenceAttempt)
			if spec.Unverified {
				cited = "skipped"
			}
			progress.VerboseReport(cmd.Context(), "Accepted publication job %s; branch %s at %s; verification %s", receipt.JobID, plain(spec.HeadBranch), spec.Desired.Head, cited)
			milestone := workflow.Completion
			if detach {
				milestone = workflow.Admission
			}
			return r.attach(cmd, services, receipt.JobID, milestone, false, false, &receipt)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "Select a tracked contribution branch (default: the current branch)")
	cmd.Flags().StringVar(&adopt, "adopt", "", "Publish the committed contents of a branch dockhand did not make; it becomes a tracked contribution")
	cmd.Flags().StringVar(&change, "change", "", "Select one tracked contribution when the target is ambiguous")
	cmd.MarkFlagsMutuallyExclusive("adopt", "branch")
	cmd.MarkFlagsMutuallyExclusive("adopt", "change")
	publicationFlags(cmd, &options)
	cmd.Flags().BoolVar(&unverified, "unverified", false, "Publish without verification; the PR body discloses that no local build ran")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the publication plan without accepting a job or writing remotely")
	cmd.Flags().BoolVar(&detach, "detach", false, "Return after driver pickup; wait or serve finishes it")
	section(cmd.Flags(), sectionSelection, "branch", "adopt", "change")
	section(cmd.Flags(), sectionRun, "unverified", "dry-run", "detach")
	return cmd
}

func publicationFlags(cmd *cobra.Command, options *publish.Options) {
	cmd.Flags().StringVar(&options.Remote, "remote", "", "Git remote whose push URL receives the contribution (default: the remote pushing to your fork)")
	cmd.Flags().StringVar(&options.Upstream, "upstream", "", "Upstream Git remote (default: the remote naming macports/macports-ports, then upstream, then the fork parent)")
	cmd.Flags().StringVar(&options.Base, "base", "", "PR base branch (defaults to the upstream default branch)")
	cmd.Flags().BoolVar(&options.RefreshBody, "update-body", false, "Rewrite an existing PR's environment section from this verification; its description and review checklist are left alone")
	section(cmd.Flags(), sectionGitHub, "remote", "upstream", "base", "update-body")
}
