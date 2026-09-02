package render

// The three renderings of one reconciliation pass, pinned. What the
// goldens hold is not only the wording — that is already RecordLines'
// and verdict's — but the shape: which line goes on which stream, and
// in what order relative to the branch it is about. Those two were
// spread across three traversals of the namespace before this package
// held them, and the only way to check them was to run the verbs.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// reportClock is the pass's clock read, fixed so that a running run's
// elapsed sentence is a constant instead of a stopwatch.
var reportClock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func passed(plat string) *record.Record {
	return &record.Record{Schema: 2, Sha: "0123456789abcdef0123", Port: "jq",
		Runs: map[string]record.Run{plat: {State: record.Passed, Linted: true, Lint: "clean"}}}
}

// sampleReport is one pass over a namespace holding every shape the
// renderings have to tell apart: a branch that only stands, one that
// stands and has a pull request to report, one deleted mid-pass, one
// whose note could not be read at all, and one whose forge would not
// answer.
func sampleReport() Report {
	merged := gh.PullRequest{Number: 80, Title: "jq: update to 1.7", State: "closed",
		MergedAt: "2026-09-01T00:00:00Z", HTMLURL: "https://x/80"}
	open := gh.PullRequest{Number: 77, Title: "jq: update to 2.5", State: "open", HTMLURL: "https://x/77"}
	return Report{
		Repository: "/checkout/ports",
		Now:        reportClock,
		Branches: []BranchReport{
			{
				Branch: "dockhand/jq-local",
				Tip:    "44cfc9eea250",
				Note:   passed("Testos"),
			},
			{
				Branch: "dockhand/jq-landed",
				Tip:    "8da38abbbd45",
				Drift:  "unverified",
				Retire: verdict.Reconciliation{Promoted: true, Cleaned: true,
					PR: verdict.PRFact{Found: true, Number: 80, Merged: true, URL: "https://x/80"}},
				PR: merged,
				Prose: []Line{
					{Stream: ToErr, Text: `removed dockhand/jq-landed from "herby"`},
					{Stream: ToOut, Text: "discarded dockhand/jq-landed (8da38abbbd45)"},
				},
			},
			{
				Branch: "dockhand/jq-open",
				Tip:    "5e687c527fc9",
				Note:   passed("Testos"),
				Retire: verdict.Reconciliation{Promoted: true,
					PR: verdict.PRFact{Found: true, Number: 77, Open: true, URL: "https://x/77"}},
				PR: open,
			},
			{
				Branch: "dockhand/jq-running",
				Tip:    "997767bd393f",
				Note: &record.Record{Schema: 2, Sha: "997767bd393f", Port: "jq",
					Runs: map[string]record.Run{"Testos": {State: record.Running,
						Job: verify.Job{Provider: "fake", ID: "fake-1",
							Started: reportClock.Add(-90 * time.Second)}}}},
			},
			{
				Branch:     "dockhand/jq-unreadable",
				ObserveErr: "reading the note: bad object",
			},
			{
				Branch: "dockhand/jq-outage",
				Tip:    "ff6909944aa2",
				Drift:  "unverified",
				Retire: verdict.Reconciliation{Promoted: true,
					Err: "gh api: HTTP 502 from api.github.com"},
			},
			{
				// The same outage as the sweep records it. One pass fills
				// one slot or the other and never both; the fixture carries
				// a branch for each so that both renderings of an
				// unanswerable lookup are pinned from one report.
				Branch:   "dockhand/jq-unswept",
				Tip:      "ff6909944aa2",
				Drift:    "unverified",
				Retire:   verdict.Reconciliation{Promoted: true},
				SweepErr: "gh api: HTTP 502 from api.github.com",
			},
		},
		Drain: []Line{{Stream: ToErr, Text: "still deferred: dockhand/jq-local on Testos — all 2 verification slots are busy"}},
		Orphans: []Orphan{
			{Name: "dockhand-worker-stray"},
			{Name: "dockhand-worker-elsewhere", Owner: "/other/ports"},
		},
	}
}

