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
}

type reporterKey struct{}
type quietKey struct{}
type observerKey struct{}
type reporter struct {
	mu     sync.Mutex
	report func(Update)
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
	for o := observed; o != nil; o = o.outer {
		o.notify(update)
	}
	if sink == nil || sink.report == nil {
		return
	}
	if quiet, _ := ctx.Value(quietKey{}).(bool); quiet && update.Level == Info {
		update.Level = Verbose
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.report(update)
}
