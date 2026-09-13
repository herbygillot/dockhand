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

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show recorded workflow status",
		Long:  "Show a repository state snapshot, including recorded jobs, verification, publication, and resource cleanup. This command does not advance work or refresh provider or pull request state.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := app.Status(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
			}
			return renderStatus(cmd.OutOrStdout(), status)
		},
	}
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
	line("Snapshot read at %s", statusTime(status.ReadAt))
	if status.Repository != "" {
		line("Repository: %s", status.Repository)
	}
	if len(status.Jobs) == 0 {
		line("No recorded jobs.")
	}

	for _, entry := range status.Jobs {
		job := entry.Job
		line("\n%s  %s  %s -> %s", job.ID, job.State, job.Spec.Action, job.Spec.Destination)
		line("  request: %s; accepted: %s", job.RequestID, statusTime(job.AcceptedAt))
		line("  verification policy: %s", job.Spec.Verification)
		for _, target := range job.Spec.Targets {
			line("  target: %s", targetLabel(target))
		}
		if job.Spec.InputRevision != "" {
			line("  input revision: %s", job.Spec.InputRevision)
		}
		line("  source tree: %s", job.Spec.Source.Tree)
		if c := job.Spec.Checkout; c != nil {
			label := c.Branch
			if label == "" {
				label = "detached HEAD"
			}
			line("  input: working tree (%s); HEAD %s; %d modified files", label, c.Head, c.ModifiedFiles)
		}
		if release := job.ResolvedRelease; release != nil {
			line("  release: %s %s; commit: %s", release.Repository, release.Tag, release.Commit)
		}
		if job.Prepared != nil {
			line("  prepared branch: %s; commit: %s", job.Prepared.Branch, job.Prepared.Source.Commit)
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
		if len(entry.Attempts) == 0 {
			line("  verification: no recorded attempts")
		}
		for _, attempt := range entry.Attempts {
			line("  attempt %s: %s; target: %s; platform: %s %s %s", attempt.ID, attempt.State, targetLabel(attempt.Spec.Target), attempt.Spec.Config.Platform.OS, attempt.Spec.Config.Platform.Version, attempt.Spec.Config.Platform.Architecture)
			if attempt.LastError != "" {
				line("    detail: %s", attempt.LastError)
			}
			if attempt.Evidence != nil {
				line("    verdict: %s; observed: %s", attempt.Evidence.Verdict, statusTime(attempt.Evidence.ObservedAt))
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
		if resource.LastError != "" {
			line("    error: %s", resource.LastError)
		}
	}
	_, err := io.Copy(out, &buffer)
	return err
}

func targetLabel(target record.Target) string {
	name := target.Name
	if target.Subport != "" {
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
