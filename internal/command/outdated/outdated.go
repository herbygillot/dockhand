package outdated

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// outdatedRequest is what --mine and the ports named choose.
func outdatedRequest(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams, ports []string, mine bool) (engine.OutdatedRequest, *lookups, error) {
	looked := newLookups(streams)
	request := engine.OutdatedRequest{Ports: ports, Progress: looked.progress, Allow: allowLook(e.GitHubAllowance, streams, s.OverAllowance)}
	if !mine {
		if len(ports) == 0 {
			return request, looked, errors.New("name ports, or choose yours with --mine")
		}
		return request, looked, nil
	}
	if len(ports) > 0 {
		return request, looked, errors.New("--mine chooses your ports; name ports or use --mine, not both")
	}
	if request.Maintainers = s.File.Maintainers(); len(request.Maintainers) == 0 {
		// The person's own line, as master's ports write their GitHub
		// login, where it can be found, as serve suggests it.
		return request, looked, fmt.Errorf("--mine needs to know who you are, as your ports' maintainers lines name you: %s", e.MaintainerHint(ctx))
	}
	return request, looked, nil
}

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var mine, all bool
	cmd := &cobra.Command{
		Use:   "outdated [port...]",
		Short: "Show which ports have newer releases upstream",
		Long: `Looks up the newest release of each port named, or with --mine each port
whose maintainers line names you (the config's maintainer), at MacPorts'
master as fetched now. It lists the ports with newer releases, and counts
the rest; --current lists every port, with why any couldn't be checked.

update --mine starts on them, a branch for each; ship --mine --to check
checks them too.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			request, looked, err := outdatedRequest(ctx, s, e, streams, args, mine)
			if err != nil {
				return err
			}
			report, err := e.Outdated(ctx, request)
			looked.stop()
			if err != nil {
				if looked.cutShort(ctx, report) {
					streams.Emit(view.OutdatedView(report))
					fmt.Fprintf(streams.Out, "Interrupted after looking up %d of %d ports; what those found:\n", len(report.Ports), looked.total.Load())
					_ = writeOutdated(context.WithoutCancel(ctx), e, streams.Out, report, all)
				}
				return err
			}
			streams.Emit(view.OutdatedView(report))
			return writeOutdated(ctx, e, streams.Out, report, all)
		},
	}
	cmd.Flags().BoolVar(&mine, "mine", false, "the ports whose maintainers line names you (config maintainer)")
	settings.OverAllowanceFlag(cmd, s)
	cmd.Flags().BoolVar(&all, "current", false, "list current ports and those that couldn't be checked too")
	settings.SupportsJSON(cmd)
	return cmd
}

func writeOutdated(ctx context.Context, e *engine.Engine, out io.Writer, report engine.OutdatedReport, all bool) error {
	summary := report.Summary()
	newer, unknown, uncertain, moved, own := summary[engine.OutdatedNewer], summary[engine.OutdatedUnchecked], summary[engine.OutdatedUncertain], summary[engine.OutdatedMoved], summary[engine.OutdatedOwnVersion]
	unready := summary[engine.OutdatedUnpublished]
	// One ahead of every version upstream lists may have a newer release
	// elsewhere, as one with a version set aside may: each is for a look.
	uncertain += summary[engine.OutdatedAhead]
	// Where no port could be checked, as offline, each says why, and the
	// command fails: "none has a newer release" would be a guess (the M1's
	// quick stage, D-N1, 2026-10-03).
	none := len(report.Ports) > 0
	for _, port := range report.Ports {
		none = none && port.Problem != ""
	}
	// A subport checked with a sibling that's listed moves with it, so
	// it's said on the sibling's row rather than its own: five python
	// ports took about 25 lines, and a subport's row said "update with
	// py-flatbuffers" while that update was on a branch already (field
	// testing, 2026-10-02).
	listed := map[string]bool{}
	for _, port := range report.Ports {
		listed[port.Port] = true
	}
	following := map[string]int{}
	for _, port := range report.Ports {
		if port.With != "" && listed[port.With] {
			following[port.With]++
		}
	}
	with := func(port string) string {
		if n := following[port]; n > 0 {
			return fmt.Sprintf(" (%s with it)", prose.Plural(n, "subport"))
		}
		return ""
	}
	// The ports by what dockhand can do with them, a group each (part 5,
	// item 4): ready to ship, held for a look, not checked, and with
	// --current, current.
	var ready, held, unchecked, current []outdatedRow
	for _, port := range report.Ports {
		folded := port.With != "" && listed[port.With]
		class := port.Class()
		row := outdatedRow{port: port.Port, now: orDash(port.Current), newest: orDash(port.Newest)}
		switch {
		case class == engine.OutdatedOwnVersion:
			row.note = "it fetches nothing here, and no livecheck reads its version"
			current = append(current, row)
		case class == engine.OutdatedMoved:
			// A port pinned to a commit of a branch that has moved on is
			// behind; update moves the pin to the head, with the version
			// the port's scheme gives it, or the one named (the roadmap's
			// v0.5.0 part 3, step 2).
			row.newest += "?"
			row.note = fmt.Sprintf("dockhand update %s: its %s names a newer commit than the one it pins", port.Port, port.Moved.Branch)
			ready = append(ready, row)
		case class == engine.OutdatedUnchecked:
			row.newest = "?"
			row.note = strings.ReplaceAll(port.Problem, "\n", "; ")
			unchecked = append(unchecked, row)
		case class == engine.OutdatedUncertain:
			// Neither current nor outdated for sure, so it's held for a
			// look, with the update that takes it.
			row.newest += "?"
			row.note = fmt.Sprintf("update %s %s after a look: %s", port.Port, port.Uncertain[0].Source, engine.SetAsideWords(port.Uncertain))
			held = append(held, row)
		case class == engine.OutdatedAhead:
			// Checked, and ahead of everything upstream lists: its
			// releases may be elsewhere now, so it's held for a look
			// (field testing's mergestat, 2026-10-23).
			row.newest += "?"
			row.note = fmt.Sprintf("update %s <version> after a look: %s", port.Port, engine.AheadWords(port.Current, port.Ahead))
			held = append(held, row)
		case class == engine.OutdatedUnpublished:
			// Newer, but update would refuse it until its release has
			// its files (field testing at 43949b89, browsh).
			row.note = "not ready: " + port.Unpublished
			held = append(held, row)
		case class == engine.OutdatedNewer && folded:
		case class == engine.OutdatedNewer:
			can := "update"
			open, err := e.BranchesChanging(ctx, port.Port)
			if err != nil {
				return err
			}
			switch {
			case len(open) > 0:
				can = "already in " + open[0].ShortName()
			case port.With != "":
				can = "update with " + port.With
			}
			row.note = can + with(port.Port)
			// A major version bump, which serve would hold
			// (holds.majors).
			if e.HoldsMajor(port.Port, port.Current, port.Newest) {
				row.note = "a new major version, held for a look unattended; " + row.note
				held = append(held, row)
				continue
			}
			ready = append(ready, row)
		case folded:
		case port.With != "":
			row.note = fmt.Sprintf("it is current, as %s is", port.With)
			current = append(current, row)
		default:
			row.note = "it is current" + with(port.Port)
			current = append(current, row)
		}
	}
	writeGroup(out, "Ready to ship", ready)
	writeGroup(out, "Held", held)
	if all || none {
		writeGroup(out, "Not checked", unchecked)
	}
	if all {
		writeGroup(out, "Current", current)
	}
	if none {
		return noneChecked(report)
	}
	ports, master := prose.Plural(len(report.Ports), "port"), report.Master.Short()
	var line string
	alone := len(report.Ports) == 1 && newer == 0 && unknown == 0 && unready == 0
	// What couldn't be checked leads, and the result is the rest's, which
	// it can't stand for: "None of 4 ports has a newer release" was said
	// where the 2 it couldn't check were due (the rc6 full stage, D-N4).
	if unknown > 0 {
		fmt.Fprintln(out, report.UncheckedWords())
		checked := len(report.Ports) - unknown
		ports = prose.Plural(checked, "port") + " checked"
	}
	switch {
	case unknown > 0 && newer == 0:
		line = fmt.Sprintf("Of the %s, none has a newer release, at master %s", ports, master)
	case unknown > 0:
		line = fmt.Sprintf("Of the %s, %d %s a newer release, at master %s", ports, newer, map[bool]string{true: "has", false: "have"}[newer == 1], master)
	case len(report.Ports) == 1 && unready == 1:
		line = fmt.Sprintf("%s has a newer release, not ready yet, at master %s", report.Ports[0].Port, master)
		unready = 0
	case alone && own == 1:
		line = fmt.Sprintf("%s has no release to look for, at master %s: it fetches nothing here, and no livecheck reads its version", report.Ports[0].Port, master)
	case alone && uncertain == 0:
		line = fmt.Sprintf("%s has no newer release, at master %s", report.Ports[0].Port, master)
	case alone:
		line = fmt.Sprintf("%s may have a newer release, at master %s", report.Ports[0].Port, master)
	case newer == 0:
		line = fmt.Sprintf("None of %s has a newer release, at master %s", ports, master)
	case newer == 1:
		line = fmt.Sprintf("1 of %s has a newer release, at master %s", ports, master)
	default:
		line = fmt.Sprintf("%d of %s have newer releases, at master %s", newer, ports, master)
	}
	if uncertain > 0 && !alone {
		line += fmt.Sprintf(" · %d may have one, for a look", uncertain)
	}
	switch {
	case unready == 1:
		line += " · 1 has a release not ready yet"
	case unready > 1:
		line += fmt.Sprintf(" · %d have releases not ready yet", unready)
	}
	switch {
	case moved == 1:
		line += " · 1 tracks a branch with a newer commit than it pins"
	case moved > 1:
		line += fmt.Sprintf(" · %d track branches with newer commits than they pin", moved)
	}
	switch {
	case own == 1 && !alone:
		line += " · 1 has no release to look for"
	case own > 1:
		line += fmt.Sprintf(" · %d have no release to look for", own)
	}
	if unknown > 0 && !all {
		line += " · --current lists those it couldn't check"
	}
	fmt.Fprintln(out, line)
	if summary[engine.OutdatedUncertain] > 0 {
		// Serve says such a port once, but outdated every time it's asked:
		// the port's own livecheck can settle it for good.
		fmt.Fprintln(out, "Where a tag set aside is an old one spelled oddly rather than a release, a livecheck.regex that skips it keeps it out of later looks.")
	}
	// Exit 3, attention: a script can't take an incomplete look for a
	// whole one. What's said is above, so the exit says nothing more.
	if unknown > 0 {
		return &term.ExitError{Code: term.ExitAttention}
	}
	return nil
}

// outdatedRow is a port's row in outdated's groups: its version now, the
// newest found, and what dockhand can do.
type outdatedRow struct {
	port, now, newest, note string
}

// writeGroup is a group's heading, with its count, and its rows, the
// versions as now → newest; none where it's empty.
func writeGroup(out io.Writer, heading string, rows []outdatedRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(out, "%s (%d)\n", heading, len(rows))
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(table, "  %s\t%s → %s\t%s\n", row.port, row.now, row.newest, row.note)
	}
	table.Flush()
}

func orDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// noneChecked is the error of a report none of whose ports could be
// checked, as offline, with the first one's reason; nil where any was.
// Saying none has a newer release would be a guess (the M1's quick stage,
// D-N1 and B3, 2026-10-03).
func noneChecked(report engine.OutdatedReport) error {
	if len(report.Ports) == 0 {
		return nil
	}
	for _, port := range report.Ports {
		if port.Problem == "" {
			return nil
		}
	}
	return fmt.Errorf("no port could be checked, at master %s: %s", report.Master.Short(), report.Ports[0].Reason())
}

// allowLook asks before a look whose estimate passes what's left of the
// hour's GitHub allowance, as GitHub says it, which other sessions sharing
// the login spend too: on a terminal, and refused without one unless
// --over-allowance says to go on. outdated over ryandesign's 2,133 ports
// and subports said ~5,500 calls of 5,000 and went on, and 106 were looked
// up after ten minutes (field testing at 6dd7745c). Where GitHub can't be
// asked, the look goes on, as it did.
func allowLook(allowed func(context.Context) (github.Allowance, error), streams term.Streams, over bool) func(context.Context, int) error {
	return func(ctx context.Context, lookups int) error {
		if over {
			return nil
		}
		estimate := lookCost(lookups)
		allowance, err := allowed(ctx)
		if err != nil || estimate <= allowance.Remaining {
			return nil
		}
		words := fmt.Sprintf("Looking up %s ports asks GitHub about %s times, and the login has %s of its %s left this hour, until %s, which other sessions sharing it spend too",
			prose.Count(lookups), prose.Count(estimate), prose.Count(allowance.Remaining), prose.Count(allowance.Limit), allowance.Reset.Local().Format("15:04"))
		if !streams.Terminal() {
			return fmt.Errorf("%s; --over-allowance looks anyway", words)
		}
		yes, err := streams.AskYes(words+".\n? Look anyway? [y/N] ", false)
		if err != nil {
			return err
		}
		if !yes {
			return errors.New("nothing was looked up")
		}
		return nil
	}
}

// lookCost is about how many requests looking up lookups ports asks of
// GitHub, at about 2.6 a port, rounded to a hundred (costlyLook).
func lookCost(lookups int) int { return (lookups*26/10 + 50) / 100 * 100 }

// outdatedBatch is your ports with newer releases, as outdated --mine
// finds them, split one branch a port: the updates ship --mine and update
// --mine make. What it leaves is said, each port the plan skipped with
// why, and the rest counted by what the look found of them.
func Batch(ctx context.Context, s *settings.Settings, e *engine.Engine, streams term.Streams) (engine.OutdatedPlan, error) {
	request, looked, err := outdatedRequest(ctx, s, e, streams, nil, true)
	if err != nil {
		return engine.OutdatedPlan{}, err
	}
	report, err := e.Outdated(ctx, request)
	looked.stop()
	if err != nil {
		if looked.cutShort(ctx, report) {
			fmt.Fprintf(streams.Out, "Interrupted after looking up %d of %d ports, so nothing was started; what those found:\n", len(report.Ports), looked.total.Load())
			_ = writeOutdated(context.WithoutCancel(ctx), e, streams.Out, report, false)
		}
		return engine.OutdatedPlan{}, err
	}
	if err := noneChecked(report); err != nil {
		return engine.OutdatedPlan{}, err
	}
	plan, err := e.PlanOutdated(ctx, report)
	if err != nil {
		return engine.OutdatedPlan{}, err
	}
	skipped := []view.SkippedJSON{}
	for _, skip := range plan.Skipped {
		fmt.Fprintf(streams.Out, "  %s: left as it is: %s\n", skip.Port, skip.Reason)
		skipped = append(skipped, view.SkippedJSON{Port: skip.Port, Reason: skip.Reason})
	}
	streams.Emit(skipped)
	if line := restWords(report, plan); line != "" {
		fmt.Fprintln(streams.Out, line)
	}
	return plan, nil
}

// restWords counts the ports a look found no update to make of, and the
// plan didn't skip by name, by what it found, naming outdated, which says
// each.
func restWords(report engine.OutdatedReport, plan engine.OutdatedPlan) string {
	summary := report.Summary()
	said := map[string]bool{}
	for _, skip := range plan.Skipped {
		said[skip.Port] = true
	}
	for _, port := range report.Ports {
		if said[port.Port] {
			summary[port.Class()]--
		}
	}
	var words []string
	for _, class := range []struct {
		class engine.OutdatedClass
		words string
	}{
		{engine.OutdatedCurrent, "%d current"},
		{engine.OutdatedUnpublished, "%d with a newer release not published yet"},
		{engine.OutdatedUncertain, "%d that may have one, for your look"},
		{engine.OutdatedAhead, "%d ahead of every version upstream lists, for your look"},
		{engine.OutdatedMoved, "%d pinned to a commit its branch has moved past, for update"},
		{engine.OutdatedUnchecked, "%d that couldn't be checked"},
	} {
		if n := summary[class.class]; n > 0 {
			words = append(words, fmt.Sprintf(class.words, n))
		}
	}
	if len(words) == 0 {
		return ""
	}
	return "Of the rest, " + strings.Join(words, ", ") + " (dockhand outdated --mine --current says each)."
}

// lookups follows outdated's lookups: on a terminal it redraws their count
// in place, on one line it clears when they all are, since a large --mine
// takes minutes; elsewhere it says the count as a line, once the look has
// run a while and then now and then, where it said nothing and a look of
// two ports was killed after 13 minutes, its wait unsaid (field testing at
// c9ad2112). It remembers how many there are, for a look cut short.
type lookups struct {
	line  *term.StatusLine
	draw  bool
	total atomic.Int64
	// costly says once what a large look spends of GitHub's allowance.
	costly sync.Once
	say    func(string)
	// Off a terminal, the count is said once the look has run first, and
	// again each every after that.
	first, every time.Duration
	mu           sync.Mutex
	started      time.Time
	said         time.Time
}

func newLookups(streams term.Streams) *lookups {
	return &lookups{line: streams.Status(), draw: streams.ErrTerminal(), say: streams.Say, first: 10 * time.Second, every: 30 * time.Second, started: time.Now()}
}

// costlyLook is the number of ports past which a look says what it spends
// of GitHub's hourly allowance, at about 2.6 requests a port, as measured
// over a maintainer's 835 (the M1's run at 1da4fdbf: 2,166 requests).
const costlyLook = 300

// progress is outdated's callback as each lookup finishes.
func (l *lookups) progress(done, total int) {
	l.total.Store(int64(total))
	if total >= costlyLook {
		l.costly.Do(func() {
			l.say(fmt.Sprintf("Looking up %s ports asks GitHub about %s times, of the 5,000 an hour a login has.", prose.Count(total), prose.Count(lookCost(total))))
		})
	}
	if !l.draw {
		l.mu.Lock()
		defer l.mu.Unlock()
		now := time.Now()
		if l.every > 0 && done < total && now.Sub(l.started) >= l.first && now.Sub(l.said) >= l.every {
			l.said = now
			l.say(fmt.Sprintf("Looked up %d of %d ports' newest releases", done, total))
		}
		return
	}
	if done == total {
		l.line.Clear()
		return
	}
	l.line.Show(fmt.Sprintf("Looking up each port's newest release: %d of %d", done, total))
}

// stop clears the count, which a look cut short leaves drawn.
func (l *lookups) stop() {
	if l.draw {
		l.line.Clear()
	}
}

// cutShort reports a look interrupted after finding something, which is
// worth printing: what was looked up is still true.
func (l *lookups) cutShort(ctx context.Context, report engine.OutdatedReport) bool {
	return ctx.Err() != nil && len(report.Ports) > 0
}
