package command

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
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
	ctx, _, end, err := observing(ctx, e)
	if err != nil {
		return err
	}
	defer end()
	one := func(branch model.Branch) error {
		if streams.json() {
			status, err := judgedStatus(ctx, e, branch)
			if err != nil {
				return err
			}
			view := branchView(status)
			streams.emit(statusJSON{Attention: attentionView(attentionFor(status)), Branches: []branchJSON{view}})
		}
		return showBranch(ctx, e, streams.Out, branch)
	}
	if len(args) == 1 {
		branch, err := e.ResolveRecord(ctx, args[0])
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
	if err := judgeStopped(ctx, e, statuses); err != nil {
		return err
	}
	if port != "" {
		statuses = slices.DeleteFunc(statuses, func(s engine.BranchStatus) bool {
			changed, _ := s.ChangedPorts()
			return !slices.Contains(s.Scope.PortNames(), port) && !slices.Contains(changed, port)
		})
	}
	var rows []attention
	for _, status := range statuses {
		rows = append(rows, attentionFor(status)...)
	}
	var serve engine.ServeState
	if !attentionOnly {
		if serve, err = readServe(ctx, e); err != nil {
			return err
		}
	}
	out := streams.Out
	if streams.json() {
		result := statusJSON{Attention: attentionView(rows)}
		if !attentionOnly {
			result.Branches = []branchJSON{}
			for _, status := range statuses {
				result.Branches = append(result.Branches, branchView(status))
			}
			view := serveView(serve)
			result.Serve, result.ServeState = serveWords(serve), &view
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
	// A branch renamed by hand is followed, and said the once.
	for _, status := range statuses {
		if status.Renamed != "" {
			fmt.Fprintf(out, "Followed %s, renamed by hand to %s.\n\n", status.Renamed, status.Branch.ShortName())
		}
	}
	if len(rows) > 0 {
		fmt.Fprintln(out, "Needs you")
		writeAttention(out, rows)
		fmt.Fprintln(out)
	}
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No open branches. dockhand update <port> starts one for a port, and dockhand start <name> one for other work.")
	} else {
		table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "BRANCH\tPORTS\tWORK\tCHECKS\tPR")
		for _, status := range statuses {
			ports := strconv.Itoa(len(status.Scope.Ports))
			if status.Cleaned() {
				// What its Git branch changed went with it.
				ports = "—"
			}
			// An archived branch is said, as clean says it; its row
			// read as an open one's (the dogfood run with bf711891).
			name := status.Branch.ShortName()
			if status.Branch.State == model.BranchArchived {
				name += " (archived)"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", name, ports, workWords(status), checkState(status), prWords(status))
		}
		table.Flush()
	}
	if found, ok := e.LastOutdatedLook(); ok && (len(found.Outdated) > 0 || len(found.Uncertain) > 0) {
		fmt.Fprintln(out)
		if len(found.Outdated) > 0 {
			fmt.Fprintf(out, "Your ports: %s newer releases, as serve found %s (dockhand update --outdated --mine)\n", prose.Plural(len(found.Outdated), "port")+map[bool]string{true: " has", false: " have"}[len(found.Outdated) == 1], ago(found.CheckedAt))
		}
		if len(found.Uncertain) > 0 {
			fmt.Fprintf(out, "Your ports: %s may have newer releases, for your look, as serve found %s (dockhand outdated %s)\n", prose.Plural(len(found.Uncertain), "port"), ago(found.CheckedAt), strings.Join(found.Uncertain, " "))
		}
	}
	fmt.Fprintf(out, "\n%s\n", serveWords(serve))
	return nil
}

// attention is one row of what needs you, with the command that moves it.
type attention struct{ mark, branch, what, next string }

// judgeStopped marks the statuses whose check stopped when the process
// running it ended, which only a session can judge.
func judgeStopped(ctx context.Context, e *engine.Engine, statuses []engine.BranchStatus) error {
	_, session, end, err := observing(ctx, e)
	if err != nil {
		return err
	}
	defer end()
	return engine.JudgeStopped(ctx, session, statuses)
}

// judgedStatus is one branch's status, with a stopped check marked.
func judgedStatus(ctx context.Context, e *engine.Engine, branch model.Branch) (engine.BranchStatus, error) {
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return status, err
	}
	statuses := []engine.BranchStatus{status}
	err = judgeStopped(ctx, e, statuses)
	return statuses[0], err
}

