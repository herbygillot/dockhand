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
			assert.False(t, d.Cleans(true), "--keep-merged withholds every deletion")
		})
	}
}

// One line per verb per fact (D27). `status` acts on nothing and names
// `cycle` beside a merged pull request; `cycle` says what it did, or —
// on every kept case — why the branch is still there. Under `status
// --no-update` the forge was never asked, and the line says so rather
// than reporting an absent pull request.
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
		{"pushed, no PR", Reconciliation{Promoted: true, Minted: true}, "promoted; no PR found"},
		{"pushed, the forge not asked (--no-update)",
			Reconciliation{Promoted: true, Minted: true, Unasked: true},
			"promoted; PR not checked (--no-update)"},
		{"merged, under status: the verb that acts is named",
			Reconciliation{Promoted: true, Minted: true, PR: merged},
			"PR #42 merged — `dockhand cycle` retires the branch"},
		{"merged and cleaned, under cycle",
			Reconciliation{Promoted: true, Minted: true, PR: merged, Cleaned: true},
			"PR #42 merged — branch cleaned"},
		{"merged, cleaning failed",
			Reconciliation{Promoted: true, Minted: true, PR: merged, CleanErr: "worker still running"},
			"PR #42 merged; cleaning failed: worker still running"},
		{"merged, withheld by --keep-merged",
			Reconciliation{Promoted: true, Minted: true, PR: merged, Withheld: "--keep-merged"},
			"PR #42 merged — kept: --keep-merged"},
		{"merged, withheld by a hold",
			Reconciliation{Promoted: true, Minted: true, PR: merged, Withheld: "held (keeping it for a bisect)"},
			"PR #42 merged — kept: held (keeping it for a bisect)"},
		{"merged on a hand-made branch: nothing here removes it",
			Reconciliation{Promoted: true, PR: merged},
			"PR #42 merged — not a dockhand branch, so nothing here removes it"},
		{"open", Reconciliation{Promoted: true, Minted: true, PR: open},
			"PR #43 open (https://example.invalid/pr/43)"},
		{"closed without merging", Reconciliation{Promoted: true, Minted: true, PR: closed},
			"PR #44 closed without merging"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.r.Line())
		})
	}
}
