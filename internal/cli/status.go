package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tui"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) statusCommand() *cobra.Command {
	var filter workflow.StatusFilter
	var all bool
	cmd := &cobra.Command{
		Use:   "status [target]",
		Short: "Show recorded workflow status",
		Long:  "Show the repository's contributions as one row each: port, change, phase, state, and what comes next, printed once; it changes nothing. console opens the live table that processes work. -v prints the full record with identifiers, and --json the same as data.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if args[0] == "" {
					return fmt.Errorf("status requires a nonempty target")
				}
				target, err := portName(args[0])
				if err != nil {
					return err
				}
				filter.Target = target
			}
			if cmd.Flags().Changed("branch") && filter.Branch == "" {
				return fmt.Errorf("--branch requires a nonempty branch name")
			}
			status, err := app.FilteredStatus(cmd.Context(), r.config, filter)
			if err != nil {
				return databaseReadError(err)
			}
			overview := workflow.Overview{Status: status, Contributions: view.Project(status.Snapshot)}
			if !all {
				overview.Contributions = view.Current(overview.Contributions)
			}
			if r.json {
				return r.emit(overview)
			}
			if r.level(cmd) >= progress.Verbose {
				return renderStatus(cmd.OutOrStdout(), status)
			}
			return renderContributions(cmd.OutOrStdout(), overview, len(view.Project(status.Snapshot))-len(overview.Contributions))
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Include retired contributions: merged, closed, and abandoned")
	cmd.Flags().StringVar((*string)(&filter.JobID), "job", "", "Inspect one job instead of a target")
	cmd.Flags().StringVar((*string)(&filter.ChangeID), "change", "", "Inspect one contribution")
	cmd.Flags().BoolVar(&filter.Active, "active", false, "Show queued and active jobs, including capacity and retry waits")
	cmd.Flags().StringVar(&filter.Branch, "branch", "", "Show jobs for a recorded contribution branch")
	return cmd
}

// liveStatus renders the contribution table with Bubble Tea and processes
// the repository's work while it is open: the driver loop runs beside the
// table with its reports in the message strip, the snapshot is reread as
// work advances, and the keys run the verbs in-process on this runtime's
// configuration, so a key has exactly the authority of the command.
func (r *runtime) consoleCommand() *cobra.Command {
	var filter workflow.StatusFilter
	var all, watch bool
	cmd := &cobra.Command{
		Use:   "console [target]",
		Short: "Open the live table that processes this repository's work",
		Long:  "Open the live contribution table. While it is open it processes the repository's pending work, rereads the snapshot as work advances, and its keys run the verbs on the selected row, each with exactly the authority of the command it names. --watch opens the table without processing, for when serve is doing the driving. It needs a terminal; status prints the same rows once.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if args[0] == "" {
					return fmt.Errorf("console requires a nonempty target")
				}
				target, err := portName(args[0])
				if err != nil {
					return err
				}
				filter.Target = target
			}
			if !isTerminal(cmd.OutOrStdout()) || !isTerminal(cmd.InOrStdin()) {
				return fmt.Errorf("console needs a terminal; status prints the table once")
			}
			return r.liveStatus(cmd, filter, all, !watch)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Include retired contributions: merged, closed, and abandoned")
	cmd.Flags().BoolVar(&watch, "watch", false, "Open the table without processing work")
	return cmd
}

// liveStatus is the console: the contribution table with Bubble Tea, and,
// when it drives, the repository's work processed while it is open.
func (r *runtime) liveStatus(cmd *cobra.Command, filter workflow.StatusFilter, showRetired, drive bool) error {

	services, err := r.build(cmd.Context(), r.config)
	if err != nil {
		return err
	}
	defer services.Close()
	level := r.level(cmd)
	options := tui.Options{
		ShowRetired: showRetired,
		Verbs:       tableVerbs(),
		Poll: func(ctx context.Context) (workflow.Overview, error) {
			status, err := services.Workflow.FilteredStatus(ctx, filter)
			if err != nil {
				return workflow.Overview{}, err
			}
			return workflow.Overview{Status: status, Contributions: view.Project(status.Snapshot)}, nil
		},
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			return run(ctx, args, Streams{Out: out, Err: out}, r.config, r.build)
		},
		Open: func(target string) error { return exec.Command("open", target).Start() },
	}
	if drive {
		options.Processor = func(ctx context.Context, say func(scope, text string)) error {
			ctx = progressContext(ctx, &lineWriter{say: func(text string) { say("", text) }}, level, false)
			services.Processes.OnCycle = func(result workflow.CycleResult) error {
				for _, problem := range result.Problems {
					say(string(problem.JobID), problem.Detail)
				}
				return nil
			}
			return services.Processes.Run(ctx, services.Workflow, workflow.Scope{All: true})
		}
	}
	return tui.Run(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), options)
}

