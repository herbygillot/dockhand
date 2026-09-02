package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	merged = PRFact{Found: true, Number: 42, URL: "https://example.invalid/pr/42", Merged: true}
	open   = PRFact{Found: true, Number: 43, URL: "https://example.invalid/pr/43", Open: true}
	closed = PRFact{Found: true, Number: 44, URL: "https://example.invalid/pr/44"}
)

func TestDecideRetire(t *testing.T) {
	cases := []struct {
		name     string
		promoted bool
		pr       PRFact
		want     Retirement
	}{
		{"never pushed", false, PRFact{}, RetireUnpromoted},
		{"never pushed, whatever a PR would say", false, merged, RetireUnpromoted},
		{"pushed, no PR", true, PRFact{}, RetireNoPR},
		{"merged", true, merged, RetireMerged},
		{"open", true, open, RetireOpen},
		{"closed without merging", true, closed, RetireClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideRetire(tc.promoted, tc.pr)
			assert.Equal(t, tc.want, d)
			assert.Equal(t, tc.want == RetireMerged, d.Cleans(false),
				"the merged verdict is the only one that deletes")
			assert.False(t, d.Cleans(true), "--no-clean withholds every deletion")
		})
	}
}

func TestSweepLine(t *testing.T) {
	assert.Equal(t, "kept — never promoted", RetireUnpromoted.SweepLine(PRFact{}, false))
	assert.Equal(t, "kept — promoted, but no PR found", RetireNoPR.SweepLine(PRFact{}, false))
	assert.Equal(t, "cleaned — PR #42 merged", RetireMerged.SweepLine(merged, true))
	// Merged is the authority; differing bytes mean a committer amended
	// in flight or a later change superseded it.
	assert.Equal(t, "cleaned — PR #42 merged (upstream bytes differ: amended on merge, or since superseded)",
		RetireMerged.SweepLine(merged, false))
	assert.Equal(t, "kept — PR #43 open (https://example.invalid/pr/43)", RetireOpen.SweepLine(open, false))
	assert.Equal(t, "kept — PR #44 closed without merging; rejection is information",
		RetireClosed.SweepLine(closed, false))

	// Everything but the merged line ignores the content comparison,
	// which is why the caller need not compute it.
	for _, d := range []Retirement{RetireUnpromoted, RetireNoPR, RetireOpen, RetireClosed} {
		assert.Equal(t, d.SweepLine(open, false), d.SweepLine(open, true))
	}
}

func TestReconciliationLine(t *testing.T) {
	cases := []struct {
		name string
		r    Reconciliation
		want string
	}{
		{"an unpromoted branch has nothing to say", Reconciliation{}, ""},
		{"a lookup that could not answer",
			Reconciliation{Promoted: true, Err: "no upstream remote"},
			"PR state unavailable: no upstream remote"},
		{"pushed, no PR", Reconciliation{Promoted: true}, "promoted; no PR found"},
		{"merged and cleaned",
			Reconciliation{Promoted: true, PR: merged, Cleaned: true},
			"PR #42 merged — branch cleaned"},
		{"merged, cleaning failed",
			Reconciliation{Promoted: true, PR: merged, CleanErr: "worker still running"},
			"PR #42 merged; cleaning failed: worker still running"},
		{"merged under --no-clean",
			Reconciliation{Promoted: true, PR: merged},
			"PR #42 merged — `dockhand clean` removes the branch"},
		{"open", Reconciliation{Promoted: true, PR: open},
			"PR #43 open (https://example.invalid/pr/43)"},
		{"closed without merging", Reconciliation{Promoted: true, PR: closed},
			"PR #44 closed without merging"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.r.Line())
		})
	}
}

// The two verbs word the same verdict differently on purpose: clean
// says what the sweep did, status says what happened to the branch the
// reader was asking about. Unifying them would move two verbs' goldens.
func TestTheTwoMergedWordingsStayApart(t *testing.T) {
	sweep := RetireMerged.SweepLine(merged, true)
	report := Reconciliation{Promoted: true, PR: merged, Cleaned: true}.Line()
	assert.Equal(t, "cleaned — PR #42 merged", sweep)
	assert.Equal(t, "PR #42 merged — branch cleaned", report)
	assert.NotEqual(t, sweep, report)
}
