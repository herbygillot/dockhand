// Package console is dockhand console: the branches in a table the
// terminal's size, kept current as serve works, a pane of the journal's
// events or a branch's detail under it, and keys that run logs, check,
// tidy, and submit on the row selected.
package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/term/screen"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var all, plain bool
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Show a live branch table and act on the selected branch",
		Long: `Shows a live table sized to your terminal. Branches needing attention
appear first, followed by work in progress and branches waiting for
reviewers. Each row shows the branch, ports, state, pull request, and next
command. The header shows branch counts; the pane below shows recent
events. The table updates as work progresses.

  ↑ ↓         select a row       PgUp PgDn Home End   page through them
  Enter       toggle the selected branch's details in the pane
  l           logs of its latest check       c   check it
  t           tidy it                        s   submit it
  r           refresh the table              q   quit (also Esc or Ctrl-C)

l, c, t, and s leave the table and run the command on the selected branch,
with that command's own questions. Ctrl-C stops the command and returns
control to the console. Press any key to return to the table.

With --plain, or when input or output is not a terminal, prints status once
and then each event on its own line, for scrollback, SSH, and screen readers.
--all includes merged, closed, and archived branches.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := s.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if plain || !streams.Terminal() || !streams.OutTerminal() {
				return watchPlain(cmd.Context(), e, streams.Plain(), all, s.Host.Timings, s.Command)
			}
			return live(cmd.Context(), e, streams, all, s.Host.Timings, s.Command)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include merged, closed, and archived branches")
	cmd.Flags().BoolVar(&plain, "plain", false, "print status once and then each event as a line, not a table that redraws")
	return cmd
}

// runner runs a command line in the console's own invocation (RunWith's
// nested runner, C8-1): its environment, directory, tree, database, and
// Git.
type runner func(context.Context, []string, term.Streams) error

// watchPlain prints status once, as status prints it, then each event as
// it is journaled, until it's stopped.
func watchPlain(ctx context.Context, e *engine.Engine, streams term.Streams, all bool, timings host.Timings, command runner) error {
	ctx, _, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	cursor := &journalCursor{e: e}
	if err := cursor.start(ctx); err != nil {
		return err
	}
	args := []string{"status"}
	if all {
		args = append(args, "--all")
	}
	if err := command(ctx, args, streams); err != nil {
		return err
	}
	fmt.Fprintln(streams.Out, "\nWatching for changes. Press Ctrl-C to stop watching.")
	names := newEventNames(e)
	for {
		select {
		case <-ctx.Done():
			// Being stopped is how it ends.
			return nil
		case <-time.After(timings.WatchPoll):
		}
		events, err := cursor.next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, event := range events {
			fmt.Fprintln(streams.Out, names.line(ctx, event))
		}
	}
}

// redrawAll is how often the table is gathered again with nothing new, so
// its ages stay true, and redrawProgress how often at most for a check's
// progress alone: gathering every branch at each second of every check
// was SQL-O6.
const (
	redrawAll      = 30 * time.Second
	redrawProgress = 5 * time.Second
)

// returnPrompt is said after a command the console ran.
const returnPrompt = "Press any key to return to the console. "

// console is the live table's state beside its model: the engine, the
// session it observes as, and how it runs a command.
type console struct {
	e       *engine.Engine
	session *coord.Session
	streams term.Streams
	all     bool
	command runner
	model   Model
	// gathered is when the rows were last gathered.
	gathered time.Time
}

// live draws the table on the terminal until q, Esc, or Ctrl-C. Its
// context is the command line's but for an interrupt while a command it
// ran runs, which stops that command alone: the command line's context
// ends at the first, for good.
func live(parent context.Context, e *engine.Engine, streams term.Streams, all bool, timings host.Timings, command runner) error {
	ctx, quit := context.WithCancel(context.WithoutCancel(parent))
	defer quit()
	ctx, session, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	cursor := &journalCursor{e: e}
	if err := cursor.start(ctx); err != nil {
		return err
	}
	s, err := screen.Open(streams)
	if err != nil {
		return err
	}
	defer s.Close()
	c := &console{e: e, session: session, streams: streams, all: all, command: command, model: Model{All: all}}
	names := newEventNames(e)
	draw := func() error {
		c.model.Width, c.model.Height = s.Size()
		c.model.At = time.Now()
		return s.Draw(Frame(c.model))
	}
	c.gather(ctx)
	if err := draw(); err != nil {
		return err
	}
	ended := parent.Done()
	poll := time.NewTicker(timings.WatchPoll)
	defer poll.Stop()
	progressed := false
	for {
		select {
		case <-ended:
			return nil
		case <-signals:
			return nil
		case key, open := <-s.Keys():
			if !open {
				return nil
			}
			switch {
			case key.Code == screen.Escape || key.Code == screen.Interrupt || key == screen.Key{Code: screen.Rune, Rune: 'q'}:
				return nil
			case key.Code == screen.Enter:
				c.model.Pane = map[pane]pane{paneEvents: paneDetail, paneDetail: paneEvents}[c.model.Pane]
				c.detail(ctx)
			case key == screen.Key{Code: screen.Rune, Rune: 'r'}:
				c.gather(ctx)
			case key.Code == screen.Rune:
				args, ok := c.verb(ctx, key.Rune)
				if !ok {
					continue
				}
				interrupted, terminated := false, false
				err := s.Suspend(ctx, func() { interrupted, terminated = c.run(ctx, args, signals) }, returnPrompt)
				if err != nil {
					return err
				}
				if terminated {
					return nil
				}
				// An interrupt that stopped the command ended the command
				// line's context too, which the console outlives.
				if interrupted {
					ended = nil
				}
				c.gather(ctx)
			default:
				if c.model.Move(key) {
					c.detail(ctx)
				}
			}
		case <-poll.C:
			events, err := cursor.next(ctx)
			if err != nil {
				c.model.Problem = "events: " + err.Error()
			}
			for _, event := range events {
				c.model.Heard(names.line(ctx, event))
			}
			changed := slices.ContainsFunc(events, func(event model.Event) bool { return event.Kind.ChangesStatus() })
			// Progress waits for its interval, and isn't lost meanwhile.
			progressed = progressed || slices.ContainsFunc(events, func(event model.Event) bool {
				return event.Kind == model.EventProgress || event.Kind == model.EventWaiting
			})
			if since := time.Since(c.gathered); changed || progressed && since >= redrawProgress || since >= redrawAll {
				c.gather(ctx)
				progressed = false
			}
		}
		if err := draw(); err != nil {
			return err
		}
	}
}

// gather reads every branch's status in one report, the rows from it, and
// the selected branch's detail where the pane shows it.
func (c *console) gather(ctx context.Context) {
	c.gathered = time.Now()
	states := []model.BranchState{model.BranchOpen}
	if c.all {
		states = []model.BranchState{model.BranchOpen, model.BranchMerged, model.BranchClosed, model.BranchArchived}
	}
	report, err := c.e.StatusReport(ctx, c.session, engine.StatusRequest{States: states, Overview: true})
	if err != nil {
		c.model.Problem = "status: " + err.Error()
		return
	}
	c.model.Problem = ""
	var rows []view.Row
	for _, status := range report.Statuses {
		rows = append(rows, view.BranchRow(status))
	}
	c.model.Gathered(rows)
	c.model.Serve = view.ServeWords(report.Serve)
	c.detail(ctx)
}

// detail gathers the selected branch's detail, where the pane shows it.
func (c *console) detail(ctx context.Context) {
	row, ok := c.model.Row()
	if c.model.Pane != paneDetail || !ok {
		c.model.Detail = nil
		return
	}
	branch, err := c.e.Branch(ctx, row.ID)
	if err != nil {
		c.model.Detail = []string{view.Look + " " + err.Error()}
		return
	}
	report, err := c.e.StatusReport(ctx, c.session, engine.StatusRequest{Branches: []model.Branch{branch}})
	if err != nil {
		c.model.Detail = []string{view.Look + " " + err.Error()}
		return
	}
	c.model.Detail = detailLines(report.Statuses[0], report)
}

// detailLines are a branch in detail, as the pane shows it: its row's
// words, what needs you of it, what status details of it (view.BranchDetail),
// and the command that moves it on.
func detailLines(status engine.BranchStatus, report engine.StatusReport) []string {
	row := view.BranchRow(status)
	lines := []string{fmt.Sprintf("%s · %s · PR %s", row.Ports, row.State, row.PR)}
	for _, attention := range view.AttentionFor(status) {
		lines = append(lines, attention.Kind.Mark()+" "+attention.What)
	}
	detail := view.BranchDetail(status, report)
	for _, decided := range detail.Decided {
		lines = append(lines, "Decided  "+decided)
	}
	for _, line := range detail.Instructions {
		lines = append(lines, "Comments "+view.Look+" "+line)
	}
	for _, line := range detail.Lint {
		lines = append(lines, "Lint     "+line.Mark+" "+line.Words)
	}
	for _, line := range detail.Links {
		lines = append(lines, "Links    "+line.Mark+" "+line.Words)
	}
	for _, line := range detail.Installed {
		lines = append(lines, "Against  "+line.Mark+" "+line.Words)
	}
	for _, line := range detail.Merge {
		lines = append(lines, "Merge    "+line)
	}
	if row.Next != "" {
		lines = append(lines, "Next: "+row.Next)
	}
	return lines
}

// verb is the command line a key runs on the selected row: logs of its
// latest check, check, tidy, or submit, by --branch; none for any other
// key, or with no row.
func (c *console) verb(ctx context.Context, key rune) ([]string, bool) {
	row, ok := c.model.Row()
	if !ok {
		return nil, false
	}
	switch key {
	case 'c':
		return []string{"check", "--branch", row.Name}, true
	case 't':
		return []string{"tidy", "--branch", row.Name}, true
	case 's':
		return []string{"submit", "--branch", row.Name}, true
	case 'l':
		latest, found, err := c.e.LatestCheck(ctx, row.ID)
		switch {
		case err != nil:
			c.model.Problem = "logs: " + err.Error()
		case !found:
			c.model.Problem = row.Name + " has no check yet; c checks it"
		default:
			return []string{"logs", latest.Name()}, true
		}
	}
	return nil, false
}

// run runs a command line, said first as the person would type it, on the
// terminal as it was: an interrupt meanwhile stops the command and is
// reported, as is a termination, which ends the console too.
func (c *console) run(ctx context.Context, args []string, signals <-chan os.Signal) (interrupted, terminated bool) {
	fmt.Fprintf(c.streams.Out, "$ dockhand %s\n", strings.Join(args, " "))
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	done, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case sig := <-signals:
			interrupted, terminated = sig == os.Interrupt, sig != os.Interrupt
			stop()
		case <-done:
		}
	}()
	err := c.command(ctx, args, c.streams)
	close(done)
	<-watched
	if err != nil && !(interrupted && errors.Is(err, context.Canceled)) {
		fmt.Fprintf(c.streams.Err, "dockhand: %v\n", err)
	}
	return interrupted, terminated
}
