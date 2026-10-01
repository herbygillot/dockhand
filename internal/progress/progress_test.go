package progress_test

import (
	"context"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestReporterSerializesConcurrentReportsAndPreservesCancellation(t *testing.T) {
	var updates []progress.Update
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := progress.WithReporter(parent, func(update progress.Update) { updates = append(updates, update) })
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() { progress.Report(ctx, "staging %d", i) })
	}
	workers.Wait()
	require.Len(t, updates, 12)
	seen := map[string]bool{}
	for _, update := range updates {
		seen[update.Message] = true
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

// An observer sees each report at the level it was made, beside the
// reporter, whatever Quiet makes of it for the terminal. One added beside
// another leaves the other watching, and reports made outside it aren't
// its; without a reporter, it still sees them.
func TestObserversSeeReportsAtTheLevelMade(t *testing.T) {
	var shown, inner, outer []progress.Update
	ctx := progress.WithReporter(context.Background(), func(update progress.Update) { shown = append(shown, update) })
	watched := progress.Observe(ctx, func(update progress.Update) { outer = append(outer, update) })
	watched = progress.Observe(watched, func(update progress.Update) { inner = append(inner, update) })
	quiet := progress.Quiet(watched)
	progress.Report(quiet, "building the index")
	progress.VerboseReport(quiet, "generating it in full")
	progress.Report(ctx, "not watched")
	require.Equal(t, []progress.Update{
		{Level: progress.Verbose, Message: "building the index"},
		{Level: progress.Verbose, Message: "generating it in full"},
		{Level: progress.Info, Message: "not watched"},
	}, shown)
	made := []progress.Update{{Level: progress.Info, Message: "building the index"}, {Level: progress.Verbose, Message: "generating it in full"}}
	require.Equal(t, made, inner)
	require.Equal(t, made, outer)

	var alone []progress.Update
	progress.Report(progress.Observe(context.Background(), func(update progress.Update) { alone = append(alone, update) }), "no reporter")
	require.Equal(t, []progress.Update{{Level: progress.Info, Message: "no reporter"}}, alone)
	progress.Report(progress.Observe(context.Background(), nil), "no observer required")

	var concurrent []progress.Update
	serialized := progress.Observe(context.Background(), func(update progress.Update) { concurrent = append(concurrent, update) })
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() { progress.Report(serialized, "staging %d", i) })
	}
	workers.Wait()
	require.Len(t, concurrent, 12)
}

// A notice made once through a command's reporter isn't made again, as a
// stub's is by each load of the port; another is, and so is the same one
// under another reporter.
func TestAReportMadeOnceIsntRepeated(t *testing.T) {
	var got []string
	ctx := progress.WithReporter(context.Background(), func(u progress.Update) { got = append(got, u.Message) })
	progress.ReportOnce(ctx, "%s is a stub", "py-demo")
	progress.ReportOnce(ctx, "%s is a stub", "py-demo")
	progress.ReportOnce(ctx, "%s is a stub", "py-other")
	progress.Report(ctx, "said each time")
	progress.Report(ctx, "said each time")
	require.Equal(t, []string{"py-demo is a stub", "py-other is a stub", "said each time", "said each time"}, got)
	var again []string
	progress.ReportOnce(progress.WithReporter(context.Background(), func(u progress.Update) { again = append(again, u.Message) }), "%s is a stub", "py-demo")
	require.Equal(t, []string{"py-demo is a stub"}, again)
}

// Reports made within what they're about say it first, outer first, to
// the reporter and to observers alike; and what a keeping observer keeps
// isn't shown again at -v by a Quiet command, which shows it through the
// observer's record (the hugo exercise's check-64).
func TestReportsSayWhatTheyreAboutAndKeptOnesArentRepeated(t *testing.T) {
	var shown, kept []progress.Update
	ctx := progress.WithReporter(context.Background(), func(update progress.Update) { shown = append(shown, update) })
	environment := progress.Keep(progress.Within(progress.Quiet(ctx), "macOS 15 (Tart)"), func(update progress.Update) { kept = append(kept, update) })
	progress.Report(environment, "Building the PortIndex")
	progress.VerboseReport(progress.Within(environment, "base fd44713"), "indexing in full")
	progress.Report(progress.Within(progress.Quiet(ctx), "not kept"), "lowered to verbose")
	require.Equal(t, []progress.Update{
		{Level: progress.Debug, Message: "macOS 15 (Tart): Building the PortIndex", About: "macOS 15 (Tart)"},
		{Level: progress.Verbose, Message: "macOS 15 (Tart): base fd44713: indexing in full", About: "macOS 15 (Tart): base fd44713"},
		{Level: progress.Verbose, Message: "not kept: lowered to verbose", About: "not kept"},
	}, shown)
	require.Equal(t, []progress.Update{
		{Level: progress.Info, Message: "macOS 15 (Tart): Building the PortIndex", About: "macOS 15 (Tart)"},
		{Level: progress.Verbose, Message: "macOS 15 (Tart): base fd44713: indexing in full", About: "macOS 15 (Tart): base fd44713"},
	}, kept)
}
