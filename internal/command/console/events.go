package console

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// journalCursor reads the journal's events past the last one it saw, by
// the sequence the store gave each, at info level: sessions' own comings
// and goings are not news.
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

// eventNames words an event with the names people use for its branch and
// run; a kind it doesn't know is worded as any other, by its message.
type eventNames struct {
	e        *engine.Engine
	branches map[model.BranchID]string
	runs     map[model.RunID]string
}

func newEventNames(e *engine.Engine) *eventNames {
	return &eventNames{e: e, branches: map[model.BranchID]string{}, runs: map[model.RunID]string{}}
}

// line is an event as a line: its time, its branch, its run where its
// message doesn't name it, and its message.
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
