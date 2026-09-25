package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// watchPoll is how often watch looks at the journal.
var watchPoll = time.Second

// watchRedraw is how often the live view is redrawn with nothing new, so
// its ages stay true.
var watchRedraw = 30 * time.Second

func watchCommand(s *settings, streams Streams) *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Show status live, as serve works",
		Long: `Shows status and keeps it current: the view is redrawn whenever something
is journaled, such as a check's progress, a result, or a pull request's
review. It only observes; serve does the work, and opening it starts
nothing.

On a terminal, a line of input runs a command on a branch with that
command's own confirmations, then returns to the view:
  c <branch>  check       l <branch>  logs of its latest check
  t <branch>  tidy        s <branch>  submit
Enter redraws, and q quits. Inside a branch's worktree the branch may be
left out.

--plain, and any output that is not a terminal, prints status once and then
each event as a line, for scrollback, SSH sessions, and screen readers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if plain || !streams.terminal() || !outputTerminal(streams) {
				err = watchPlain(ctx, e, streams)
			} else {
				err = watchLive(ctx, e, streams)
			}
			// Being stopped is how watch ends.
			if ctx.Err() != nil && (err == nil || errors.Is(err, ctx.Err())) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print events as lines, not a view that redraws")
	return cmd
}

// outputTerminal reports whether the output is a terminal that can be
// redrawn.
func outputTerminal(streams Streams) bool {
	if streams.interactive {
		return true
	}
	file, ok := streams.Out.(*os.File)
	return ok && isTerminal(file.Fd())
}

// journalCursor reads the journal's events past the last one it saw, at
// info level: sessions' own comings and goings are not news.
type journalCursor struct {
	e    *engine.Engine
	last int64
}

func (c *journalCursor) next(ctx context.Context) ([]model.Event, error) {
	events, err := c.e.Events(ctx, c.last)
	var news []model.Event
	for _, event := range events {
		c.last = event.Sequence
		if event.Level == model.LevelInfo {
			news = append(news, event)
		}
	}
	return news, err
}

// watchPlain prints status once, then each event as it is journaled.
func watchPlain(ctx context.Context, e *engine.Engine, streams Streams) error {
	cursor := &journalCursor{e: e}
	if _, err := cursor.next(ctx); err != nil {
		return err
	}
	if err := showStatus(ctx, e, streams, nil, false, false, ""); err != nil {
		return err
	}
	fmt.Fprintln(streams.Out, "\nWatching; events follow as they happen. Ctrl-C stops.")
	names := &eventNames{e: e, branches: map[model.BranchID]string{}, runs: map[model.RunID]string{}}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(watchPoll):
		}
		events, err := cursor.next(ctx)
		if err != nil {
			return err
		}
		for _, event := range events {
			fmt.Fprintln(streams.Out, names.line(ctx, event))
		}
	}
}

// eventNames words an event with the names people use for its branch and
// run.
type eventNames struct {
	e        *engine.Engine
	branches map[model.BranchID]string
	runs     map[model.RunID]string
}

func (n *eventNames) line(ctx context.Context, event model.Event) string {
	parts := []string{event.At.Local().Format("15:04:05")}
	if event.Branch != "" {
		if _, ok := n.branches[event.Branch]; !ok {
			if branch, err := n.e.Branch(ctx, event.Branch); err == nil {
				n.branches[event.Branch] = branch.ShortName()
			}
		}
		if name := n.branches[event.Branch]; name != "" {
			parts = append(parts, name)
		}
	}
	if event.Run != "" {
		if _, ok := n.runs[event.Run]; !ok {
			if run, err := n.e.Run(ctx, event.Run); err == nil {
				n.runs[event.Run] = run.Name()
			}
		}
		if name := n.runs[event.Run]; name != "" && !strings.Contains(event.Message, name) {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, "  ") + "  " + event.Message
}

const (
	enterView = "\x1b[?1049h"
	leaveView = "\x1b[?1049l"
	clearView = "\x1b[H\x1b[2J"
)

// watchLive redraws status on a terminal as the journal changes, and runs
// the commands typed at it.
func watchLive(ctx context.Context, e *engine.Engine, streams Streams) error {
	fmt.Fprint(streams.Out, enterView)
	defer fmt.Fprint(streams.Out, leaveView)
	cursor := &journalCursor{e: e}
	if _, err := cursor.next(ctx); err != nil {
		return err
	}
	type input struct {
		line string
		err  error
	}
	want := make(chan struct{})
	lines := make(chan input)
	go func() {
		for range want {
			line, err := streams.lines.ReadString('\n')
			select {
			case lines <- input{strings.TrimSpace(line), err}:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(want)

	var drawn time.Time
	draw := func() {
		var view bytes.Buffer
		if err := showStatus(ctx, e, Streams{Out: &view, Err: &view}, nil, false, false, ""); err != nil {
			fmt.Fprintf(&view, "status: %v\n", err)
		}
		drawn = time.Now()
		fmt.Fprintf(streams.Out, "%sdockhand watch · %s · c check · l logs · t tidy · s submit, each <branch> · q quits\n\n%s\n> ",
			clearView, drawn.Format("15:04:05"), strings.TrimRight(view.String(), "\n"))
	}
	draw()
	want <- struct{}{}
	paused := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case in := <-lines:
			if in.err != nil && in.line == "" {
				if errors.Is(in.err, io.EOF) {
					return nil
				}
				return in.err
			}
			if paused {
				paused = false
				draw()
				want <- struct{}{}
				continue
			}
			args, quit, err := watchVerb(ctx, e, in.line)
			switch {
			case quit:
				return nil
			case err != nil:
				draw()
				fmt.Fprintf(streams.Out, "%v\n> ", err)
			case args == nil:
				draw()
			default:
				fmt.Fprintf(streams.Out, "%s$ dockhand %s\n", clearView, strings.Join(args, " "))
				if err := Run(ctx, args, streams); err != nil {
					fmt.Fprintf(streams.Err, "dockhand: %v\n", err)
				}
				fmt.Fprint(streams.Out, "\nEnter returns to the view. ")
				paused = true
			}
			want <- struct{}{}
		case <-time.After(watchPoll):
			if paused {
				continue
			}
			events, err := cursor.next(ctx)
			if err != nil {
				return err
			}
			if len(events) > 0 || time.Since(drawn) >= watchRedraw {
				draw()
			}
		}
	}
}

// watchVerb turns a line typed at the view into a command line: its verb,
// and the branch it names. Nothing typed means redraw.
func watchVerb(ctx context.Context, e *engine.Engine, line string) (args []string, quit bool, err error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, false, nil
	}
	if len(fields) > 2 {
		return nil, false, fmt.Errorf("%q: type a letter and at most one branch, such as c jq-update", line)
	}
	branch := ""
	if len(fields) == 2 {
		branch = fields[1]
	}
	with := func(verb string) []string {
		if branch == "" {
			return []string{verb}
		}
		return []string{verb, "--branch", branch}
	}
	switch fields[0] {
	case "q":
		return nil, true, nil
	case "c":
		return with("check"), false, nil
	case "t":
		return with("tidy"), false, nil
	case "s":
		return with("submit"), false, nil
	case "l":
		filter := store.RunFilter{}
		if branch != "" {
			found, err := e.Resolve(ctx, branch)
			if err != nil {
				return nil, false, err
			}
			filter.Branch = found.ID
		} else if current, err := e.Current(ctx); err == nil {
			filter.Branch = current.ID
		}
		runs, err := e.Runs(ctx, filter)
		if err != nil {
			return nil, false, err
		}
		if len(runs) == 0 {
			return nil, false, errors.New("no checks to show the logs of")
		}
		return []string{"logs", runs[0].Name()}, false, nil
	}
	return nil, false, fmt.Errorf("%q: c, l, t, or s and a branch; Enter redraws; q quits", fields[0])
}