// lineWriter hands each complete line written to it to say.
type lineWriter struct {
	say func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.say(line)
		}
		w.buf = w.buf[i+1:]
	}
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// renderContributions prints the info-level status: one row per port with
// the words of the projection, and nothing a person has to decode. hidden
// counts the retired rows left out without --all.
func renderContributions(out io.Writer, overview workflow.Overview, hidden int) error {
	if len(overview.Contributions) == 0 {
		message := "No recorded jobs."
		switch {
		case hidden > 0:
			message = fmt.Sprintf("No open contributions; %d retired (--all shows them).", hidden)
		case overview.Filter != nil:
			message = "No matching jobs."
		}
		_, err := fmt.Fprintln(out, message)
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "PORT\tCHANGE\tPHASE\tSTATE\tPR\tNEXT")
	for _, row := range overview.Contributions {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", plain(row.Port), plain(row.Change), plain(row.Phase), plain(row.State), plain(row.PullRequest), plain(row.Next))
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if hidden > 0 {
		_, err := fmt.Fprintf(out, "%d retired hidden (--all shows them).\n", hidden)
		return err
	}
	return nil
}

// renderStatus prints the full record behind -v: every job, attempt,
// publication, change, revision, pull request, and resource with its
// identifiers and times.
func renderStatus(out io.Writer, status workflow.Status) error {
	var buffer bytes.Buffer
	line := func(format string, values ...any) {
		for i, value := range values {
			quoted := strconv.Quote(fmt.Sprint(value))
			values[i] = quoted[1 : len(quoted)-1]
		}
		fmt.Fprintf(&buffer, format+"\n", values...)
	}
	environment := func(indent string, facts view.Evidence) {
		if flow := facts.Workflow; flow != nil {
			line("%sGitHub Actions: %s; run %s attempt %s; %s", indent, flow.Outcome, flow.RunID, flow.RunAttempt, flow.URL)
			line("%s  Fork branch: %s:%s at %s", indent, flow.Repository, flow.Branch, flow.Commit)
			for _, job := range flow.Jobs {
				line("%s  %s: %s %s; %s", indent, job.Name, job.Status, job.Conclusion, job.URL)
			}
			line("%sTest policy: workflow; individual port test success is not established", indent)
		}
		if observed := facts.Environment; observed != nil {
			line("%senvironment: %s %s; capabilities: %s", indent, observed.Provider, observed.Identity, observed.CapabilityIdentity)
			line("%sMacPorts: %s at %s; developer tools: %s", indent, observed.MacPortsVersion, observed.MacPortsPrefix, observed.DeveloperTools)
		}
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
			environment("  ", view.Facts(entry.Reused.Evidence))
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
				if release.LeavesStable {
					line("  stability: prerelease; this change takes the port out of stable")
				}
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
			line("  waiting: %s consecutive %s waits; next look %s", job.ConsecutiveWaits, job.WaitKind, statusTime(*job.RetryAt))
		}
		if len(entry.Attempts) == 0 {
			line("  verification: no recorded attempts")
		}
		for _, attempt := range entry.Attempts {
			platformLabel := "platform"
			if attempt.Spec.Config.Tests == record.TestWorkflow {
				platformLabel = "evaluation platform"
			}
			facts := view.Verification(attempt)
			line("  attempt %s: %s; target: %s; %s: %s", attempt.ID, attempt.State, targetLabel(attempt.Spec.Target), platformLabel, facts.Platform)
			if attempt.LastError != "" {
				line("    detail: %s", attempt.LastError)
			}
			if attempt.ConsecutiveWaits > 1 && attempt.RetryAt != nil {
				line("    waiting: %s consecutive %s waits; next look %s", attempt.ConsecutiveWaits, attempt.WaitKind, statusTime(*attempt.RetryAt))
			}
			if attempt.Evidence != nil {
				line("    verdict: %s; observed: %s", facts.Verdict, statusTime(facts.ObservedAt))
				environment("    ", facts)
				if failure := facts.Failure; failure != nil {
					line("    failure: %s; package: %s; phase: %s; %s", failure.Kind, failure.Package, failure.Phase, failure.Detail)
					for _, fetch := range failure.Fetches {
						line("      fetch: %s", fetch)
					}
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
		for _, file := range revision.Shared {
			line("\nShared file %s changed by revision %s; loaded by %d files, only the port was built", file.Path, revision.ID, len(file.Loaders))
			for _, loader := range file.Loaders {
				line("  %s", loader)
			}
		}
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
		if pr.Status != nil {
			line("  status: %s; inspected %s", pr.Status.Summary(), statusTime(pr.Status.ObservedAt))
		}
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
