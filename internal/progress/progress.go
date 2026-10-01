// Package progress carries user-facing progress reports from operations to
// whoever is watching: the CLI prints them, a check's run keeps the ones a
// person following it needs, and tests collect them. Nothing decides
// anything by them. Reports are sentences for a person, not log lines.
package progress

import (
	"context"
	"fmt"
	"sync"
)

// Level says who a report is for. Info is the minimum a person needs to
// follow a command; Verbose adds identifiers and the work behind the scenes;
// Debug adds every sub-operation.
type Level int

const (
	Info Level = iota
	Verbose
	Debug
)

func (l Level) String() string {
	switch l {
	case Verbose:
		return "verbose"
	case Debug:
		return "debug"
	}
	return "info"
}

type Update struct {
	Level   Level
	Message string
	// About is what the report is about, as Within said, which Message
	// begins with too: a part of the work, as one environment of a check.
	About string
}

type reporterKey struct{}
type quietKey struct{}
type observerKey struct{}
type keptKey struct{}
type withinKey struct{}
type reporter struct {
	mu     sync.Mutex
	report func(Update)
	// said are the reports ReportOnce has made through it.
	said map[string]bool
}

// WithReporter serializes callbacks, including those from concurrent operations.
// The callback should return promptly and must not report recursively.
func WithReporter(ctx context.Context, report func(Update)) context.Context {
	return context.WithValue(ctx, reporterKey{}, &reporter{report: report})
}

// observer is one that Observe added, and the one it was added beside.
type observer struct {
	mu      sync.Mutex
	observe func(Update)
	outer   *observer
}

// Observe has the reports made under ctx go to observe as well as to the
// reporter, at the level they were made: Quiet lowers what a command
// shows of the work it drives, not what the work's own record keeps.
// Callbacks are serialized; observe should return promptly and must not
// report under the context Observe returns.
func Observe(ctx context.Context, observe func(Update)) context.Context {
	if observe == nil {
		return ctx
	}
	outer, _ := ctx.Value(observerKey{}).(*observer)
	return context.WithValue(ctx, observerKey{}, &observer{observe: observe, outer: outer})
}

func (o *observer) notify(update Update) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.observe(update)
}

// Keep is Observe for an observer that keeps the info reports made under
// ctx for whoever follows the work, as a check's run keeps each
// environment's: a command driving it Quiet shows them through that
// record, so its reporter lowers them to debug rather than verbose, and -v
// doesn't show each twice, once bare and once kept (the hugo exercise's
// check-64).
func Keep(ctx context.Context, keep func(Update)) context.Context {
	return context.WithValue(Observe(ctx, keep), keptKey{}, true)
}

// Within has the reports made under ctx say first what they're about, as
// "macOS 15 (Tart): ": two environments of a check stage at once, and
// their lines were told apart only by their order (the hugo exercise's
// check-64). Within an earlier one, both are said, outer first.
func Within(ctx context.Context, about string) context.Context {
	if outer, _ := ctx.Value(withinKey{}).(string); outer != "" {
		about = outer + ": " + about
	}
	return context.WithValue(ctx, withinKey{}, about)
}

// Quiet lowers the info reports made under ctx to verbose. A command that
// drives the workflow only to see its own job through is not the driver's
// audience: what the engine and providers say as they work is the work
// behind the scenes to it, and its own narrative says what matters. A
// resident driver reports under the plain context and keeps every line.
func Quiet(ctx context.Context) context.Context {
	return context.WithValue(ctx, quietKey{}, true)
}

// Report emits an info-level report.
func Report(ctx context.Context, format string, args ...any) { emit(ctx, Info, format, args...) }

// ReportOnce emits an info-level report the first time it's made to the
// command's reporter, and not again: a notice about the work, such as a
// port being a stub, that each pass over the same work would repeat.
func ReportOnce(ctx context.Context, format string, args ...any) {
	if sink, _ := ctx.Value(reporterKey{}).(*reporter); sink != nil {
		message := fmt.Sprintf(format, args...)
		sink.mu.Lock()
		said := sink.said[message]
		if sink.said == nil {
			sink.said = map[string]bool{}
		}
		sink.said[message] = true
		sink.mu.Unlock()
		if said {
			return
		}
	}
	emit(ctx, Info, format, args...)
}

// VerboseReport emits a report for people who asked for more.
func VerboseReport(ctx context.Context, format string, args ...any) {
	emit(ctx, Verbose, format, args...)
}

// DebugReport emits a report about a sub-operation.
func DebugReport(ctx context.Context, format string, args ...any) { emit(ctx, Debug, format, args...) }

func emit(ctx context.Context, level Level, format string, args ...any) {
	sink, _ := ctx.Value(reporterKey{}).(*reporter)
	observed, _ := ctx.Value(observerKey{}).(*observer)
	if (sink == nil || sink.report == nil) && observed == nil {
		return
	}
	update := Update{Level: level, Message: fmt.Sprintf(format, args...)}
	if about, _ := ctx.Value(withinKey{}).(string); about != "" {
		update.Message, update.About = about+": "+update.Message, about
	}
	for o := observed; o != nil; o = o.outer {
		o.notify(update)
	}
	if sink == nil || sink.report == nil {
		return
	}
	if quiet, _ := ctx.Value(quietKey{}).(bool); quiet && update.Level == Info {
		update.Level = Verbose
		if kept, _ := ctx.Value(keptKey{}).(bool); kept {
			update.Level = Debug
		}
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.report(update)
}
