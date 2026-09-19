package progress_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestReporterSerializesConcurrentScopesAndPreservesCancellation(t *testing.T) {
	var updates []progress.Update
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := progress.WithReporter(parent, func(update progress.Update) { updates = append(updates, update) })
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() { progress.Report(progress.WithScope(ctx, fmt.Sprint(i)), "staging %d", i) })
	}
	workers.Wait()
	require.Len(t, updates, 12)
	seen := map[string]bool{}
	for _, update := range updates {
		require.Equal(t, "staging "+update.Scope, update.Message)
		seen[update.Scope] = true
	}
	require.Len(t, seen, 12)
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	progress.Report(t.Context(), "no observer required")
	progress.Report(progress.WithReporter(t.Context(), nil), "no observer required")
}

func TestQuietLowersInfoToVerbose(t *testing.T) {
	t.Parallel()
	var seen []progress.Level
	ctx := progress.WithReporter(context.Background(), func(update progress.Update) { seen = append(seen, update.Level) })
	quiet := progress.Quiet(ctx)
	progress.Report(quiet, "cloning image")
	progress.VerboseReport(quiet, "already verbose")
	progress.DebugReport(quiet, "a sub-operation")
	progress.Report(ctx, "still info outside the quiet context")
	require.Equal(t, []progress.Level{progress.Verbose, progress.Verbose, progress.Debug, progress.Info}, seen)
}
