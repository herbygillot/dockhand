package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) statusCommand() *cobra.Command {
	var filter workflow.StatusFilter
	cmd := &cobra.Command{
		Use:   "status [target]",
		Short: "Show recorded workflow status",
		Long:  "Show a repository state snapshot, including recorded jobs, verification, publication, and resource cleanup. This command does not advance work or refresh provider or pull request state.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if args[0] == "" {
					return fmt.Errorf("status requires a nonempty target")
				}
				filter.Target = args[0]
			}
			if cmd.Flags().Changed("branch") && filter.Branch == "" {
				return fmt.Errorf("--branch requires a nonempty branch name")
			}
			status, err := app.FilteredStatus(cmd.Context(), r.config, filter)
			if err != nil {
				return databaseReadError(err)
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
			}
			return renderStatus(cmd.OutOrStdout(), status)
		},
	}
	cmd.Flags().StringVar((*string)(&filter.JobID), "job", "", "Inspect one job instead of a target")
	cmd.Flags().StringVar((*string)(&filter.ChangeID), "change", "", "Inspect one contribution")
	cmd.Flags().BoolVar(&filter.Active, "active", false, "Show queued and active jobs, including capacity and retry waits")
	cmd.Flags().StringVar(&filter.Branch, "branch", "", "Show jobs for a recorded contribution branch")
	return cmd
}

