package verdict

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/record"
)

// noted builds a record over a tree with one run in a state.
func noted(sha, tree string, states map[string]record.RunState) Noted {
	r := set(states)
	r.Tree = tree
	r.Sha = sha
	return Noted{Sha: sha, Record: r}
}

// drift asks the two questions in the order describeUnverifiedTip asks
// them: content identity first, and the branch's own history only when
// nothing covers the tip. The cases below are about that composition as
// much as about either sentence.
func drift(tipTree, branch string, noted []Noted, behind []Ancestor) string {
	if s := DriftOverTree(tipTree, slices.Values(noted)); s != "" {
		return s
	}
	return DriftBehind(branch, slices.Values(behind))
}

func TestDrift(t *testing.T) {
	passed := map[string]record.RunState{"Sequoia": record.Passed}
	failed := map[string]record.RunState{"Sequoia": record.Failed}

	t.Run("nothing anywhere", func(t *testing.T) {
		assert.Equal(t, "unverified", drift("t1", "dockhand/jq", nil, nil))
	})

	// An amend replaces the commit, so a reworded tip's verdicts live on
	// a sha the branch no longer reaches. The tree is what still matches,
	// and matching it is not drift at all.
	t.Run("the same content under another name", func(t *testing.T) {
		assert.Equal(t, "passed (Sequoia) at abc1234 — the tip differs only in commit metadata",
			drift("t1", "dockhand/jq", []Noted{noted("abc1234", "t1", passed)}, nil))
	})

	t.Run("a same-tree record with nothing passing is not evidence", func(t *testing.T) {
		assert.Equal(t, "unverified",
			drift("t1", "dockhand/jq", []Noted{noted("abc1234", "t1", failed)}, nil))
	})

	t.Run("a passing record over other content is not evidence", func(t *testing.T) {
		assert.Equal(t, "unverified",
			drift("t1", "dockhand/jq", []Noted{noted("abc1234", "t9", passed)}, nil))
	})

	t.Run("verified behind the tip", func(t *testing.T) {
		assert.Equal(t,
			"tip unverified; passed (Sequoia) at abc1234, 2 commit(s) behind — `dockhand verify dockhand/jq` tests the tip",
			drift("t1", "dockhand/jq", nil,
				[]Ancestor{{Noted: noted("abc1234", "t0", passed), Behind: 2}}))
	})

	// An ancestor's verdict is reported whatever it says: the sentence
	// is about the gap, not about the outcome.
	t.Run("a failed ancestor is still the drift", func(t *testing.T) {
		assert.Equal(t,
			"tip unverified; failed (Sequoia) at abc1234, 1 commit(s) behind — `dockhand verify dockhand/jq` tests the tip",
			drift("t1", "dockhand/jq", nil,
				[]Ancestor{{Noted: noted("abc1234", "t0", failed), Behind: 1}}))
	})

	// Schema 3 bears a record at mint, so a --no-verify branch that has
	// grown a commit has an ancestor carrying a note that holds nothing.
	// The behind sentence claims the change was verified at a commit the
	// branch moved past, which of that ancestor is false twice over — so
	// it is stepped over, and the honest bare word is what is left.
	t.Run("an ancestor holding no verdict is not the drift", func(t *testing.T) {
		minted := noted("abc1234", "t0", nil)
		assert.Empty(t, minted.Record.Runs)
		assert.Equal(t, "unverified",
			drift("t1", "dockhand/jq", nil, []Ancestor{{Noted: minted, Behind: 1}}))
	})

	// And it is stepped OVER, not stopped at: a real verdict further back
	// is still the gap the sentence is about.
	t.Run("the nearest ancestor that holds one is the drift", func(t *testing.T) {
		assert.Equal(t,
			"tip unverified; passed (Sequoia) at def5678, 2 commit(s) behind — `dockhand verify dockhand/jq` tests the tip",
			drift("t1", "dockhand/jq", nil, []Ancestor{
				{Noted: noted("abc1234", "t0", nil), Behind: 1},
				{Noted: noted("def5678", "t0", passed), Behind: 2},
			}))
	})

	// Content identity is checked before ancestry, so a tip whose
	// content was verified under another sha never reads as behind.
	t.Run("content identity wins over ancestry", func(t *testing.T) {
		assert.Equal(t, "passed (Sequoia) at abc1234 — the tip differs only in commit metadata",
			drift("t1", "dockhand/jq",
				[]Noted{noted("abc1234", "t1", passed)},
				[]Ancestor{{Noted: noted("def5678", "t0", passed), Behind: 1}}))
	})

	// Both searches take the FIRST match in the caller's order, which is
	// the notes ref's own listing and the rev-list walk. A caller that
	// sorts either list changes which record the sentence names.
	t.Run("the first match wins in the caller's order", func(t *testing.T) {
		assert.Equal(t, "passed (Sequoia) at first — the tip differs only in commit metadata",
			drift("t1", "dockhand/jq", []Noted{
				noted("zzz", "t9", passed), // wrong tree, skipped
				noted("first", "t1", passed),
				noted("second", "t1", passed),
			}, nil))
		assert.Equal(t,
			"tip unverified; passed (Sequoia) at first, 1 commit(s) behind — `dockhand verify dockhand/jq` tests the tip",
			drift("t1", "dockhand/jq", nil, []Ancestor{
				{Noted: noted("first", "t0", passed), Behind: 1},
				{Noted: noted("second", "t0", passed), Behind: 2},
			}))
	})
}

// A pull is a git read at the caller, so the record that answers must
// be the last one read. Status describes every unnoted branch, and a
// scan that ran to the end would cost a note read per commit in the ref
// each time it did.
func TestDriftStopsAtTheMatch(t *testing.T) {
	passed := map[string]record.RunState{"Sequoia": record.Passed}

	t.Run("over the tree", func(t *testing.T) {
		pulled := 0
		records := []Noted{
			noted("zzz", "t9", passed), // wrong tree: read, and stepped over
			noted("first", "t1", passed),
			noted("second", "t1", passed), // never read
		}
		assert.Equal(t, "passed (Sequoia) at first — the tip differs only in commit metadata",
			DriftOverTree("t1", func(yield func(Noted) bool) {
				for _, n := range records {
					pulled++
					if !yield(n) {
						return
					}
				}
			}))
		assert.Equal(t, 2, pulled)
	})

	t.Run("behind the tip", func(t *testing.T) {
		pulled := 0
		assert.Equal(t,
			"tip unverified; passed (Sequoia) at first, 1 commit(s) behind — `dockhand verify dockhand/jq` tests the tip",
			DriftBehind("dockhand/jq", func(yield func(Ancestor) bool) {
				for _, a := range []Ancestor{
					{Noted: noted("first", "t0", passed), Behind: 1},
					{Noted: noted("second", "t0", passed), Behind: 2},
				} {
					pulled++
					if !yield(a) {
						return
					}
				}
			}))
		assert.Equal(t, 1, pulled)
	})
}
