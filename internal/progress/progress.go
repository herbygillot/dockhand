// Package progress carries user-facing progress reports from operations to
// whoever is watching: the CLI prints them, tests collect them, and nothing
// else depends on them. Reports are sentences for a person, not log lines.
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
	Scope   string
	Message string
}

type reporterKey struct{}
type scopeKey struct{}
type reporter struct {
	mu     sync.Mutex
	report func(Update)
}

// WithReporter serializes callbacks, including those from concurrent operations.
// The callback should return promptly and must not report recursively.
func WithReporter(ctx context.Context, report func(Update)) context.Context {
	return context.WithValue(ctx, reporterKey{}, &reporter{report: report})
}

// WithScope names what subsequent reports are about, such as a port.
func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
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
	if sink == nil || sink.report == nil {
		return
	}
	scope, _ := ctx.Value(scopeKey{}).(string)
	update := Update{Level: level, Scope: scope, Message: fmt.Sprintf(format, args...)}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.report(update)
}