func attentionFor(s engine.BranchStatus) []attention {
	name := s.Branch.ShortName()
	row := func(mark, what, next string) []attention {
		return []attention{{mark: mark, branch: name, what: what, next: next}}
	}
	if s.Cleaned() {
		return nil
	}
	if s.Missing && s.Branch.PullRequest != nil {
		// Status never reads the network, so it can't know the pull
		// request merged: a branch cleaned by hand after its merge read as
		// trouble, adopt suggested, until --refresh (the sand-runner
		// session, batch 31).
		return row("!", fmt.Sprintf("its Git branch is gone, and its pull request, #%d, may have merged", s.Branch.PullRequest.Number), "dockhand status --refresh reads it; dockhand adopt <new name>, if you renamed the branch")
	}
	if s.Missing {
		return row("!", "its Git branch is gone", "dockhand adopt <new name>, if you renamed it")
	}
	if run := s.Stopped; run != nil {
		return row("!", run.Name()+" stopped: the process running it ended; dockhand cancel "+run.Name()+" ends it", "dockhand wait "+run.Name())
	}
	if rows := pullRequestAttention(s); len(rows) > 0 {
		return rows
	}
	if pr := s.Branch.PullRequest; pr != nil && s.Branch.Origin == model.OriginServe && (pr.Observed == nil || pr.Observed.State == "open") {
		return row("·", fmt.Sprintf("#%d opened by serve, without a person's review", pr.Number), "dockhand status "+name)
	}
	// A branch set aside asks nothing of its checks.
	if s.Branch.State == model.BranchArchived {
		return nil
	}
	// Work that landed on master by another route asks nothing more of
	// checks, only to be set aside (cleaning up duckdb-cxx14, finding 1).
	if s.OnMaster != "" && s.Branch.PullRequest == nil {
		return row("·", "its changes are on master already, as of "+engine.Short(s.OnMaster), "dockhand archive "+name)
	}
	if s.Latest == nil || !s.Current || len(s.Active) > 0 {
		if s.Latest != nil && !s.Current && s.Latest.State == model.RunPassed && len(s.Active) == 0 {
			return row("!", engine.Describe(*s.LatestRevision)+" passed; the files have changed since", "dockhand check --branch "+name)
		}
		return nil
	}
	run := *s.Latest
	if s.Evidence == nil {
		return nil
	}
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
		var unchecked, remade []string
		var why string
		for _, target := range s.Evidence.Missing() {
			switch {
			case len(target.Remade()) > 0:
				remade = append(remade, target.Target.Target.Name)
				why = engine.RemadeWords(target)
			default:
				unchecked = append(unchecked, target.Target.Target.Name)
			}
		}
		// A check that builds them again builds where this one did, which
		// check.on may not name: what still stands is reused, and the rest
		// built (the hugo exercise's re-checks after the guest fix).
		again := "dockhand check --branch " + name
		if on, ok := engine.OnValues(s.Evidence.Plan.Environments); ok && len(on) > 0 {
			again += " --on " + strings.Join(on, " --on ")
		}
		switch {
		case len(remade) > 0:
			return row("!", fmt.Sprintf("%s passed, but since then %s; %s must be built there again", run.Name(), why, strings.Join(remade, ", ")), again)
		case len(unchecked) > 0:
			return row("!", fmt.Sprintf("%s passed, but no check of these files built %s", run.Name(), strings.Join(unchecked, ", ")), again)
		case len(s.Edited) > 0:
			return row("·", engine.Describe(*s.LatestRevision)+" passed; commit it for review", "dockhand tidy --branch "+name)
		case s.Branch.PullRequest == nil && len(s.Held) > 0:
			return row("!", "passed; held for a look: "+s.Held[0], submitNext(s, name))
		case s.Branch.PullRequest == nil && len(s.Moved) > 0:
			// The check built another source than the update chose: serve
			// waits for a look, and a person's submission shows it.
			what := "passed; " + s.Moved[0].Detail
			if s.Branch.Origin == model.OriginServe {
				what = "passed; held for a look: " + s.Moved[0].Detail
			}
			return row("!", what, submitNext(s, name))
		case s.Branch.PullRequest == nil && s.Assessment == engine.AssessmentPending:
			// Status collects nothing; submitting assesses what isn't yet.
			return row("·", "passed; what upstream's change means isn't assessed yet, which submitting does", submitNext(s, name))
		case s.Branch.PullRequest == nil:
			return row("·", "passed; waiting for you to submit", submitNext(s, name))
		case !s.Pushed():
			return row("·", fmt.Sprintf("passed; #%d does not have it yet", s.Branch.PullRequest.Number), submitNext(s, name))
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
		if !pr.Observed.ReviewedAt.IsZero() {
			age = " (" + ago(pr.Observed.ReviewedAt) + ")"
		}
		return []attention{{mark: "!", branch: name, what: fmt.Sprintf("#%d changes requested%s", pr.Number, age), next: next}}
	case pr.Observed.Checks == "failing":
		return []attention{{mark: "✗", branch: name, what: fmt.Sprintf("#%d MacPorts CI failing: %s%s", pr.Number, strings.Join(s.Failing(), ", "), age),
			next: "open " + github.PullRequestChecksURL(pr.Repository, pr.Number)}}
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

// submitNext is the submit a branch is ready for, with the fork it needs
// first where no remote pushes to one: a Next: line names only what
// would take the branch (the M1's rerun, D-S6).
func submitNext(s engine.BranchStatus, name string) string {
	if s.NoFork {
		return "fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit --branch " + name
	}
	return "dockhand submit --branch " + name
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
	case s.Cleaned():
		return "cleaned"
	case s.Missing:
		return "branch gone"
	case s.Commits == 0 && len(s.Edited) == 0:
		return "nothing yet"
	case s.Commits == 0:
		return "edits, uncommitted"
	case len(s.Edited) > 0:
		return prose.Plural(s.Commits, "commit") + " + edits"
	}
	return prose.Plural(s.Commits, "commit")
}

// checkState is the checks column: what is running, else what the latest
// check says about the files as they are now.
func checkState(s engine.BranchStatus) string {
	if s.Stopped != nil {
		return fmt.Sprintf("stopped (%s)", s.Stopped.Name())
	}
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
		// A passed check whose results no longer all stand, as after its
		// environment changed, isn't a pass for the branch now. An archived
		// branch asks nothing of its checks, as its attention doesn't, so
		// its check is said as it ended: archived duckdb-cxx14's read
		// "needs another check" (the dogfood run with 3086fdb3).
		if s.Evidence != nil && len(s.Evidence.Missing()) > 0 {
			if s.Branch.State == model.BranchArchived {
				return fmt.Sprintf("passed (%s)", s.Latest.Name())
			}
			return "passed, but needs another check"
		}
		// A check of the working files that tidy then committed unchanged
		// checked the commit's files, as submit credits it.
		if s.Current && (s.LatestRevision.Kind == model.RevisionCommit || s.Commits > 0 && len(s.Edited) == 0) {
			return "passed for this commit"
		}
		if s.Current {
			return "passed for " + engine.Describe(*s.LatestRevision)
		}
		return "passed for older work"
	case model.RunFailed:
		if s.Evidence == nil {
			return fmt.Sprintf("failed (%s)", s.Latest.Name())
		}
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
	if !s.Missing && !s.Pushed() {
		words += ", not pushed"
	}
	return words
}

// readServe is serve's state, as this command's observer sees it.
func readServe(ctx context.Context, e *engine.Engine) (engine.ServeState, error) {
	_, session, end, err := observing(ctx, e)
	if err != nil {
		return engine.ServeState{}, err
	}
	defer end()
	return e.ServeState(ctx, session)
}

// serveWords are serve's state as a line: "serve: running (pid 34857) ·
// queue: 1 run".
func serveWords(s engine.ServeState) string {
	line := "serve: not running"
	if s.Running {
		line = fmt.Sprintf("serve: running (pid %d)", s.PID)
		if s.OpensPullRequests {
			line += " · opens PRs for passing updates"
		}
	}
	switch {
	case s.Queue == 0:
		return line + " · queue: empty"
	case s.Stopped > 0:
		return line + fmt.Sprintf(" · queue: %s, %d stopped", prose.Plural(s.Queue, "run"), s.Stopped)
	}
	return line + " · queue: " + prose.Plural(s.Queue, "run")
}

type observerKey struct{}

// observing gives a command one observer session, a session that only reads
// and judges who is alive, on ctx, which every judgment made under ctx
// shares. Each session is a row and two journal events, and status judged
// twice a render, which watch redrew every 30 seconds. A ctx that has one
// keeps it, and ending it is its opener's.
func observing(ctx context.Context, e *engine.Engine) (context.Context, *coord.Session, func(), error) {
	if session, ok := ctx.Value(observerKey{}).(*coord.Session); ok {
		return ctx, session, func() {}, nil
	}
	session, err := startSession(ctx, e, model.SessionObserver)
	if err != nil {
		return ctx, nil, func() {}, err
	}
	return context.WithValue(ctx, observerKey{}, session), session, func() { session.End(context.WithoutCancel(ctx)) }, nil
}

func showBranch(ctx context.Context, e *engine.Engine, out io.Writer, branch model.Branch) error {
	status, err := judgedStatus(ctx, e, branch)
	if err != nil {
		return err
	}
	head := branch.ShortName()
	if branch.Worktree != "" && !status.Cleaned() {
		head += " · " + tilde(branch.Worktree)
	}
	fmt.Fprintln(out, head)
	if status.Cleaned() {
		// What its Git branch changed, and what checked it, went with it:
		// its pull request says what became of the work.
		fmt.Fprintln(out, "  Work     cleaned after its merge")
	} else {
		ports := portWords(status)
		switch included := status.IncludedPorts(); {
		case ports != "":
		case len(included) > 0:
			ports = fmt.Sprintf("none committed; %s built from untracked files (check --include)", prose.And(included))
		default:
			ports = "none yet"
		}
		fmt.Fprintf(out, "  Ports    %s\n", ports)
		fmt.Fprintf(out, "  Work     %s above master %s\n", workWords(status), engine.Short(branch.Base))
		writeReleases(out, status)
		if len(status.Edited) > 0 {
			fmt.Fprintf(out, "  Edited   %s\n", strings.Join(status.Edited, ", "))
		}
		fmt.Fprintf(out, "  Checks   %s\n", checkState(status))
		if status.Evidence != nil {
			// status has no plan above it, so one environment is named.
			writeResults(out, "           ", *status.Evidence, true)
		}
	}
	pr := prWords(status)
	if opened := status.Branch.PullRequest; opened != nil && opened.Observed == nil {
		// Nothing has read it from GitHub since it was opened: say how.
		pr += " · CI not read yet (dockhand status --refresh)"
	}
	fmt.Fprintf(out, "  PR       %s\n", pr)
	writeNext(out, status)
	return nil
}

// writeReleases shows the releases the branch's updates chose, and below
// them each whose tag named another commit when the check planned it, as
// submit's preview marks it.
func writeReleases(out io.Writer, status engine.BranchStatus) {
	for _, found := range status.Releases {
		fmt.Fprintf(out, "  Release  %s\n", releaseWords(found.Port, found.Release))
	}
	for _, moved := range status.Moved {
		fmt.Fprintf(out, "           ! %s\n", moved.Detail)
	}
}

// writeNext says what moves a branch on, as its attention row does.
func writeNext(out io.Writer, status engine.BranchStatus) {
	for _, row := range attentionFor(status) {
		fmt.Fprintf(out, "Next: %s\n", row.next)
	}
}

// writeNextFor says what moves a branch on after its check, as status
// would.
func writeNextFor(ctx context.Context, e *engine.Engine, out io.Writer, id model.BranchID) error {
	branch, err := e.Branch(ctx, id)
	if err != nil {
		return err
	}
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return err
	}
	writeNext(out, status)
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

// portWords are what a branch changes as status says it, a port read
// from its directory's text rather than its change record marked why.
func portWords(status engine.BranchStatus) string {
	ports, notes := status.ChangedPorts()
	var words []string
	for _, port := range ports {
		if note, ok := notes[port]; ok {
			port += " (" + note + ")"
		}
		words = append(words, port)
	}
	return strings.Join(words, ", ")
}