// reportDir holds one pinned document per rendering. A directory of
// its own, and not the PR bodies': those are pinned per input variant
// and swept for strays against that list, and a report golden sitting
// among them would read as a body nobody renders.
const reportDir = "testdata/report"

// checkReport compares both streams of one rendering against
// testdata/report/<name>.golden. The two are joined into a single
// document because half of what these pin is which stream a line chose.
func checkReport(t *testing.T, name string, out, errb *bytes.Buffer) {
	t.Helper()
	got := "--- stdout\n" + out.String() + "--- stderr\n" + errb.String()
	path := filepath.Join(reportDir, name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll(reportDir, 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden at %s; `go test ./internal/render -update` writes it", path)
	if string(want) != got {
		t.Errorf("report differs from %s:\n%s\nif the new rendering is intended, `go test ./internal/render -update` rewrites the golden", path, lineDiff(path, string(want), got))
	}
}

// reportGoldens is every rendering pinned here, which is also the list
// the stale sweep below reads: a golden no rendering produces is a
// shape that stopped existing and leaves with the code.
var reportGoldens = []string{"report_text", "report_sweep", "report_json"}

func TestReportRenderings(t *testing.T) {
	t.Run("report_text", func(t *testing.T) {
		var out, errb bytes.Buffer
		sampleReport().Text(&out, &errb)
		checkReport(t, "report_text", &out, &errb)
	})
	t.Run("report_sweep", func(t *testing.T) {
		var out, errb bytes.Buffer
		sampleReport().Sweep(&out, &errb)
		checkReport(t, "report_sweep", &out, &errb)
	})
	t.Run("report_json", func(t *testing.T) {
		var out, errb bytes.Buffer
		require.NoError(t, sampleReport().JSON(&out, &errb, exitcode.Of(exitcode.OK, "")))
		checkReport(t, "report_json", &out, &errb)
	})
	entries, err := os.ReadDir(reportDir)
	require.NoError(t, err)
	pinned := map[string]bool{}
	for _, n := range reportGoldens {
		pinned[n] = true
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".golden")
		assert.True(t, pinned[name], "stale golden %s: no rendering produces it", filepath.Join(reportDir, e.Name()))
	}
}

func TestReportJSONEmitsAnEmptyNamespaceAsAnEmptyList(t *testing.T) {
	var out, errb bytes.Buffer
	require.NoError(t, Report{Repository: "/checkout/ports"}.JSON(&out, &errb, exitcode.Of(exitcode.OK, "")))
	assert.Contains(t, out.String(), `"branches": []`,
		"the key is always there and never null; a consumer indexes it")
	assert.NotContains(t, out.String(), "orphan_workers",
		"and the audit's key is absent on a clean machine rather than empty")
	assert.True(t, strings.HasSuffix(out.String(), "}\n"),
		"an encoder's trailing newline is part of what has always been published")
	assert.Empty(t, errb.String())
}

func TestReportTextNamesTheRepositoryWhenTheNamespaceIsEmpty(t *testing.T) {
	var out, errb bytes.Buffer
	Report{Repository: "/checkout/ports"}.Text(&out, &errb)
	assert.Equal(t, "no dockhand branches in /checkout/ports\n", out.String())
	assert.Empty(t, errb.String())
}

func TestReportCleanedBranchDropsItsStandingButKeepsItInTheDocument(t *testing.T) {
	// The human report says the one thing left to say about a branch
	// that is gone; the document still publishes what was deleted,
	// because a consumer is entitled to know more than that something
	// happened.
	var out, errb bytes.Buffer
	sampleReport().Text(&out, &errb)
	assert.Contains(t, out.String(), "dockhand/jq-landed               PR #80 merged — branch cleaned")
	assert.NotContains(t, out.String(), "dockhand/jq-landed\n  unverified")

	var jout, jerr bytes.Buffer
	require.NoError(t, sampleReport().JSON(&jout, &jerr, exitcode.Of(exitcode.OK, "")))
	assert.Contains(t, jout.String(), `"drift": "unverified"`)
	assert.Contains(t, jout.String(), `"cleaned": true`)
}
