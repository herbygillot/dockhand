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
)

// reportClock is the pass's clock read, fixed so that a running run's
// elapsed sentence is a constant instead of a stopwatch.
var reportClock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func passed(plat string) *record.Record {
	n := templateNote(map[string]record.Run{
		plat: {State: record.Passed, Linted: true, Lint: "clean"},
	}, nil)
	return &n
}

// sampleReport is one `status` pass over a namespace holding every
// shape the renderings have to tell apart: a branch that only stands,
// one whose pull request merged and is left for `cycle` (D27), one
// with an open pull request, a running one, one with a queued run the
// remedy is named beside, one minted with --no-verify, one whose note
// could not be read at all, one whose forge would not answer, and a
// hand-made branch carrying a note whose pull request merged — shown,
// and never anybody's to delete.
func sampleReport() Report {
	merged := gh.PullRequest{Number: 80, Title: "jq: update to 1.7", State: "closed",
		MergedAt: "2026-09-01T00:00:00Z", HTMLURL: "https://x/80"}
	open := gh.PullRequest{Number: 77, Title: "jq: update to 2.5", State: "open", HTMLURL: "https://x/77"}
	landed := gh.PullRequest{Number: 81, Title: "jq: update to 1.8", State: "closed",
		MergedAt: "2026-09-01T00:00:00Z", HTMLURL: "https://x/81"}
	return Report{
		Repository: "/checkout/ports",
		Now:        reportClock,
		Branches: []BranchReport{
			{
				Branch: "dockhand/jq-local",
				Minted: true,
				Tip:    "44cfc9eea250",
				Note:   passed("Testos"),
			},
			{
				Branch: "dockhand/jq-landed",
				Minted: true,
				Tip:    "8da38abbbd45",
				Drift:  "unverified",
				Retire: verdict.Reconciliation{Promoted: true, Minted: true,
					PR: verdict.PRFact{Found: true, Number: 80, Merged: true, URL: "https://x/80"}},
				PR: merged,
			},
			{
				Branch: "dockhand/jq-open",
				Minted: true,
				Tip:    "5e687c527fc9",
				Note:   passed("Testos"),
				Retire: verdict.Reconciliation{Promoted: true, Minted: true,
					PR: verdict.PRFact{Found: true, Number: 77, Open: true, URL: "https://x/77"}},
				PR: open,
			},
			{
				Branch: "dockhand/jq-running",
				Minted: true,
				Tip:    "997767bd393f",
				Note:   running("Testos", reportClock.Add(-90*time.Second)),
			},
			{
				Branch: "dockhand/jq-queued",
				Minted: true,
				Tip:    "0a1b2c3d4e5f",
				Note:   queued("Testos", "all 2 verification slots are busy (2 VMs running)"),
			},
			{
				// A branch minted with --no-verify: the record exists from
				// the moment the branch does and will never hold a run,
				// because nobody asked for a verdict and the drain steps
				// over it. Schema 2 had no note here at all.
				Branch: "dockhand/jq-unasked",
				Minted: true,
				Tip:    "b21bd0e5a1c7",
				Note:   unasked(),
			},
			{
				Branch:     "dockhand/jq-unreadable",
				Minted:     true,
				ObserveErr: "reading the note: bad object",
			},
			{
				Branch: "dockhand/jq-outage",
				Minted: true,
				Tip:    "ff6909944aa2",
				Drift:  "unverified",
				Retire: verdict.Reconciliation{Promoted: true, Minted: true,
					Err: "gh api: HTTP 502 from api.github.com"},
			},
			{
				// The fold-in: a person's branch that `verify` was pointed
				// at. Its note is shown and its pull request judged, and
				// deletion stays inside dockhand/.
				Branch: "erasure-test",
				Tip:    "c0ffee0c0ffe",
				Note:   passed("Testos"),
				Retire: verdict.Reconciliation{Promoted: true,
					PR: verdict.PRFact{Found: true, Number: 81, Merged: true, URL: "https://x/81"}},
				PR: landed,
			},
		},
		Orphans: []Orphan{
			{Name: "dockhand-worker-stray"},
			{Name: "dockhand-worker-elsewhere", Owner: "/other/ports"},
		},
	}
}

