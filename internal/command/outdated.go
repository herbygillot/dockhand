package command

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

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
)

// outdatedRequest is what --mine and the ports named choose.
func outdatedRequest(ctx context.Context, s *settings, e *engine.Engine, streams Streams, ports []string, mine bool) (engine.OutdatedRequest, *lookups, error) {
	looked := newLookups(streams)
	request := engine.OutdatedRequest{Ports: ports, Progress: looked.progress}
	if !mine {
		if len(ports) == 0 {
			return request, looked, errors.New("name ports, or choose yours with --mine")
		}
		return request, looked, nil
	}
	if len(ports) > 0 {
		return request, looked, errors.New("--mine chooses your ports; name ports or use --mine, not both")
	}
	if request.Maintainers = s.file.Maintainers(); len(request.Maintainers) == 0 {
		// The person's own line, as master's ports write their GitHub
		// login, where it can be found, as serve suggests it.
		return request, looked, fmt.Errorf("--mine needs to know who you are, as your ports' maintainers lines name you: %s", e.MaintainerHint(ctx))
	}
	return request, looked, nil
}

func outdatedCommand(s *settings, streams Streams) *cobra.Command {
	var mine, all bool
	cmd := &cobra.Command{
		Use:   "outdated [port...]",
		Short: "Show which ports have newer releases upstream",
		Long: `Looks up the newest release of each port named, or with --mine each port
whose maintainers line names you (the config's maintainer), at MacPorts'
master as fetched now. It lists the ports with newer releases, and counts
the rest; --current lists every port, with why any couldn't be checked.

update --outdated --mine starts on them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
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
					streams.emit(outdatedView(report))
					fmt.Fprintf(streams.Out, "Interrupted after looking up %d of %d ports; what those found:\n", len(report.Ports), looked.total.Load())
					_ = writeOutdated(context.WithoutCancel(ctx), e, streams.Out, report, all)
				}
				return err
			}
			streams.emit(outdatedView(report))
			return writeOutdated(ctx, e, streams.Out, report, all)
		},
	}
	cmd.Flags().BoolVar(&mine, "mine", false, "the ports whose maintainers line names you (config maintainer)")
	cmd.Flags().BoolVar(&all, "current", false, "list current ports and those that couldn't be checked too")
	return cmd
}

func writeOutdated(ctx context.Context, e *engine.Engine, out io.Writer, report engine.OutdatedReport, all bool) error {
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "  PORT\tNOW\tNEWEST\tDOCKHAND CAN")
	summary := report.Summary()
	newer, unknown, uncertain, moved, own := summary[engine.OutdatedNewer], summary[engine.OutdatedUnchecked], summary[engine.OutdatedUncertain], summary[engine.OutdatedMoved], summary[engine.OutdatedOwnVersion]
	unready := summary[engine.OutdatedUnpublished]
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
	for _, port := range report.Ports {
		folded := port.With != "" && listed[port.With]
		class := port.Class()
		switch {
		case class == engine.OutdatedOwnVersion:
			if all {
				fmt.Fprintf(table, "  %s\t%s\t—\tnothing; it fetches nothing here, and no livecheck reads its version\n", port.Port, orDash(port.Current))
			}
		case class == engine.OutdatedMoved:
			// A port pinned to a commit of a branch that has moved on is
			// behind, and the version to give the commit is a person's.
			fmt.Fprintf(table, "  %s\t%s\t%s?\tedit %s by hand: its %s names a newer commit than the one it pins\n", port.Port, port.Current, port.Newest, port.Port, port.Moved.Branch)
		case class == engine.OutdatedUnchecked:
			if all || none {
				fmt.Fprintf(table, "  %s\t%s\t?\tcouldn't check: %s\n", port.Port, orDash(port.Current), strings.ReplaceAll(port.Problem, "\n", "; "))
			}
		case class == engine.OutdatedUncertain:
			// Neither current nor outdated for sure, so it's listed for
			// a look, with the update that takes it.
			fmt.Fprintf(table, "  %s\t%s\t%s?\tupdate %s %s after a look: %s\n", port.Port, port.Current, port.Newest, port.Port, port.Uncertain[0].Source, setAsideWords(port.Uncertain))
		case class == engine.OutdatedUnpublished:
			// Newer, but update would refuse it until its release has
			// its files (field testing at 43949b89, browsh).
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing yet: %s\n", port.Port, port.Current, port.Newest, port.Unpublished)
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
			// A major version bump and serve would hold (holds.majors).
			if e.HoldsMajor(port.Current, port.Newest) {
				can += "; a new major version, held for a look unattended"
			}
			fmt.Fprintf(table, "  %s\t%s\t%s\t%s%s\n", port.Port, port.Current, port.Newest, can, with(port.Port))
		case folded:
		case all && port.With != "":
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing; it is current, as %s is\n", port.Port, port.Current, port.Newest, port.With)
		case all:
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing; it is current%s\n", port.Port, port.Current, port.Newest, with(port.Port))
		}
	}
	if newer > 0 || uncertain > 0 || moved > 0 || unready > 0 || all || none {
		table.Flush()
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
	if uncertain > 0 {
		// Serve says such a port once, but outdated every time it's asked:
		// the port's own livecheck can settle it for good.
		fmt.Fprintln(out, "Where a tag set aside is an old one spelled oddly rather than a release, a livecheck.regex that skips it keeps it out of later looks.")
	}
	// Exit 3, attention: a script can't take an incomplete look for a
	// whole one. What's said is above, so the exit says nothing more.
	if unknown > 0 {
		return &ExitError{Code: ExitAttention}
	}
	return nil
}

// setAsideWords says why a port's newest release is uncertain: what was set
// aside compares newer than the port's version, but was tagged on a commit
// older than the port's own tag's.
func setAsideWords(aside []engine.SetAside) string {
	if len(aside) == 1 {
		return fmt.Sprintf("%s compares newer, but its commit is older than %s's", aside[0].Tag, aside[0].Predates)
	}
	var tags []string
	for _, version := range aside {
		tags = append(tags, version.Tag)
	}
	return fmt.Sprintf("%s compare newer, but their commits are older than %s's", strings.Join(tags, ", "), aside[0].Predates)
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
	problem, _, _ := strings.Cut(report.Ports[0].Problem, "\n")
	return fmt.Errorf("no port could be checked, at master %s: %s", report.Master.Short(), problem)
}

// outdatedBatch is your ports with newer releases, as outdated --mine
// finds them, split one branch a port: the updates ship --mine and update
// --mine make. What it leaves is said, each port the plan skipped with
// why, and the rest counted by what the look found of them.
func outdatedBatch(ctx context.Context, s *settings, e *engine.Engine, streams Streams) (engine.OutdatedPlan, error) {
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
	skipped := []skippedJSON{}
	for _, skip := range plan.Skipped {
		fmt.Fprintf(streams.Out, "  %s: left as it is: %s\n", skip.Port, skip.Reason)
		skipped = append(skipped, skippedJSON{Port: skip.Port, Reason: skip.Reason})
	}
	streams.emit(skipped)
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
		{engine.OutdatedMoved, "%d pinned to a commit its branch has moved past, to edit by hand"},
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
	line  *statusLine
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

func newLookups(streams Streams) *lookups {
	return &lookups{line: streams.stderrLine(), draw: streams.errTerminal(), say: streams.status.say, first: 10 * time.Second, every: 30 * time.Second, started: time.Now()}
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
			l.say(fmt.Sprintf("Looking up %s ports asks GitHub about %s times, of the 5,000 an hour a login has.", prose.Count(total), prose.Count((total*26/10+50)/100*100)))
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
		l.line.clear()
		return
	}
	l.line.show(fmt.Sprintf("Looking up each port's newest release: %d of %d", done, total))
}

// stop clears the count, which a look cut short leaves drawn.
func (l *lookups) stop() {
	if l.draw {
		l.line.clear()
	}
}

// cutShort reports a look interrupted after finding something, which is
// worth printing: what was looked up is still true.
func (l *lookups) cutShort(ctx context.Context, report engine.OutdatedReport) bool {
	return ctx.Err() != nil && len(report.Ports) > 0
}