func renderStatus(out io.Writer, status workflow.Status) error {
	var buffer bytes.Buffer
	line := func(format string, values ...any) {
		for i, value := range values {
			quoted := strconv.Quote(fmt.Sprint(value))
			values[i] = quoted[1 : len(quoted)-1]
		}
		fmt.Fprintf(&buffer, format+"\n", values...)
	}
	environment := func(indent string, evidence *record.Evidence) {
		if evidence != nil && evidence.Workflow != nil {
			flow := evidence.Workflow
			outcome := flow.Status
			if flow.Conclusion != "" {
				outcome = flow.Conclusion
			}
			line("%sGitHub Actions: %s; run %s attempt %s; %s", indent, outcome, flow.RunID, flow.RunAttempt, flow.URL)
			line("%s  Fork branch: %s:%s at %s", indent, flow.Repository, flow.Branch, flow.Commit)
			for _, job := range flow.Jobs {
				line("%s  %s: %s %s; %s", indent, job.Name, job.Status, job.Conclusion, job.URL)
			}
			line("%sTest policy: workflow; individual port test success is not established", indent)
		}
		if evidence == nil || evidence.Environment == nil {
			return
		}
		observed := evidence.Environment
		tools := string(observed.Capabilities.DeveloperTools)
		if observed.Capabilities.XcodeVersion != "" {
			tools += " " + observed.Capabilities.XcodeVersion
		}
		line("%senvironment: %s %s; capabilities: %s", indent, observed.Provider, observed.EnvironmentDigest, observed.CapabilityDigest)
		line("%sMacPorts: %s at %s; developer tools: %s", indent, observed.Capabilities.MacPortsVersion, observed.Capabilities.MacPortsPrefix, tools)
	}
	line("Snapshot read at %s", statusTime(status.ReadAt))
	if status.Repository != "" {
		line("Repository: %s", status.Repository)
	}
	if f := status.Filter; f != nil {
		if f.Target != "" {
			line("Target: %s", f.Target)
		}
		if f.ChangeID != "" {
			line("Contribution: %s", f.ChangeID)
		}
		if f.JobID != "" {
			line("Job: %s", f.JobID)
		}
		if f.Branch != "" {
			line("Contribution branch: %s", f.Branch)
		}
		if f.Active {
			line("Showing queued and active jobs.")
		}
	}
	if len(status.Jobs) == 0 {
		if status.Filter != nil {
			line("No matching jobs.")
		} else {
			line("No recorded jobs.")
		}
	}

	for _, entry := range status.Jobs {
		job := entry.Job
		line("\n%s  %s  %s -> %s", job.ID, job.State, job.Spec.Action, job.Spec.Destination)
		if outcome := completedOutcome(entry); outcome != "" {
			line("  outcome: %s", outcome)
		}
		line("  request: %s; accepted: %s", job.RequestID, statusTime(job.AcceptedAt))
		line("  phase: %s", job.Phase)
		line("  verification policy: %s", job.Spec.Verification)
		if d := job.Spec.PublishTo; d != nil {
			line("  publication destination: %s -> %s:%s", d.HeadRepository, d.Repository, d.BaseBranch)
		}
		for _, target := range job.Spec.Targets {
			line("  target: %s", targetLabel(target))
			if version := job.Spec.EvaluatedVersions[target.Name]; version != "" {
				line("    input version: %s", version)
			}
		}
		if entry.Plan != nil {
			for _, target := range entry.Plan.Targets {
				line("  coverage target: %s", targetLabel(target.Port))
				for _, reason := range target.Reasons {
					line("    selected by: %s", reason)
				}
				if target.Problem != "" {
					line("    blocked: %s", target.Problem)
				}
				for _, problem := range target.CoverageProblems {
					line("    coverage gap: %s", problem)
				}
			}
		}
		if job.Spec.InputRevision != "" {
			line("  input revision: %s", job.Spec.InputRevision)
		}
		line("  source tree: %s", job.Spec.Source.Tree)
		if job.ReuseDetail != "" {
			line("  verification reuse: %s", job.ReuseDetail)
		}
		if entry.Reused != nil {
			line("  original attempt: %s; job: %s", entry.Reused.ID, entry.Reused.JobID)
			environment("  ", entry.Reused.Evidence)
		}
		if c := job.Spec.Checkout; c != nil {
			label := c.Branch
			if label == "" {
				label = "detached HEAD"
			}
			line("  input: working tree (%s); HEAD %s; %s modified files", label, c.Head, c.ModifiedFiles)
		}
		if release := job.ResolvedRelease; release != nil {
			if release.Archive {
				if release.Listing != nil {
					line("  release: archive version %s; discovered at %s", release.Version, release.Listing.URL)
				} else {
					line("  release: explicit archive version %s", release.Version)
				}
			} else {
				line("  release: %s %s; commit: %s", release.Repository, release.Tag, release.Commit)
			}
		}
		if job.Spec.Checkout == nil && job.Spec.SourceBranch != "" {
			line("  input: committed branch %s; commit %s", job.Spec.SourceBranch, job.Spec.Source.Commit)
		}
		if job.Prepared != nil {
			if job.ResultRevision != "" {
				line("  prepared branch: %s; commit: %s", job.Prepared.Branch, job.Prepared.Source.Commit)
			} else {
				line("  candidate awaiting confirmed branch integration: %s; commit: %s", job.Prepared.Branch, job.Prepared.Source.Commit)
			}
		}
		if job.Spec.Action == record.Verify && job.ChangeID == "" {
			line("  scope: standalone verification; no update contribution was prepared by this job")
		}
		if job.Phase == record.PhasePreparation && job.ResultRevision == "" && job.Prepared == nil {
			line("  update branch: none created")
		}
		if job.ResultRevision != "" {
			line("  result revision: %s", job.ResultRevision)
		}
		if job.CancelRequestedAt != nil {
			line("  cancellation requested: %s", statusTime(*job.CancelRequestedAt))
		}
		if job.AdmittedAt != nil {
			line("  admitted: %s", statusTime(*job.AdmittedAt))
		}
		if job.FinishedAt != nil {
			line("  finished: %s", statusTime(*job.FinishedAt))
		}
		if job.Detail != "" {
			line("  detail: %s", job.Detail)
		}
		if job.ConsecutiveWaits > 1 && job.RetryAt != nil {
			line("  waiting: %d consecutive waits; next look %s", job.ConsecutiveWaits, statusTime(*job.RetryAt))
		}
		if len(entry.Attempts) == 0 {
			line("  verification: no recorded attempts")
		}
		for _, attempt := range entry.Attempts {
			platformLabel := "platform"
			if attempt.Spec.Config.Tests == record.TestWorkflow {
				platformLabel = "evaluation platform"
			}
			line("  attempt %s: %s; target: %s; %s: %s %s %s", attempt.ID, attempt.State, targetLabel(attempt.Spec.Target), platformLabel, attempt.Spec.Config.Platform.OS, attempt.Spec.Config.Platform.Version, attempt.Spec.Config.Platform.Architecture)
			if attempt.LastError != "" {
				line("    detail: %s", attempt.LastError)
			}
			if attempt.ConsecutiveWaits > 1 && attempt.RetryAt != nil {
				line("    waiting: %d consecutive waits; next look %s", attempt.ConsecutiveWaits, statusTime(*attempt.RetryAt))
			}
			if attempt.Evidence != nil {
				line("    verdict: %s; observed: %s", attempt.Evidence.Verdict, statusTime(attempt.Evidence.ObservedAt))
				environment("    ", attempt.Evidence)
				if failure := attempt.Evidence.Failure; failure != nil {
					line("    failure: %s; package: %s; phase: %s; %s", failure.Kind, failure.Package, failure.Phase, failure.Detail)
				}
			}
		}
		if len(entry.Publications) == 0 {
			line("  publication: no recorded actions")
		}
		for _, publication := range entry.Publications {
			line("  publication %s: %s; revision: %s", publication.ID, publication.State, publication.RevisionID)
			if publication.ConfirmedAt != nil {
				line("    confirmed: %s", statusTime(*publication.ConfirmedAt))
			}
			if publication.LastError != "" {
				line("    error: %s", publication.LastError)
			}
		}
	}
	for _, change := range status.Changes {
		line("\nChange %s: %s; branch: %s; current revision: %s; published revision: %s", change.ID, change.Disposition, change.Branch, change.CurrentRevision, change.PublishedRevision)
	}
	for _, revision := range status.Revisions {
		if revision.Scope != nil {
			line("\nShared release %s:", revision.ID)
			for _, member := range revision.Scope.Affected {
				kind := "verification required"
				if member.MetadataOnly {
					kind = "metadata only"
				}
				line("  %s: %s -> %s; %s", member.Target.Name, member.Before.Version, member.After.Version, kind)
			}
			for _, member := range revision.Scope.Protected {
				line("  protected: %s %s", member.Target.Name, member.After.Version)
			}
		}
	}
	for _, pr := range status.PullRequests {
		line("\nPull request %s: %s; change: %s; %s", pr.ID, pr.State, pr.ChangeID, pr.Ref.URL)
		line("  observed: %s", statusTime(pr.ObservedAt))
	}
	if len(status.Resources) != 0 {
		line("\nResources:")
	}
	for _, resource := range status.Resources {
		line("  %s: %s; attempt: %s; provider: %s", resource.ID, resource.State, resource.AttemptID, resource.Handle.Provider)
		if resource.RetainUntil != nil {
			line("    retain until: %s", statusTime(*resource.RetainUntil))
		}
		if resource.ReleasedAt != nil {
			line("    released: %s", statusTime(*resource.ReleasedAt))
		}
		if resource.ArtifactsPrunedAt != nil {
			line("    diagnostic files pruned: %s", statusTime(*resource.ArtifactsPrunedAt))
		}
		if resource.LastError != "" {
			line("    error: %s", resource.LastError)
		}
	}
	_, err := io.Copy(out, &buffer)
	return err
}

func targetLabel(target record.Target) string {
	name := target.Name
	if target.Subport != "" && target.Subport != target.Name {
		name += "/" + target.Subport
	}
	var variants []string
	for _, name := range slices.Sorted(maps.Keys(target.Variants)) {
		prefix := "-"
		if target.Variants[name] {
			prefix = "+"
		}
		variants = append(variants, prefix+name)
	}
	return strings.TrimSpace(name + " (" + target.Portfile + ") " + strings.Join(variants, " "))
}

func statusTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
