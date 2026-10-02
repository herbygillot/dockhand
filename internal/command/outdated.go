package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// outdatedOptions are update's --outdated, --mine, --check, and --yes.
type outdatedOptions struct {
	outdated, mine, check, yes bool
	// plan shows how the work splits and starts nothing.
	plan bool
}

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
the rest; --all lists every port, with why any couldn't be checked.

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
	cmd.Flags().BoolVar(&all, "all", false, "list current ports and those that couldn't be checked too")
	return cmd
}

func writeOutdated(ctx context.Context, e *engine.Engine, out io.Writer, report engine.OutdatedReport, all bool) error {
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "  PORT\tNOW\tNEWEST\tDOCKHAND CAN")
	newer, unknown, uncertain, moved, own := 0, 0, 0, 0, 0
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
		switch {
		case port.OwnVersion:
			own++
			if all {
				fmt.Fprintf(table, "  %s\t%s\t—\tnothing; it fetches nothing here, and no livecheck reads its version\n", port.Port, orDash(port.Current))
			}
		case port.Moved != nil:
			// A port pinned to a commit of a branch that has moved on is
			// behind, and the version to give the commit is a person's.
			moved++
			fmt.Fprintf(table, "  %s\t%s\t%s?\tedit %s by hand: its %s names a newer commit than the one it pins\n", port.Port, port.Current, port.Newest, port.Port, port.Moved.Branch)
		case port.Problem != "":
			unknown++
			if all {
				fmt.Fprintf(table, "  %s\t%s\t?\tcouldn't check: %s\n", port.Port, orDash(port.Current), port.Problem)
			}
		case len(port.Uncertain) > 0:
			// Neither current nor outdated for sure, so it's listed for
			// a look, with the update that takes it.
			uncertain++
			fmt.Fprintf(table, "  %s\t%s\t%s?\tupdate %s %s after a look: %s\n", port.Port, port.Current, port.Newest, port.Port, port.Uncertain[0].Source, setAsideWords(port.Uncertain))
		case port.Outdated && folded:
			newer++
		case port.Outdated:
			newer++
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
			fmt.Fprintf(table, "  %s\t%s\t%s\t%s%s\n", port.Port, port.Current, port.Newest, can, with(port.Port))
		case folded:
		case all && port.With != "":
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing; it is current, as %s is\n", port.Port, port.Current, port.Newest, port.With)
		case all:
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing; it is current%s\n", port.Port, port.Current, port.Newest, with(port.Port))
		}
	}
	if newer > 0 || uncertain > 0 || moved > 0 || all {
		table.Flush()
	}
	ports, master := prose.Plural(len(report.Ports), "port"), engine.Short(report.Master)
	var line string
	alone := len(report.Ports) == 1 && newer == 0 && unknown == 0
	switch {
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
	if unknown > 0 {
		line += fmt.Sprintf(" · %d couldn't be checked", unknown)
		if !all {
			line += " (--all says why)"
		}
	}
	fmt.Fprintln(out, line)
	if uncertain > 0 {
		// Serve says such a port once, but outdated every time it's asked:
		// the port's own livecheck can settle it for good.
		fmt.Fprintln(out, "Where a tag set aside is an old one spelled oddly rather than a release, a livecheck.regex that skips it keeps it out of later looks.")
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

// updateOutdated is update --outdated: it shows how it splits the work,
// one branch per port, then prepares each.
func updateOutdated(ctx context.Context, s *settings, streams Streams, args []string, options outdatedOptions) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	request, looked, err := outdatedRequest(ctx, s, e, streams, args, options.mine)
	if err != nil {
		return err
	}
	var environments []model.Environment
	if options.check {
		if environments, err = e.Environments(ctx, s.file.Check.On); err != nil {
			return err
		}
	}
	report, err := e.Outdated(ctx, request)
	looked.stop()
	if err != nil {
		if looked.cutShort(ctx, report) {
			fmt.Fprintf(streams.Out, "Interrupted after looking up %d of %d ports, so nothing was started; what those found:\n", len(report.Ports), looked.total.Load())
			_ = writeOutdated(context.WithoutCancel(ctx), e, streams.Out, report, false)
		}
		return err
	}
	plan, err := e.PlanOutdated(ctx, report)
	if err != nil {
		return err
	}
	out := streams.Out
	if len(plan.Updates) == 0 {
		fmt.Fprintln(out, "Nothing to update: none of them has a newer release that isn't already in a branch.")
		writeSkipped(out, plan, report)
		return nil
	}
	var names []string
	for _, update := range plan.Updates {
		names = append(names, engine.BranchName(update.Name))
	}
	fmt.Fprintf(out, "Will start %s, one per port (unrelated ports go in separate PRs):\n  %s\n", prose.Plural(len(plan.Updates), "branch"), strings.Join(names, " · "))
	writeSkipped(out, plan, report)
	if options.plan {
		fmt.Fprintln(out, "Nothing was started (--plan).")
		return nil
	}
	if !options.yes {
		if !streams.terminal() {
			return errors.New("nothing was started: without a terminal, --yes starts what is shown")
		}
		ok, err := confirm(streams, "? go ahead? [y/N] ")
		if err != nil || !ok {
			fmt.Fprintln(out, "Nothing was started.")
			return err
		}
	}
	prepared := e.PrepareOutdated(ctx, plan, engine.PrepareOptions{Origin: model.OriginPerson, Check: options.check, Environments: environments, Tests: model.TestPolicy(s.file.Check.Tests)})
	streams.emit(preparedView(prepared))
	return writePrepared(ctx, e, out, prepared, options.check)
}

// writeSkipped says the ports update --outdated leaves alone, and why: the
// plan's, and those whose newest release is uncertain, which a person
// names after a look.
func writeSkipped(out io.Writer, plan engine.OutdatedPlan, report engine.OutdatedReport) {
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(out, "Skipped: %s (%s)\n", skipped.Port, skipped.Reason)
	}
	for _, port := range report.Ports {
		if len(port.Uncertain) > 0 {
			fmt.Fprintf(out, "Skipped: %s (%s; after a look, dockhand update %s %s)\n", port.Port, setAsideWords(port.Uncertain), port.Port, port.Uncertain[0].Source)
		}
	}
}