// cycleReport is the same namespace as `cycle` leaves it: the merged
// dockhand branch demolished, with the prose the demolition produced
// kept beside it; a merged branch a hold kept, saying why; the
// hand-made branch left where it was, saying why; and the drain's own
// lines behind the branches.
func cycleReport() Report {
	rep := sampleReport()
	for i := range rep.Branches {
		b := &rep.Branches[i]
		if b.Branch == "dockhand/jq-landed" {
			b.Retire.Cleaned = true
			b.Prose = []Line{
				{Stream: ToErr, Text: `removed dockhand/jq-landed from "herby"`},
				{Stream: ToOut, Text: "discarded dockhand/jq-landed (8da38abbbd45)"},
			}
		}
	}
	rep.Branches = append(rep.Branches, BranchReport{
		Branch: "dockhand/jq-held",
		Minted: true,
		Tip:    "1122334455aa",
		Note:   passed("Testos"),
		Retire: verdict.Reconciliation{Promoted: true, Minted: true,
			PR:       verdict.PRFact{Found: true, Number: 82, Merged: true, URL: "https://x/82"},
			Withheld: "held (keeping it for a bisect, 2026-09-01 02:00 UTC)"},
		Prose: []Line{{Stream: ToErr,
			Text: "dockhand/jq-held is held (keeping it for a bisect, 2026-09-01 02:00 UTC): the deletion is withheld — `dockhand unhold dockhand/jq-held` releases it"}},
	})
	rep.Drain = []Line{
		{Stream: ToErr, Text: "verify: submitted jq on Testos (job fake-1); `dockhand status` follows it"},
		{Stream: ToErr, Text: "still deferred: dockhand/jq-local on Testos — all 2 verification slots are busy (2 VMs running)"},
	}
	rep.Orphans = nil
	return rep
}

// queued is a branch whose run was deferred: one queued run carrying
// the provider's own words for why, and no job.
func queued(plat, detail string) *record.Record {
	n := templateNote(map[string]record.Run{plat: {State: record.Queued, Detail: detail}}, nil)
	n.Sha = "0a1b2c3d4e5f"
	return &n
}

// running is a branch mid-build: one run, in the guest that started at
// the given time.
func running(plat string, started time.Time) *record.Record {
	n := templateNote(map[string]record.Run{plat: {State: record.Running}},
		map[string]guest{plat: {Started: started}})
	n.Sha = "997767bd393f"
	return &n
}

// unasked is a branch minted with --no-verify: subjects, a destination
// that stops at the branch, and no run there ever will be.
func unasked() *record.Record {
	return &record.Record{
		Schema: record.Schema, Sha: "b21bd0e5a1c7", Slug: "jq-1.8.1",
		Subjects:    []record.Subject{{Port: "jq", Names: []string{"jq"}}},
		Destination: record.ToBranch,
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
var reportGoldens = []string{"report_text", "report_cycle", "report_as_recorded", "report_json", "report_attention"}

func TestReportRenderings(t *testing.T) {
	t.Run("report_text", func(t *testing.T) {
		var out, errb bytes.Buffer
		sampleReport().Text(&out, &errb)
		checkReport(t, "report_text", &out, &errb)
	})
	t.Run("report_cycle", func(t *testing.T) {
		var out, errb bytes.Buffer
		cycleReport().Text(&out, &errb)
		checkReport(t, "report_cycle", &out, &errb)
	})
	t.Run("report_as_recorded", func(t *testing.T) {
		// `status --no-update`: the same branches as the ledger holds
		// them, no pull request asked about, the mark at the top.
		rep := sampleReport()
		rep.AsRecorded = true
		rep.Orphans = nil
		for i := range rep.Branches {
			b := &rep.Branches[i]
			if b.Retire.Promoted {
				b.Retire = verdict.Reconciliation{Promoted: true, Minted: b.Minted, Unasked: true}
				b.PR = nil
			}
		}
		var out, errb bytes.Buffer
		rep.Text(&out, &errb)
		checkReport(t, "report_as_recorded", &out, &errb)
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

func TestCycleCleanedBranchDropsItsStanding(t *testing.T) {
	// The human report says the one thing left to say about a branch
	// that is gone.
	var out, errb bytes.Buffer
	cycleReport().Text(&out, &errb)
	assert.Contains(t, out.String(), "dockhand/jq-landed               PR #80 merged — branch cleaned")
	assert.NotContains(t, out.String(), "dockhand/jq-landed\n  unverified")
}

// D27: `status` deletes nothing, so its document has no `cleaned` key
// to be true, and says which branches are dockhand's own so a consumer
// can tell the fold-in's hand-made branch from a minted one.
func TestStatusJSONSaysMintedAndNeverCleaned(t *testing.T) {
	var out, errb bytes.Buffer
	require.NoError(t, sampleReport().JSON(&out, &errb, exitcode.Of(exitcode.OK, "")))
	assert.NotContains(t, out.String(), `"cleaned"`)
	assert.Contains(t, out.String(), "\"branch\": \"erasure-test\",\n      \"minted\": false")
	assert.Contains(t, out.String(), "\"branch\": \"dockhand/jq-local\",\n      \"minted\": true")
	assert.Empty(t, errb.String(), "nothing was acted on, so nothing was said about acting")
}
