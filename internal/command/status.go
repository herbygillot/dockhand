package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func statusCommand(s *settings, streams term.Streams) *cobra.Command {
	var attentionOnly, all, refresh, watch, plain bool
	var where branchFlags
	cmd := &cobra.Command{
		Use:   "status [branch]",
		Short: "Show your branches, and what needs you",
		Long: `Shows each open branch: its ports, its work, its checks, and its pull
request, each in a column of its own, under a list of what needs you, each
row ending with the command that moves it forward. Inside a branch's
worktree, or naming one, it shows that branch in detail.

Below the branches, the checks queued and running, each with its branch,
what it checks, where, and its state; a check whose process ended is said
to be stopped, for dockhand wait to resume.

--attention prints only what needs you, for a prompt or a script, and exits
3 when anything does. -b and --pr name a branch, shown in detail; -p finds
the branches touching a port, shown in detail where one does and listed
where several do. --all
includes merged and archived branches.

--watch keeps status current as serve works, redrawn whenever what it
shows changes, as a check ends or a pull request is reviewed; it only
observes. On a terminal, a line of input runs a command on a branch, with
that command's own confirmations, then returns to the view:
  c <branch>  check       l <branch>  logs of its latest check
  t <branch>  tidy        s <branch>  submit
Enter redraws, and q quits; inside a branch's worktree the branch may be
left out. --plain, and any output that isn't a terminal, prints status once
and then each event as a line, for scrollback, SSH, and screen readers. Your pull requests' state, reviews,
and CI are read from GitHub first where what's recorded is older than serve
reads them, every five minutes; --refresh reads them whatever their age.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if plain && !watch {
				return errors.New("--plain requires --watch")
			}
			if watch && (len(args) > 0 || attentionOnly || all || where.explicit() || streams.JSON()) {
				return errors.New("--watch shows all current status and cannot be combined with a branch selector, --attention, --all, or --json")
			}
			if len(args) > 0 && where.explicit() {
				return errors.New("name the branch once: as the argument, or with -b, -p, or --pr")
			}
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if watch {
				return statusWatch(cmd.Context(), e, streams, plain, s.host.Timings, s.places(), s.command)
			}
			if refresh {
				// A read GitHub refused, as with a revoked login, is the
				// command's error: status is still shown, from what's
				// recorded, but for --json, whose one envelope says it.
				if err := refreshPullRequests(cmd.Context(), e, streams.Err); err != nil {
					if streams.JSON() {
						return err
					}
					if shown := showStatus(cmd.Context(), e, streams, args, attentionOnly, all, where, s.places()); shown != nil {
						return shown
					}
					return err
				}
			} else if !attentionOnly {
				// What's recorded of the pull requests, older than serve
				// reads them, is read now (SQL-O8); --attention, for a
				// prompt, reads nothing.
				readStalePullRequests(cmd.Context(), e, streams.Err)
			}
			return showStatus(cmd.Context(), e, streams, args, attentionOnly, all, where, s.places())
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "read your pull requests' state, reviews, and CI from GitHub first")
	cmd.Flags().BoolVar(&attentionOnly, "attention", false, "print only what needs you; exit 3 when anything does")
	cmd.Flags().BoolVar(&all, "all", false, "include merged, closed, and archived branches")
	where.register(cmd, s, "show")
	cmd.Flags().Lookup("port").Usage = "show the branches touching this port: the one in detail, or several listed"
	cmd.Flags().BoolVar(&watch, "watch", false, "keep status current as serve works, until q or Ctrl-C")
	cmd.Flags().BoolVar(&plain, "plain", false, "with --watch, print events as lines, not a view that redraws")
	supportsJSON(cmd)
	return cmd
}

func showStatus(ctx context.Context, e *engine.Engine, streams term.Streams, args []string, attentionOnly, all bool, where branchFlags, places config.Places) error {
	port := where.port
	ctx, _, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	one := func(branch model.Branch) error {
		if streams.JSON() {
			status, err := judgedStatus(ctx, e, branch)
			if err != nil {
				return err
			}
			viewed := view.BranchView(status)
			streams.Emit(view.StatusJSON{Attention: view.AttentionView(view.AttentionFor(status)), Branches: []view.BranchJSON{viewed}})
		}
		return showBranch(ctx, e, streams.Out, places, branch)
	}
	// A read reaches a merged branch's record, as path does.
	if len(args) == 1 || where.branch != "" {
		branch, err := e.ResolveRecord(ctx, cmp.Or(where.branch, append(args, "")[0]))
		if err != nil {
			return err
		}
		return one(branch)
	}
	if where.pr != 0 {
		branch, err := where.resolve(ctx, e, streams)
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
		statuses = slices.DeleteFunc(statuses, func(s engine.BranchStatus) bool { return !s.Touches(port) })
		// For a read, several aren't ambiguous: one is shown in detail,
		// and several listed (the second look's §3).
		switch {
		case len(statuses) == 1 && !attentionOnly:
			return one(statuses[0].Branch)
		case len(statuses) == 0 && !attentionOnly && !streams.JSON():
			fmt.Fprintf(streams.Out, "No open branch touches %s yet. dockhand update %s starts one.\n", port, port)
			return nil
		}
	}
	var rows []view.Attention
	for _, status := range statuses {
		rows = append(rows, view.AttentionFor(status)...)
	}
	var serve engine.ServeState
	var queue []engine.QueuedCheck
	var processes []engine.Process
	if !attentionOnly {
		if serve, err = readServe(ctx, e); err != nil {
			return err
		}
		_, session, _, err := e.Observing(ctx)
		if err != nil {
			return err
		}
		if queue, err = e.Queue(ctx, session); err != nil {
			return err
		}
		if processes, err = e.Processes(ctx, session); err != nil {
			return err
		}
	}
	out := streams.Out
	if streams.JSON() {
		result := view.StatusJSON{Attention: view.AttentionView(rows)}
		if !attentionOnly {
			result.Branches = []view.BranchJSON{}
			for _, status := range statuses {
				result.Branches = append(result.Branches, view.BranchView(status))
			}
			viewed := view.ServeView(serve)
			result.Serve, result.ServeState = serveWords(serve), &viewed
			result.Queue = view.QueueView(queue)
			result.Processes = view.ProcessesView(processes)
		}
		streams.Emit(result)
	}
	if attentionOnly {
		writeAttention(out, rows, streams.Width())
		if len(rows) > 0 {
			return &term.ExitError{Code: term.ExitAttention}
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
		writeAttention(out, rows, streams.Width())
		fmt.Fprintln(out)
	}
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No open branches. dockhand update <port> starts one for a port, and dockhand start <name> one for other work.")
	} else {
		table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "BRANCH\tPORTS\tWORK\tCHECKS\tPR")
		for _, status := range statuses {
			ports := strconv.Itoa(len(status.Scope.Ports))
			if len(status.Scope.Ports) == 0 && len(status.Branch.Ports) > 0 {
				// The ports it was started for, which it doesn't change yet.
				ports = strconv.Itoa(len(status.Branch.Ports))
			}
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
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", name, ports, workWords(status), view.CheckState(status), prWords(status))
		}
		table.Flush()
	}
	if found, ok := e.LastOutdatedLook(); ok && (len(found.Outdated) > 0 || len(found.Uncertain) > 0) {
		fmt.Fprintln(out)
		if len(found.Outdated) > 0 {
			fmt.Fprintf(out, "Your ports: %s newer releases, as serve found %s (dockhand ship --mine)\n", prose.Plural(len(found.Outdated), "port")+map[bool]string{true: " has", false: " have"}[len(found.Outdated) == 1], engine.Ago(found.CheckedAt))
		}
		if len(found.Uncertain) > 0 {
			fmt.Fprintf(out, "Your ports: %s may have newer releases; review needed. Last checked %s by serve. Run dockhand outdated %s for details.\n", prose.Plural(len(found.Uncertain), "port"), engine.Ago(found.CheckedAt), strings.Join(found.Uncertain, " "))
		}
	}
	view.WriteQueue(out, queue)
	writeProcesses(out, processes)
	fmt.Fprintf(out, "\n%s\n", serveWords(serve))
	return nil
}

// writeProcesses lists the dockhand processes working on the checkout,
// each with what it does, and one whose schema isn't the database's said,
// where the exiv2 agent compared pgrep and versions by hand.
func writeProcesses(out io.Writer, processes []engine.Process) {
	if len(processes) == 0 {
		return
	}
	fmt.Fprintln(out, "\nDockhand processes")
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "  PID\tKIND\tBUILD\tSINCE\tDOING")
	for _, process := range processes {
		fmt.Fprintf(table, "  %d\t%s\t%s\t%s\t%s\n", process.Session.PID, process.Session.Kind, cmp.Or(process.Session.Version, "unknown"), engine.Ago(process.Session.StartedAt), processWords(process))
	}
	table.Flush()
	for _, process := range processes {
		if process.Expects != 0 {
			fmt.Fprintf(out, "! pid %d expects schema %d and the database is at %d: what it records may not be read; stop it and run this dockhand\n", process.Session.PID, process.Expects, process.Database)
		}
	}
}

// processWords is what a process is doing: leading the queue, running
// checks, or neither.
func processWords(process engine.Process) string {
	var doing []string
	if process.Leads {
		doing = append(doing, "leads the queue")
	}
	for _, run := range process.Runs {
		doing = append(doing, "runs "+run.Name())
	}
	if len(doing) == 0 {
		return "—"
	}
	return strings.Join(doing, ", ")
}

// judgeStopped marks the statuses whose check stopped when the process
// running it ended, which only a session can judge.
func judgeStopped(ctx context.Context, e *engine.Engine, statuses []engine.BranchStatus) error {
	_, session, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	return e.JudgeStopped(ctx, session, statuses)
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

// writeAttention writes the rows, each one's message wrapped at width
// beside its branch and its command on the line below: one long message
// set every row's command column, which ended near column 400 (the
// gnome-voice session's notes, 2026-10-20).
func writeAttention(out io.Writer, rows []view.Attention, width int) {
	column := 0
	for _, row := range rows {
		column = max(column, len([]rune(row.Branch)))
	}
	// "  ! " and the branch, then two spaces.
	column += 6
	indent := strings.Repeat(" ", column)
	for _, row := range rows {
		lines := strings.Split(prose.Wrap(row.What, max(width-column, 20)), "\n")
		fmt.Fprintf(out, "  %s %-*s  %s\n", row.Kind.Mark(), column-6, row.Branch, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
		if row.Next != "" {
			fmt.Fprintf(out, "%s%s\n", indent, row.Next)
		}
	}
}

func workWords(s engine.BranchStatus) string {
	switch {
	case s.Cleaned() && s.Branch.CleanKept != "":
		return "cleaned, " + s.Branch.CleanKept
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
		case observed.State != model.PullRequestOpen:
			words += " " + string(observed.State)
		case observed.Review == model.ReviewChangesRequested:
			words += " changes requested"
		case observed.Review == model.ReviewApproved:
			words += " approved"
		}
		switch observed.Checks {
		case model.ChecksPassing:
			words += ", CI ✓"
		case model.ChecksFailing:
			words += ", CI ✗"
		case model.ChecksPending:
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
	_, session, end, err := e.Observing(ctx)
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
		// A serve on another build than this command's is said.
		if build, upgraded := s.Upgraded(); upgraded {
			line += fmt.Sprintf(" · running dockhand %s; this command is %s", build, buildinfo.Current())
		}
		// A serve not acting on GitHub, as its login stands, says why.
		if s.NotActing != "" {
			line += " · not acting on GitHub: " + s.NotActing
		}
		// One that couldn't read its login says so, where it said nothing
		// and kept its state (the rc8 full stage's D-C3).
		if s.LoginProblem != "" {
			line += " · couldn't read its GitHub login: " + s.LoginProblem
		}
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

func showBranch(ctx context.Context, e *engine.Engine, out io.Writer, places config.Places, branch model.Branch) error {
	status, err := judgedStatus(ctx, e, branch)
	if err != nil {
		return err
	}
	head := branch.ShortName()
	if branch.Worktree != "" && !status.Cleaned() {
		head += " · " + places.Tilde(branch.Worktree)
	}
	fmt.Fprintln(out, head)
	if status.Cleaned() {
		// What its Git branch changed, and what checked it, went with it:
		// its pull request says what became of the work.
		fmt.Fprintln(out, "  Work     cleaned after its merge")
		if branch.CleanKept != "" {
			fmt.Fprintf(out, "  Kept     %s\n", branch.CleanKept)
		}
	} else {
		ports := portWords(status)
		switch included := status.IncludedPorts(); {
		case ports != "":
		case len(included) > 0:
			ports = fmt.Sprintf("none committed; %s built from untracked files (check --include)", prose.And(included))
		case len(branch.Ports) > 0:
			ports = "none changed yet; started for " + prose.And(branch.Ports)
		default:
			ports = "none yet"
		}
		fmt.Fprintf(out, "  Ports    %s\n", ports)
		fmt.Fprintf(out, "  Work     %s above master %s\n", workWords(status), branch.Base.Short())
		writeReleases(out, status)
		if len(status.Edited) > 0 {
			fmt.Fprintf(out, "  Edited   %s\n", strings.Join(status.Edited, ", "))
		}
		fmt.Fprintf(out, "  Checks   %s\n", view.CheckState(status))
		if status.Evidence != nil {
			// status has no plan above it, so one environment is named.
			view.WriteResults(out, "           ", *status.Evidence, true)
			if status.Evidence.Problem != "" {
				fmt.Fprintf(out, "           ! %s\n", status.Evidence.Problem)
			}
		}
		// Every hold, where the attention row says the first (C2).
		for i, line := range engine.HoldLines(status.Held) {
			label := "         "
			if i == 0 {
				label = "Holds    "
			}
			fmt.Fprintf(out, "  %s! %s\n", label, line)
		}
	}
	pr := prWords(status)
	if opened := status.Branch.PullRequest; opened != nil && opened.Observed == nil {
		// Nothing has read it from GitHub since it was opened: say how.
		pr += " · CI not read yet (dockhand status --refresh)"
	}
	fmt.Fprintf(out, "  PR       %s\n", pr)
	view.WriteNext(out, status)
	return nil
}

// writeReleases shows the releases the branch's updates chose, and below
// them each whose tag named another commit when the check planned it, as
// submit's preview marks it.
func writeReleases(out io.Writer, status engine.BranchStatus) {
	for _, found := range status.Releases {
		fmt.Fprintf(out, "  Release  %s\n", engine.ReleaseWords(found.Port, found.Release))
	}
	for _, moved := range status.Moved {
		fmt.Fprintf(out, "           ! %s\n", moved.Detail)
	}
}

// refreshPullRequests reads the pull requests and says what changed, and
// what could not be read. GitHub refusing the read altogether is its
// error, worded once for every command that reads them (status, clean),
// and said, as its caller goes on by what was last read; the store's is
// returned as it is.
func refreshPullRequests(ctx context.Context, e *engine.Engine, out io.Writer) error {
	refreshed, err := readPullRequests(ctx, e, out)
	if err != nil {
		return err
	}
	for _, r := range refreshed.Branches {
		if r.Err != nil {
			fmt.Fprintf(out, "%s: could not read #%d: %v\n", r.Branch.ShortName(), r.Branch.PullRequest.Number, r.Err)
		}
	}
	if refreshed.Refused != nil {
		return refusedRead{refreshed.Refused}
	}
	return nil
}

// readPullRequests is the one read of the pull requests status and clean
// make, saying what changed of each; its callers say what couldn't be read
// as they go on (the architecture review of 5897a82, R3-39).
func readPullRequests(ctx context.Context, e *engine.Engine, out io.Writer) (engine.RefreshResult, error) {
	refreshed, err := e.RefreshPullRequests(ctx)
	if err != nil {
		return refreshed, err
	}
	for _, r := range refreshed.Branches {
		for _, change := range r.Changes {
			fmt.Fprintf(out, "%s: %s\n", r.Branch.ShortName(), change)
		}
	}
	return refreshed, nil
}

// refusedRead is GitHub refusing to let the pull requests be read at
// all, as a revoked login: one error for every pull request.
type refusedRead struct{ err error }

func (r refusedRead) Error() string { return "could not read pull requests: " + r.err.Error() }
func (r refusedRead) Unwrap() error { return r.err }

// staleReadTimeout bounds the read status makes by itself, so a slow or
// unreachable GitHub delays it only so long.
const staleReadTimeout = 20 * time.Second

// readStalePullRequests reads the pull requests where what's recorded of
// them is older than serve reads them (engine.PullRequestsStale), as a
// plain status does by itself, where it said "CI not read yet" until
// --refresh. It says what changed; a read that fails, wholly or for some,
// is said in one line, and status goes on by what was last read.
func readStalePullRequests(ctx context.Context, e *engine.Engine, out io.Writer) {
	if stale, err := e.PullRequestsStale(ctx); err != nil || !stale {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, staleReadTimeout)
	defer cancel()
	refreshed, err := readPullRequests(ctx, e, out)
	if err == nil {
		err = refreshed.Refused
	}
	var failed []string
	for _, r := range refreshed.Branches {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("#%d", r.Branch.PullRequest.Number))
			if err == nil {
				err = r.Err
			}
		}
	}
	switch {
	case refreshed.Refused != nil:
		fmt.Fprintf(out, "Could not refresh pull requests; showing previously recorded status: %v\n", err)
	case len(failed) > 0:
		fmt.Fprintf(out, "Could not refresh %s; showing previously recorded status: %v\n", strings.Join(failed, ", "), err)
	case err != nil:
		fmt.Fprintf(out, "Could not refresh pull requests; showing previously recorded status: %v\n", err)
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
