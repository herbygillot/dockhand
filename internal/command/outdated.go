package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// outdatedOptions are update's --outdated, --mine, --check, and --yes.
type outdatedOptions struct {
	outdated, mine, check, yes bool
}

// outdatedRequest is what --mine and the ports named choose.
func outdatedRequest(s *settings, ports []string, mine bool) (engine.OutdatedRequest, error) {
	request := engine.OutdatedRequest{Ports: ports}
	if !mine {
		if len(ports) == 0 {
			return request, errors.New("name ports, or choose yours with --mine")
		}
		return request, nil
	}
	if len(ports) > 0 {
		return request, errors.New("--mine chooses your ports; name ports or use --mine, not both")
	}
	if request.Maintainers = s.file.Maintainers(); len(request.Maintainers) == 0 {
		return request, errors.New(`--mine needs to know who you are: set maintainer = "{@you example.org:you}" in ~/.dockhand/config.toml, as your ports' maintainers lines name you`)
	}
	return request, nil
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
			request, err := outdatedRequest(s, args, mine)
			if err != nil {
				return err
			}
			report, err := e.Outdated(ctx, request)
			if err != nil {
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
	newer, unknown := 0, 0
	for _, port := range report.Ports {
		switch {
		case port.Problem != "":
			unknown++
			if all {
				fmt.Fprintf(table, "  %s\t%s\t?\tcouldn't check: %s\n", port.Port, orDash(port.Current), port.Problem)
			}
		case port.Outdated:
			newer++
			can := "update"
			open, err := e.BranchesChanging(ctx, port.Port)
			if err != nil {
				return err
			}
			if len(open) > 0 {
				can = "already in " + open[0].ShortName()
			}
			fmt.Fprintf(table, "  %s\t%s\t%s\t%s\n", port.Port, port.Current, port.Newest, can)
		case all:
			fmt.Fprintf(table, "  %s\t%s\t%s\tnothing; it is current\n", port.Port, port.Current, port.Newest)
		}
	}
	if newer > 0 || all {
		table.Flush()
	}
	line := fmt.Sprintf("%d of %s have newer releases, at master %s", newer, plural(len(report.Ports), "port"), engine.Short(report.Master))
	if unknown > 0 {
		line += fmt.Sprintf(" · %d couldn't be checked", unknown)
		if !all {
			line += " (--all says why)"
		}
	}
	fmt.Fprintln(out, line)
	return nil
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
	request, err := outdatedRequest(s, args, options.mine)
	if err != nil {
		return err
	}
	var environments []model.Environment
	if options.check {
		if environments, err = e.Environments(s.file.Check.On); err != nil {
			return err
		}
	}
	report, err := e.Outdated(ctx, request)
	if err != nil {
		return err
	}
	plan, err := e.PlanOutdated(ctx, report)
	if err != nil {
		return err
	}
	out := streams.Out
	if len(plan.Updates) == 0 {
		fmt.Fprintln(out, "Nothing to update: none of them has a newer release that isn't already in a branch.")
		writeSkipped(out, plan)
		return nil
	}
	var names []string
	for _, update := range plan.Updates {
		names = append(names, engine.BranchName(update.Name))
	}
	fmt.Fprintf(out, "Will start %s, one per port (unrelated ports go in separate PRs):\n  %s\n", plural(len(plan.Updates), "branch"), strings.Join(names, " · "))
	writeSkipped(out, plan)
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

func writeSkipped(out io.Writer, plan engine.OutdatedPlan) {
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(out, "Skipped: %s (%s)\n", skipped.Port, skipped.Reason)
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
	summary := fmt.Sprintf("%s updated and tidied into one commit each", plural(tidied, "branch"))
	if check {
		summary += fmt.Sprintf("; %s queued", plural(queued, "check"))
	}
	if failed > 0 {
		summary += fmt.Sprintf("; %d need a look", failed)
	}
	fmt.Fprintln(out, summary)
	if check && queued > 0 {
		if line := serveLine(ctx, e); strings.HasPrefix(line, "serve: not running") {
			fmt.Fprintln(out, "serve isn't running: dockhand serve, or dockhand wait to run them here")
		}
	}
	if failed > 0 {
		return &ExitError{Code: 3}
	}
	return nil
}
