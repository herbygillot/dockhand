package status

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var attentionOnly, all, refresh, watch, plain bool
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "status [branch]",
		Short: "Show branches and work that needs attention",
		Long: `Shows each open branch: its ports, its work, its checks, and its pull
request, each in a column of its own, under a list of what needs attention, each
row ending with the command that moves it forward. Inside a branch's
worktree, or naming one, it shows that branch in detail.

Below the branches, the checks queued and running, each with its branch,
what it checks, where, and its state; a check whose process ended is said
to be stopped, for dockhand wait to resume.

--attention prints only what needs attention, for a prompt or a script, and exits
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
			if watch && (len(args) > 0 || attentionOnly || all || where.Explicit() || streams.JSON()) {
				return errors.New("--watch shows all current status and cannot be combined with a branch selector, --attention, --all, or --json")
			}
			if len(args) > 0 && where.Explicit() {
				return errors.New("name the branch once: as the argument, or with -b, -p, or --pr")
			}
			e, err := s.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if watch {
				return statusWatch(cmd.Context(), e, streams, plain, s.Host.Timings, s.Places(), s.Command)
			}
			if refresh {
				// A read GitHub refused, as with a revoked login, is the
				// command's error: status is still shown, from what's
				// recorded, but for --json, whose one envelope says it.
				if err := readAndSay(cmd.Context(), e, streams.Err); err != nil {
					if streams.JSON() {
						return err
					}
					if shown := ShowStatus(cmd.Context(), e, streams, args, attentionOnly, all, where, s.Places()); shown != nil {
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
			return ShowStatus(cmd.Context(), e, streams, args, attentionOnly, all, where, s.Places())
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "read your pull requests' state, reviews, and CI from GitHub first")
	cmd.Flags().BoolVar(&attentionOnly, "attention", false, "print only what needs attention; exit 3 when anything does")
	cmd.Flags().BoolVar(&all, "all", false, "include merged, closed, and archived branches")
	where.Register(cmd, s, "show")
	cmd.Flags().Lookup("port").Usage = "show the branches touching this port: the one in detail, or several listed"
	cmd.Flags().BoolVar(&watch, "watch", false, "keep status current as serve works, until q or Ctrl-C")
	cmd.Flags().BoolVar(&plain, "plain", false, "with --watch, print events as lines, not a view that redraws")
	settings.SupportsJSON(cmd)
	return cmd
}

// ShowStatus gathers what status shows in one engine.StatusReport, then
// writes its text and JSON from the report alone (Then's item 6): it judged
// a branch twice under --json, and read its links while writing it.
func ShowStatus(ctx context.Context, e *engine.Engine, streams term.Streams, args []string, attentionOnly, all bool, where branchflags.BranchFlags, places config.Places) error {
	port := where.Port
	ctx, session, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	one := func(branch model.Branch) error {
		report, err := e.StatusReport(ctx, session, engine.StatusRequest{Branches: []model.Branch{branch}})
		if err != nil {
			return err
		}
		status := report.Statuses[0]
		if streams.JSON() {
			viewed := view.BranchView(status)
			streams.Emit(view.StatusJSON{Attention: view.AttentionView(view.AttentionFor(status)), Branches: []view.BranchJSON{viewed}})
		}
		showBranch(streams.Out, places, status, report)
		return nil
	}
	// A read reaches a merged branch's record, as path does.
	if len(args) == 1 || where.Branch != "" {
		branch, err := e.ResolveRecord(ctx, cmp.Or(where.Branch, append(args, "")[0]))
		if err != nil {
			return err
		}
		return one(branch)
	}
	if where.PR != 0 {
		branch, err := where.Resolve(ctx, e, streams)
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
	report, err := e.StatusReport(ctx, session, engine.StatusRequest{States: states, Port: port, Overview: !attentionOnly})
	if err != nil {
		return err
	}
	statuses := report.Statuses
	if port != "" {
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
	serve, queue, processes, changes := report.Serve, report.Queue, report.Processes, report.Changes
	out := streams.Out
	if streams.JSON() {
		result := view.StatusJSON{Attention: view.AttentionView(rows)}
		if !attentionOnly {
			result.Branches = []view.BranchJSON{}
			for _, status := range statuses {
				result.Branches = append(result.Branches, view.BranchView(status))
			}
			viewed := view.ServeView(serve)
			result.Serve, result.ServeState = view.ServeWords(serve), &viewed
			result.Queue = view.QueueView(queue)
			result.Processes = view.ProcessesView(processes)
			result.Changed = view.ChangesView(changes)
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
	writeOverview(out, streams, statuses, rows, processes, all)
	if len(changes) > 0 {
		fmt.Fprintln(out)
		for _, change := range changes {
			fmt.Fprintln(out, view.ChangeWords(change))
		}
	}
	if found := report.Look; found != nil {
		fmt.Fprintln(out)
		if len(found.Outdated) > 0 {
			fmt.Fprintf(out, "%s: %s newer releases, as serve found %s (%s)\n", e.Serves.Title(), prose.Plural(len(found.Outdated), "port")+map[bool]string{true: " has", false: " have"}[len(found.Outdated) == 1], engine.Ago(found.CheckedAt), e.Serves.Command("ship"))
		}
		if len(found.Uncertain) > 0 {
			fmt.Fprintf(out, "%s: %s may have newer releases; review needed. Last checked %s by serve. Run dockhand outdated %s for details.\n", e.Serves.Title(), prose.Plural(len(found.Uncertain), "port"), engine.Ago(found.CheckedAt), strings.Join(found.Uncertain, " "))
		}
	}
	// The checks and the processes are -v's tables: a check shows on its
	// branch's row, and other processes are a line (part 5, item 4).
	if streams.Verbose() > 0 {
		view.WriteQueue(out, queue, serve.Running)
		writeProcesses(out, processes)
	} else if others := len(processes); others > 0 {
		fmt.Fprintf(out, "\n%s working on this checkout; dockhand status -v lists %s.\n", prose.Plural(others, "other dockhand process"), map[bool]string{true: "it", false: "them"}[others == 1])
	}
	fmt.Fprintf(out, "\n%s\n", view.ServeWords(serve))
	return nil
}

// writeOverview is the branches by bucket (branchstate.BucketOf): what
// needs you, each row's why wrapped and its command below it; what's under
// way, a line each; and what waits on reviewers, counted, and listed with
// -v, which gives the whole table of branches, as status gave it before;
// --all lists the merged, closed, and archived too. With nothing for the
// person, it's one line.
func writeOverview(out io.Writer, streams term.Streams, statuses []engine.BranchStatus, rows []view.Attention, processes []engine.Process, all bool) {
	if len(statuses) == 0 && len(rows) == 0 {
		fmt.Fprintln(out, "No open branches. Run dockhand update <port> to start a port update, or dockhand start <name> for other work.")
		return
	}
	// A process that expects another schema needs you: what it records may
	// not be read.
	for _, process := range processes {
		if process.Expects != 0 {
			rows = append(rows, view.Attention{Kind: view.AttentionNeedsYou, Branch: fmt.Sprintf("pid %d", process.Session.PID),
				What: fmt.Sprintf("expects schema %d and the database is at %d: what it records may not be read", process.Expects, process.Database),
				Next: "stop it, and run this dockhand"})
		}
	}
	buckets := map[branchstate.Bucket][]engine.BranchStatus{}
	for _, status := range statuses {
		bucket := branchstate.BucketOf(status.Facts())
		buckets[bucket] = append(buckets[bucket], status)
	}
	working, waiting, done := buckets[branchstate.BucketWorking], buckets[branchstate.BucketWaiting], buckets[branchstate.BucketDone]
	said := false
	group := func(heading string, count int) {
		if said {
			fmt.Fprintln(out)
		}
		said = true
		fmt.Fprintf(out, "%s (%d)\n", heading, count)
	}
	if len(rows) > 0 {
		group("Needs attention", len(rows))
		writeAttention(out, rows, streams.Width())
	}
	if len(working) > 0 {
		group("Working", len(working))
		table := rowsWriter(out)
		for _, status := range working {
			next := ""
			if step, ok := branchstate.Working(status.Facts()); ok {
				next = view.RenderNext(step, "")
			}
			fmt.Fprintf(table, "  %s %s\t%s\t%s\n", view.Working, status.Branch.ShortName(), doingWords(status), next)
		}
		table.Flush()
	}
	if len(waiting) > 0 {
		heading := fmt.Sprintf("Waiting on reviewers (%d): %s", len(waiting), ciTally(waiting))
		if said {
			fmt.Fprintln(out)
		} else {
			// Nothing else to say is one line.
			heading = fmt.Sprintf("Nothing needs attention; %s %s on reviewers (%s)", prose.Plural(len(waiting), "pull request"), map[bool]string{true: "waits", false: "wait"}[len(waiting) == 1], ciTally(waiting))
		}
		said = true
		if streams.Verbose() == 0 {
			heading += " · dockhand status -v lists them"
		}
		fmt.Fprintln(out, heading)
	}
	if all && len(done) > 0 {
		group("Merged, closed, and archived", len(done))
		table := rowsWriter(out)
		for _, status := range done {
			// An archived branch is said, as clean says it.
			name := status.Branch.ShortName()
			if status.Branch.State == model.BranchArchived {
				name += " (archived)"
			}
			fmt.Fprintf(table, "  %s %s\t%s\t%s\n", view.Unmarked, name, workWords(status), prWords(status))
		}
		table.Flush()
	}
	if !said {
		fmt.Fprintln(out, "Nothing needs attention.")
	}
	if streams.Verbose() > 0 {
		fmt.Fprintln(out)
		writeBranches(out, statuses)
	}
}

// rowsWriter sets a group's rows in columns, a line each with no spaces
// after its last word, as a row with no command would leave.
func rowsWriter(out io.Writer) *trimmedTable {
	t := &trimmedTable{out: out}
	t.Writer = tabwriter.NewWriter(&t.buf, 0, 0, 2, ' ', 0)
	return t
}

// trimmedTable is a tabwriter whose lines are written without trailing
// spaces once it's flushed.
type trimmedTable struct {
	*tabwriter.Writer
	buf bytes.Buffer
	out io.Writer
}

func (t *trimmedTable) Flush() {
	t.Writer.Flush()
	for line := range strings.SplitSeq(strings.TrimSuffix(t.buf.String(), "\n"), "\n") {
		fmt.Fprintln(t.out, strings.TrimRight(line, " "))
	}
	t.buf.Reset()
}

// doingWords is what a branch under way is doing: its work, and its check
// where it has one.
func doingWords(status engine.BranchStatus) string {
	words := workWords(status)
	if check := view.CheckState(status); check != "—" {
		words += " · " + check
	}
	return words
}

// ciTally counts the waiting pull requests by their CI: "CI passed 134,
// running 8".
func ciTally(waiting []engine.BranchStatus) string {
	counts := map[model.PullRequestChecks]int{}
	for _, status := range waiting {
		checks := model.PullRequestChecks("")
		if observed := status.Branch.PullRequest.Observed; observed != nil {
			checks = observed.Checks
		}
		counts[checks]++
	}
	var tally []string
	for _, kind := range []struct {
		checks model.PullRequestChecks
		words  string
	}{{model.ChecksPassing, "passed"}, {model.ChecksPending, "running"}, {model.ChecksNone, "none"}, {"", "not read yet"}} {
		if n := counts[kind.checks]; n > 0 {
			tally = append(tally, fmt.Sprintf("%s %d", kind.words, n))
			delete(counts, kind.checks)
		}
	}
	for _, n := range counts {
		tally = append(tally, fmt.Sprintf("other %d", n))
	}
	if len(tally) == 0 {
		return "CI not read yet"
	}
	return "CI " + strings.Join(tally, ", ")
}

// writeBranches is every branch in a table, -v's: its ports, work, checks,
// and pull request.
func writeBranches(out io.Writer, statuses []engine.BranchStatus) {
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

// writeProcesses lists the dockhand processes working on the checkout,
// each with what it does, where the exiv2 agent compared pgrep and
// versions by hand; one whose schema isn't the database's is a row of
// what needs you.
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
	// A merged or closed branch clean kept is kept, and why, never
	// cleaned.
	case s.Branch.State != model.BranchOpen && s.Branch.CleanKept != "":
		return s.Branch.CleanKept
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

// showBranch writes one branch's status in detail, its latest check's
// links from what the report gathered.
func showBranch(out io.Writer, places config.Places, status engine.BranchStatus, report engine.StatusReport) {
	branch := status.Branch
	head := branch.ShortName()
	if branch.Worktree != "" && !status.Cleaned() {
		head += view.Where(places, branch.Worktree)
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
		// The person's decisions on them, which stand for these files (the
		// person's voice).
		for i, decision := range status.Decided {
			label := "         "
			if i == 0 {
				label = "Decided  "
			}
			fmt.Fprintf(out, "  %s%s\n", label, engine.DecisionWords(decision))
		}
		// A hand edit, with why dockhand left it to the person (Then's
		// item 3).
		for i, hand := range status.HandEdits {
			label := "         "
			if i == 0 {
				label = "By hand  "
			}
			fmt.Fprintf(out, "  %s! %s: %s\n", label, hand.Directory, hand.Line())
		}
		// A maintainer's instruction in its Portfiles' comments, quoted
		// (the docs dig's I7).
		for i, line := range report.Instructions[status.Branch.ID] {
			label := "         "
			if i == 0 {
				label = "Comments "
			}
			fmt.Fprintf(out, "  %s! %s\n", label, line)
		}
		// What the latest check's builds link and don't declare, a notice
		// (v0.5.0 part 6).
		if status.Latest != nil {
			// What lint warned of, as the check's report says (the docs
			// dig's I13).
			for i, line := range view.LintLines(report.Lint[status.Latest.ID]) {
				label := "         "
				if i == 0 {
					label = "Lint     "
				}
				fmt.Fprintf(out, "  %s%s %s\n", label, line.Mark, line.Words)
			}
			if links, ok := report.Links[status.Latest.ID]; ok {
				for i, line := range view.LinkLines(links) {
					label := "         "
					if i == 0 {
						label = "Links    "
					}
					fmt.Fprintf(out, "  %s%s %s\n", label, line.Mark, line.Words)
				}
			}
			// And what it installed and configure found against the port's
			// earlier build, as the check's report says (Then's item 9).
			for i, line := range view.InstalledLines(report.Installed[status.Latest.ID]) {
				label := "         "
				if i == 0 {
					label = "Against  "
				}
				fmt.Fprintf(out, "  %s%s %s\n", label, line.Mark, line.Words)
			}
		}
	}
	pr := prWords(status)
	if opened := status.Branch.PullRequest; opened != nil && opened.Observed == nil {
		// Nothing has read it from GitHub since it was opened: say how.
		pr += " · CI not read yet (dockhand status --refresh)"
	}
	fmt.Fprintf(out, "  PR       %s\n", pr)
	view.WriteNext(out, status)
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

// readStalePullRequests reads the pull requests where what's recorded of
// them is older than serve reads them (engine.PullRequestsStale), as a
// plain status does by itself, where it said "CI not read yet" until
// --refresh. It says what changed; a read that fails, wholly or for some,
// is said in one line, and status goes on by what was last read.
func readStalePullRequests(ctx context.Context, e *engine.Engine, out io.Writer) {
	refreshed, read, err := e.RefreshStalePullRequests(ctx)
	if !read {
		return
	}
	view.WriteChanges(out, refreshed)
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

// readAndSay reads the pull requests and says what it found
// (view.WriteRefreshed), as clean does.
func readAndSay(ctx context.Context, e *engine.Engine, out io.Writer) error {
	refreshed, err := e.RefreshPullRequests(ctx)
	if err != nil {
		return err
	}
	return view.WriteRefreshed(out, refreshed)
}
