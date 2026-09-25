package command

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func statusCommand(s *settings, streams Streams) *cobra.Command {
	var attentionOnly, all, refresh bool
	var port string
	cmd := &cobra.Command{
		Use:   "status [branch]",
		Short: "Show your branches, and what needs you",
		Long: `Shows each open branch: its ports, its work, its checks, and its pull
request, each in a column of its own, under a list of what needs you, each
row ending with the command that moves it forward. Inside a branch's
worktree, or naming one, it shows that branch in detail.

--attention prints only what needs you, for a prompt or a script, and exits
3 when anything does. --port finds every branch touching a port. --all
includes merged and archived branches. --refresh reads your pull requests
from GitHub first; serve does that every few minutes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if refresh {
				refreshPullRequests(cmd.Context(), e, streams.Err)
			}
			return showStatus(cmd.Context(), e, streams, args, attentionOnly, all, port)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "read your pull requests' state, reviews, and CI from GitHub first")
	cmd.Flags().BoolVar(&attentionOnly, "attention", false, "print only what needs you; exit 3 when anything does")
	cmd.Flags().BoolVar(&all, "all", false, "include merged, closed, and archived branches")
	cmd.Flags().StringVar(&port, "port", "", "only the branches touching this port")
	return cmd
}

func showStatus(ctx context.Context, e *engine.Engine, streams Streams, args []string, attentionOnly, all bool, port string) error {
	one := func(branch model.Branch) error {
		if streams.json() {
			status, err := e.BranchStatus(ctx, branch)
			if err != nil {
				return err
			}
			view := branchView(status)
			streams.emit(statusJSON{Attention: attentionView(attentionFor(status)), Branches: []branchJSON{view}})
		}
		return showBranch(ctx, e, streams.Out, branch)
	}
	if len(args) == 1 {
		branch, err := e.Resolve(ctx, args[0])
		if err != nil {
			return err
		}
		return one(branch)
	}
	if !attentionOnly && !all && port == "" {
		if branch, err := e.Current(ctx); err == nil {
			return one(branch)
		}
	}
	states := []model.BranchState{model.BranchOpen}
	if all {
		states = []model.BranchState{model.BranchOpen, model.BranchMerged, model.BranchClosed, model.BranchArchived}
	}
	statuses, err := e.Status(ctx, states...)
	if err != nil {
		return err
	}
	if port != "" {
		statuses = slices.DeleteFunc(statuses, func(s engine.BranchStatus) bool { return !slices.Contains(s.Scope.PortNames(), port) })
	}
	var rows []attention
	for _, status := range statuses {
		rows = append(rows, attentionFor(status)...)
	}
	out := streams.Out
	if streams.json() {
		result := statusJSON{Attention: attentionView(rows)}
		if !attentionOnly {
			result.Branches = []branchJSON{}
			for _, status := range statuses {
				result.Branches = append(result.Branches, branchView(status))
			}
			result.Serve = serveLine(ctx, e)
		}
		streams.emit(result)
	}
	if attentionOnly {
		writeAttention(out, rows)
		if len(rows) > 0 {
			return &ExitError{Code: 3}
		}
		return nil
	}
	if len(rows) > 0 {
		fmt.Fprintln(out, "Needs you")
		writeAttention(out, rows)
		fmt.Fprintln(out)
	}
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No open branches. Start one with dockhand start <name>.")
	} else {
		table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "BRANCH\tPORTS\tWORK\tCHECKS\tPR")
		for _, status := range statuses {
			fmt.Fprintf(table, "%s\t%d\t%s\t%s\t%s\n", status.Branch.ShortName(), len(status.Scope.Ports), workWords(status), checkState(status), prWords(status))
		}
		table.Flush()
	}
	if found, ok := readOutdated(e); ok && len(found.Outdated) > 0 {
		fmt.Fprintf(out, "\nYour ports: %s newer releases, as serve found %s (dockhand update --outdated --mine)\n", plural(len(found.Outdated), "port")+map[bool]string{true: " has", false: " have"}[len(found.Outdated) == 1], ago(found.CheckedAt))
	}
	fmt.Fprintf(out, "\n%s\n", serveLine(ctx, e))
	return nil
}

// attention is one row of what needs you, with the command that moves it.
type attention struct{ mark, branch, what, next string }

func attentionFor(s engine.BranchStatus) []attention {
	name := s.Branch.ShortName()
	row := func(mark, what, next string) []attention {
		return []attention{{mark: mark, branch: name, what: what, next: next}}
	}
	if s.Missing {
		return row("!", "its Git branch is gone", "dockhand adopt <new name>, if you renamed it")
	}
	if rows := pullRequestAttention(s); len(rows) > 0 {
		return rows
	}
	if pr := s.Branch.PullRequest; pr != nil && s.Branch.Origin == model.OriginServe && (pr.Observed == nil || pr.Observed.State == "open") {
		return row("·", fmt.Sprintf("#%d opened by serve, without a person's review", pr.Number), "dockhand status "+name)
	}
	if s.Latest == nil || !s.Current || len(s.Active) > 0 {
		if s.Latest != nil && !s.Current && s.Latest.State == model.RunPassed && len(s.Active) == 0 {
			return row("!", engine.Describe(*s.LatestRevision)+" passed; the files have changed since", "dockhand check --branch "+name)
		}
		return nil
	}
	run := *s.Latest
	switch run.State {
	case model.RunFailed:
		for _, target := range s.Evidence.Failed() {
			for i, result := range target.Outcomes {
				if result.Outcome == model.OutcomeFailed {
					return row("✗", fmt.Sprintf("%s failed at %s on %s (%s)", target.Target.Target.Name, result.Phase, environmentWords(s.Evidence.Plan.Environments[i]), run.Name()),
						fmt.Sprintf("dockhand logs %s --port %s", run.Name(), target.Target.Target.Name))
				}
			}
		}
		return row("✗", run.Name()+" failed: "+run.Detail, "dockhand logs "+run.Name())
	case model.RunAttention:
		return row("!", run.Name()+" needs attention: "+run.Detail, "dockhand logs "+run.Name())
	case model.RunPassed:
		switch {
		case len(s.Edited) > 0:
			return row("·", engine.Describe(*s.LatestRevision)+" passed; commit it for review", "dockhand tidy --branch "+name)
		case s.Branch.PullRequest == nil && len(s.Held) > 0:
			return row("!", "passed; held for a look: "+s.Held[0], "dockhand submit --branch "+name)
		case s.Branch.PullRequest == nil:
			return row("·", "passed; waiting for you to submit", "dockhand submit --branch "+name)
		case !s.Pushed():
			return row("·", fmt.Sprintf("passed; #%d does not have it yet", s.Branch.PullRequest.Number), "dockhand submit --branch "+name)
		}
	}
	return nil
}

// pullRequestAttention is what the forge's last reading asks of you:
// someone else's push, requested changes, or failing CI.
func pullRequestAttention(s engine.BranchStatus) []attention {
	pr := s.Branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.State != "open" {
		return nil
	}
	name, age := s.Branch.ShortName(), " ("+ago(pr.Observed.At)+")"
	next := "dockhand status " + name
	if ports := s.Scope.PortNames(); len(ports) > 0 {
		next = "dockhand edit " + ports[0]
	}
	switch {
	case s.SomeoneElsePushed():
		return []attention{{mark: "!", branch: name, what: fmt.Sprintf("someone else pushed to #%d%s", pr.Number, age), next: "dockhand submit --branch " + name + " (it shows the comparison)"}}
	case pr.Observed.Review == "changes-requested":
		return []attention{{mark: "!", branch: name, what: fmt.Sprintf("#%d changes requested%s", pr.Number, age), next: next}}
	case pr.Observed.Checks == "failing":
		return []attention{{mark: "✗", branch: name, what: fmt.Sprintf("#%d MacPorts CI failing: %s%s", pr.Number, strings.Join(s.Failing(), ", "), age),
			next: fmt.Sprintf("open https://github.com/%s/pull/%d/checks", pr.Repository, pr.Number)}}
	}
	return nil
}

// ago says how old an observation is, roughly.
func ago(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
}

func writeAttention(out io.Writer, rows []attention) {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(table, "  %s %s\t%s\t%s\n", row.mark, row.branch, row.what, row.next)
	}
	table.Flush()
}

func workWords(s engine.BranchStatus) string {
	switch {
	case s.Missing:
		return "branch gone"
	case s.Commits == 0 && len(s.Edited) == 0:
		return "nothing yet"
	case s.Commits == 0:
		return "edits, uncommitted"
	case len(s.Edited) > 0:
		return plural(s.Commits, "commit") + " + edits"
	}
	return plural(s.Commits, "commit")
}

// checkState is the checks column: what is running, else what the latest
// check says about the files as they are now.
func checkState(s engine.BranchStatus) string {
	if len(s.Active) > 0 {
		run := s.Active[len(s.Active)-1]
		return fmt.Sprintf("%s (%s)", run.State, run.Name())
	}
	if s.Latest == nil {
		return "—"
	}
	suffix := ""
	if !s.Current {
		suffix = ", for older work"
	}
	switch s.Latest.State {
	case model.RunPassed:
		if s.Current && s.LatestRevision.Kind == model.RevisionCommit {
			return "passed for this commit"
		}
		if s.Current {
			return "passed for " + engine.Describe(*s.LatestRevision)
		}
		return "passed for older work"
	case model.RunFailed:
		failed := len(s.Evidence.Failed())
		return fmt.Sprintf("%d failed, %d passed%s", failed, len(s.Evidence.Targets)-failed, suffix)
	}
	return "needs attention" + suffix
}

func prWords(s engine.BranchStatus) string {
	pr := s.Branch.PullRequest
	if pr == nil {
		return "—"
	}
	words := fmt.Sprintf("#%d", pr.Number)
	if pr.Draft {
		words += " draft"
	}
	if observed := pr.Observed; observed != nil {
		switch {
		case observed.State != "open":
			words += " " + observed.State
		case observed.Review == "changes-requested":
			words += " changes requested"
		case observed.Review == "approved":
			words += " approved"
		}
		switch observed.Checks {
		case "passing":
			words += ", CI ✓"
		case "failing":
			words += ", CI ✗"
		case "pending":
			words += ", CI …"
		}
	}
	if !s.Pushed() {
		words += ", not pushed"
	}
	return words
}

// serveLine says whether serve runs, and what the queue holds.
func serveLine(ctx context.Context, e *engine.Engine) string {
	queued, _ := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	line := "serve: not running"
	session, err := observe(ctx, e)
	if err == nil {
		defer session.End(context.WithoutCancel(ctx))
		if leader, err := session.Holder(ctx, coord.LeaderResource); err == nil && leader != nil {
			line = fmt.Sprintf("serve: running (pid %d)", leader.PID)
			if leading, ok := readServing(e); ok && leading.PID == leader.PID && leading.SubmitPassing {
				line += " · opens PRs for passing updates"
			}
		}
	}
	switch len(queued) {
	case 0:
		return line + " · queue: empty"
	}
	return line + " · queue: " + plural(len(queued), "run")
}

// observe starts an observer session: one that only reads, and judges who
// is alive.
func observe(ctx context.Context, e *engine.Engine) (*coord.Session, error) {
	return startSession(ctx, e, model.SessionObserver)
}

func showBranch(ctx context.Context, e *engine.Engine, out io.Writer, branch model.Branch) error {
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return err
	}
	head := branch.ShortName()
	if branch.Worktree != "" {
		head += " · " + tilde(branch.Worktree)
	}
	fmt.Fprintln(out, head)
	ports := strings.Join(status.Scope.PortNames(), ", ")
	if status.Scope.Resources {
		ports = strings.TrimPrefix(ports+", _resources", ", ")
	}
	if ports == "" {
		ports = "none yet"
	}
	fmt.Fprintf(out, "  Ports    %s\n", ports)
	fmt.Fprintf(out, "  Work     %s above master %s\n", workWords(status), engine.Short(branch.Base))
	if len(status.Edited) > 0 {
		fmt.Fprintf(out, "  Edited   %s\n", strings.Join(status.Edited, ", "))
	}
	fmt.Fprintf(out, "  Checks   %s\n", checkState(status))
	if status.Evidence != nil {
		for _, target := range status.Evidence.Targets {
			var cells []string
			for i, result := range target.Outcomes {
				environment := status.Evidence.Plan.Environments[i]
				cells = append(cells, environmentWords(environment)+" "+resultWords(status.Evidence.Plan, target.Target, environment, result))
			}
			fmt.Fprintf(out, "           %s  %s\n", target.Target.Target.Name, strings.Join(cells, "   "))
		}
	}
	fmt.Fprintf(out, "  PR       %s\n", prWords(status))
	for _, row := range attentionFor(status) {
		fmt.Fprintf(out, "Next: %s\n", row.next)
	}
	return nil
}

// refreshPullRequests reads the pull requests and says what changed, and
// what could not be read.
func refreshPullRequests(ctx context.Context, e *engine.Engine, out io.Writer) {
	refreshed, err := e.RefreshPullRequests(ctx)
	if err != nil {
		fmt.Fprintf(out, "Could not read pull requests: %v\n", err)
		return
	}
	for _, r := range refreshed {
		if r.Err != nil {
			fmt.Fprintf(out, "%s: could not read #%d: %v\n", r.Branch.ShortName(), r.Branch.PullRequest.Number, r.Err)
		}
		for _, change := range r.Changes {
			fmt.Fprintf(out, "%s: %s\n", r.Branch.ShortName(), change)
		}
	}
}
