package progress

import (
	"context"
	"fmt"
	"sync"
)

type Update struct {
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

func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func Report(ctx context.Context, format string, args ...any) {
	sink, _ := ctx.Value(reporterKey{}).(*reporter)
	if sink == nil || sink.report == nil {
		return
	}
	scope, _ := ctx.Value(scopeKey{}).(string)
	update := Update{Scope: scope, Message: fmt.Sprintf(format, args...)}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.report(update)
}
