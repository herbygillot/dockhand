package status

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
)

// statusWatch is status --watch: status kept current as serve works, a
// view redrawn on a terminal, else events as lines; it only observes. A
// command typed at its prompt runs through command, the whole command
// line's, which the watch is given rather than reaching back to.
func statusWatch(ctx context.Context, e *engine.Engine, streams term.Streams, plain bool, timings host.Timings, places config.Places, command func(context.Context, []string, term.Streams) error) error {
	// One observer for the whole watch, however often it redraws.
	ctx, _, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	if plain {
		streams = streams.Plain()
	}
	if plain || !streams.Terminal() || !streams.OutTerminal() {
		err = watchPlain(ctx, e, streams, timings, places)
	} else {
		err = watchLive(ctx, e, streams, timings, places, command)
	}
	// Being stopped is how a watch ends.
	if ctx.Err() != nil && (err == nil || errors.Is(err, ctx.Err())) {
		return nil
	}
	return err
}

// journalCursor reads the journal's events past the last one it saw, at
// info level: sessions' own comings and goings are not news.
type journalCursor struct {
	e    *engine.Engine
	last int64
}

// start begins at the journal's newest event: a watcher follows what
// happens from now, without reading what came before to pass it by.
func (c *journalCursor) start(ctx context.Context) error {
	last, err := c.e.LatestEvent(ctx)
	c.last = last
	return err
}

func (c *journalCursor) next(ctx context.Context) ([]model.Event, error) {
	events, err := c.e.Events(ctx, c.last, model.LevelInfo)
	if len(events) > 0 {
		c.last = events[len(events)-1].Sequence
	}
	return events, err
}

// watchPlain prints status once, then each event as it is journaled.
func watchPlain(ctx context.Context, e *engine.Engine, streams term.Streams, timings host.Timings, places config.Places) error {
	cursor := &journalCursor{e: e}
	if err := cursor.start(ctx); err != nil {
		return err
	}
	if err := ShowStatus(ctx, e, streams, nil, false, false, branchflags.BranchFlags{}, places); err != nil {
		return err
	}
	fmt.Fprintln(streams.Out, "\nWatching for changes. Press Ctrl-C to stop watching.")
	names := &eventNames{e: e, branches: map[model.BranchID]string{}, runs: map[model.RunID]string{}}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(timings.WatchPoll):
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

// watchLive redraws status on a terminal as what it shows changes, and
// runs the commands typed at it. A check's progress isn't redrawn for: it
// redrew every branch's status each second of every check (SQL-O6).
func watchLive(ctx context.Context, e *engine.Engine, streams term.Streams, timings host.Timings, places config.Places, command func(context.Context, []string, term.Streams) error) error {
	fmt.Fprint(streams.Out, enterView)
	defer fmt.Fprint(streams.Out, leaveView)
	cursor := &journalCursor{e: e}
	if err := cursor.start(ctx); err != nil {
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
			line, err := streams.ReadLine()
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
		var viewed bytes.Buffer
		if err := ShowStatus(ctx, e, term.Streams{Out: &viewed, Err: &viewed}, nil, false, false, branchflags.BranchFlags{}, places); err != nil {
			fmt.Fprintf(&viewed, "status: %v\n", err)
		}
		drawn = time.Now()
		// The header, its blank line, and the prompt's line take three
		// of the screen's rows.
		fmt.Fprintf(streams.Out, "%sdockhand status --watch · %s · c check · l logs · t tidy · s submit · q quit (optional branch: c <branch>)\n\n%s\n> ",
			clearView, drawn.Format("15:04:05"), fitHeight(strings.TrimRight(viewed.String(), "\n"), streams.Height()-3))
	}
	draw()
	want <- struct{}{}
	paused, progressed := false, false
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
				if err := command(ctx, args, streams); err != nil {
					fmt.Fprintf(streams.Err, "dockhand: %v\n", err)
				}
				fmt.Fprint(streams.Out, "\nPress Enter to return to status. ")
				paused = true
			}
			want <- struct{}{}
		case <-time.After(timings.WatchPoll):
			if paused {
				continue
			}
			events, err := cursor.next(ctx)
			if err != nil {
				return err
			}
			changed := slices.ContainsFunc(events, func(event model.Event) bool { return event.Kind.ChangesStatus() })
			// Progress waits for its interval, and isn't lost meanwhile.
			progressed = progressed || slices.ContainsFunc(events, func(event model.Event) bool {
				return event.Kind == model.EventProgress || event.Kind == model.EventWaiting
			})
			if changed || progressed && time.Since(drawn) >= timings.WatchProgress || time.Since(drawn) >= timings.WatchRedraw {
				draw()
				progressed = false
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
		// The branch named, else the one checked out here; with neither,
		// the newest check of any branch.
		var latest model.Run
		var ok bool
		var err error
		if branch != "" {
			found, resolveErr := e.Resolve(ctx, branch)
			if resolveErr != nil {
				return nil, false, resolveErr
			}
			latest, ok, err = e.LatestCheck(ctx, found.ID)
		} else if current, currentErr := e.Current(ctx); currentErr == nil {
			latest, ok, err = e.LatestCheck(ctx, current.ID)
		} else {
			var runs []model.Run
			runs, err = e.Runs(ctx, store.RunFilter{NoBaselines: true, Limit: 1})
			ok = len(runs) > 0
			if ok {
				latest = runs[0]
			}
		}
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, errors.New("No checks have been recorded yet.")
		}
		return []string{"logs", latest.Name()}, false, nil
	}
	return nil, false, fmt.Errorf("%q: c, l, t, or s and a branch; Enter redraws; q quits", fields[0])
}

// fitHeight is a view cut to rows lines, its last saying how many more
// there are, where it's taller: status's groups come first what needs you,
// so what's cut is what waits (part 5, item 4). A height not known, as off
// a terminal, fits everything.
func fitHeight(view string, rows int) string {
	lines := strings.Split(view, "\n")
	if rows <= 1 || len(lines) <= rows {
		return view
	}
	kept := lines[:rows-1]
	return strings.Join(kept, "\n") + fmt.Sprintf("\n  +%d more; dockhand status shows them all", len(lines)-len(kept))
}
