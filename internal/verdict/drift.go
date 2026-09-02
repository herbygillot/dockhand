package verdict

import (
	"fmt"
	"iter"

	"github.com/herbygillot/dockhand/internal/record"
)

// Noted pairs a record with the commit it is attached to. The sha is
// already abbreviated: how a sha is shortened is git's business, and a
// judgment that reached for it to render one would be a judgment that
// needs a repository to test.
type Noted struct {
	Sha    string
	Record record.Record
}

// Ancestor is a Noted on the branch's own history, with how far behind
// the tip it sits. The distance is the caller's count over the same
// rev-list walk it read the notes from, because it is a fact about the
// branch and not about the record.
type Ancestor struct {
	Noted
	Behind int
}

// The drift finding is asked in two questions, in this order, because
// answering the first is cheap and answering the second is not: the
// caller reads the notes ref either way, and only a tip nothing covers
// is worth a rev-list and a note read per ancestor.
//
// Both take a sequence rather than a slice, and both stop pulling at
// the first match. A caller's sequence is one git read per element, so
// stopping is the difference between reading the note that answers and
// reading every note there is — while the judgment stays a function of
// the values it was handed.

// DriftOverTree names the record that already covers the tip's content,
// and returns "" when none does.
//
// Content identity is checked against every record, not just ancestors:
// an amend replaces the commit, so a reworded tip's verdicts live on a
// sha the branch no longer reaches, and the tree is what still matches.
// A verdict set with a pass over the tip's own tree is therefore not
// drift at all, it is the same content under another name — which is
// why this is asked before anything about ancestry.
//
// The match is the FIRST in the order the caller yields, the notes
// ref's own listing, so a caller that sorts or dedupes changes which
// record the sentence names.
func DriftOverTree(tipTree string, noted iter.Seq[Noted]) string {
	for n := range noted {
		if n.Record.Tree != tipTree || !n.Record.AnyState(record.Passed) {
			continue
		}
		return fmt.Sprintf("%s at %s — the tip differs only in commit metadata", Summarize(n.Record), n.Sha)
	}
	return ""
}

// DriftBehind says what a tip no record covers means: verified at a
// commit the branch has since moved past — the sha gap that IS the
// drift mechanism — or never verified at all.
//
// The nearest ancestor is the FIRST the caller yields, walking the
// rev-list from the tip, so a caller that reorders the walk changes
// which commit the sentence reports the gap to.
func DriftBehind(branch string, behind iter.Seq[Ancestor]) string {
	for a := range behind {
		return fmt.Sprintf("tip unverified; %s at %s, %d commit(s) behind — `dockhand verify %s` tests the tip",
			Summarize(a.Record), a.Sha, a.Behind, branch)
	}
	return "unverified"
}