func writePrepared(ctx context.Context, e *engine.Engine, out io.Writer, prepared []engine.PreparedUpdate, check bool) error {
	tidied, queued, failed := 0, 0, 0
	for _, done := range prepared {
		name := done.Planned.Name
		switch {
		case done.Problem != "":
			failed++
			fmt.Fprintf(out, "  ✗ %s: %s\n", name, done.Problem)
		default:
			tidied++
			line := fmt.Sprintf("  ✓ %s: %s → %s, one commit", name, done.Update.Before, done.Update.After)
			if done.Run != nil {
				queued++
				line += ", " + done.Run.Name() + " queued"
			}
			fmt.Fprintln(out, line)
		}
		if done.Update.Upstream != nil {
			for _, change := range done.Update.Upstream.Changes {
				fmt.Fprintf(out, "      %s\n", upstreamWords(change))
			}
		}
	}
	summary := fmt.Sprintf("%s updated and tidied into one commit each", prose.Plural(tidied, "branch"))
	if check {
		summary += fmt.Sprintf("; %s queued", prose.Plural(queued, "check"))
	}
	if failed > 0 {
		summary += fmt.Sprintf("; %d need a look", failed)
	}
	fmt.Fprintln(out, summary)
	if check && queued > 0 {
		// Whether serve runs is only a hint here, after the work is done.
		if serve, err := readServe(ctx, e); err == nil && !serve.Running {
			fmt.Fprintln(out, "serve isn't running: dockhand serve, or dockhand wait to run them here")
		}
	}
	if failed > 0 {
		return &ExitError{Code: 3}
	}
	return nil
}

// lookupProgress shows, on a terminal, how many ports' newest releases are
// looked up, on one line it redraws and clears when they all are: a large
// --mine takes minutes. Elsewhere, and for --json, it shows nothing.
// lookups follows outdated's lookups: on a terminal it redraws their count
// in place, and it remembers how many there are, for a look cut short.
type lookups struct {
	line  *statusLine
	draw  bool
	total atomic.Int64
}

func newLookups(streams Streams) *lookups {
	return &lookups{line: streams.stderrLine(), draw: streams.errTerminal()}
}

// progress is outdated's callback as each lookup finishes.
func (l *lookups) progress(done, total int) {
	l.total.Store(int64(total))
	if !l.draw {
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
