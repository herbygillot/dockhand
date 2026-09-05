package verdict

import "fmt"

// PRFact is a pull request reduced to what a judgment weighs. The
// callers map it from whatever the forge handed them, so verdict decides
// about pull requests without ever running gh — and a table test can
// state one in a struct literal instead of standing up a fake forge.
//
// The zero value is "no pull request", which is what a lookup that found
// nothing means. A PR taken from a list is one that exists, so a mapper
// building list entries sets Found on every one of them.
type PRFact struct {
	Found  bool
	Number int
	Title  string
	URL    string
	// Merged and Open are the two states that matter, mapped from the
	// forge's own spelling — a merge timestamp being present, a state
	// word reading "open" — so the spelling stays at the boundary where
	// the forge's JSON is already being read.
	Merged bool
	Open   bool
	// Version is what the pull request takes its port to, and
	// VersionSource is where that was read from, in words that follow
	// "read from" — "its branch name dockhand/jq-1.9". The mapper sets
	// both or neither: a version is only as good as its source, and the
	// note that weighs one names the other so a reader can weigh it
	// too. Empty is the honest reading for most pull requests — a branch
	// somebody named by hand says nothing dockhand can vouch for, and a
	// title is prose — and the same-port advisory then says only that
	// the PR exists rather than guessing.
	Version       string
	VersionSource string
}

// Retirement is what a dockhand branch's pull request says should
// happen to the branch. Merged-ness is never sha ancestry: the project's
// merge styles rewrite commits as they land, so the PR's own state is
// the only authority.
type Retirement int

const (
	// RetireUnpromoted is a branch that was never pushed. There is no PR
	// to ask about and nothing to conclude.
	RetireUnpromoted Retirement = iota
	// RetireNoPR is a promoted branch with no pull request found — the
	// push happened, the PR did not, or it was deleted.
	RetireNoPR
	// RetireMerged is the one verdict that deletes: the work landed, so
	// everything the branch holds can go.
	RetireMerged
	// RetireOpen is a pull request still under review. The branch stays;
	// a later push updates the PR in place.
	RetireOpen
	// RetireClosed is a pull request closed without merging. The branch
	// stays too — rejection is information, and deleting the evidence of
	// it helps nobody.
	RetireClosed
)

// DecideRetire judges one branch's standing. promoted is whether some
// remote holds a copy of the branch — its remote-tracking ref exists —
// which is the only sense in which dockhand ever "knows" a branch was
// published.
func DecideRetire(promoted bool, pr PRFact) Retirement {
	switch {
	case !promoted:
		return RetireUnpromoted
	case !pr.Found:
		return RetireNoPR
	case pr.Merged:
		return RetireMerged
	case pr.Open:
		return RetireOpen
	default:
		return RetireClosed
	}
}

// Cleans reports whether this verdict deletes the branch. keepMerged is
// `cycle --keep-merged`: it withholds the deletion without changing the
// verdict, because what a merged PR means does not depend on whether the
// user asked to act on it. (`status` never asks this at all — it acts on
// no verdict, D27 — so the only caller is the pass that retires.)
func (d Retirement) Cleans(keepMerged bool) bool { return d == RetireMerged && !keepMerged }

// Reconciliation is the report's view of the same judgment, plus what
// happened when the pass acted on it. Two verbs render it (D27):
// `status`, which acts on nothing and names `cycle` where the verdict
// wants acting on, and `cycle`, which retires and says what it did —
// or why it did not. Both are pure functions of this value, so the
// wording of either can be pinned by a table test with no forge behind
// it.
type Reconciliation struct {
	Promoted bool
	// Minted says the branch is one dockhand minted — it lives under
	// git.BranchNamespace. Deletion stays inside that namespace whatever
	// a pull request did (D27's fold-in): a hand-made branch that
	// carries a verify note is shown and settled, and never removed by
	// anything here. It is a fact about the name, handed in by the
	// engine that knows the namespace, so that this judgment can word
	// the merged case without learning where branches come from.
	Minted bool
	// Unasked says no forge question was asked of this branch: `status
	// --no-update` reads the ledger and nothing else (D27), so the
	// pull request's state is unknown rather than absent, and a line
	// that read "no PR found" would be a lie.
	Unasked bool
	// Err is a lookup that could not answer — a broken remote table, a
	// forge that would not respond. It is reported rather than returned,
	// because one unreachable PR must not end a pass over every branch.
	Err string
	PR  PRFact
	// Cleaned says the branch was actually demolished, and CleanErr why
	// it was not when the verdict said it should have been.
	Cleaned  bool
	CleanErr string
	// Withheld says why a deletion the verdict called for, and the pass
	// was asked to perform, did not happen: `--keep-merged`, or a hold's
	// own words. Set only by the pass that retires — under `status` the
	// deletion is never asked for, so there is nothing to withhold and
	// the line names the verb instead. No kept case may be silent (D27):
	// a reader of `cycle`'s report must see why each merged branch is
	// still there.
	Withheld string
}

// Line is the branch's pull request line, worded for whichever verb
// produced it. The cases are ordered from "nothing to say" through the
// answers only one verb can reach: Unasked is `status --no-update`'s
// alone, Cleaned, CleanErr and Withheld are `cycle`'s alone, and the
// bare merged case is `status` naming the verb that acts.
//
// An unpromoted branch has nothing to say here at all, and says nothing.
func (r Reconciliation) Line() string {
	switch {
	case !r.Promoted:
		return ""
	case r.Unasked:
		return "promoted; PR not checked (--no-update)"
	case r.Err != "":
		return "PR state unavailable: " + r.Err
	case !r.PR.Found:
		return "promoted; no PR found"
	case r.CleanErr != "":
		return fmt.Sprintf("PR #%d merged; cleaning failed: %s", r.PR.Number, r.CleanErr)
	case r.Cleaned:
		return fmt.Sprintf("PR #%d merged — branch cleaned", r.PR.Number)
	case r.Withheld != "":
		return fmt.Sprintf("PR #%d merged — kept: %s", r.PR.Number, r.Withheld)
	case r.PR.Merged && !r.Minted:
		// The fold-in's one new sentence: the work landed and the branch
		// is somebody's own, so the pass that retires leaves it and says
		// so rather than naming a verb that would not touch it either.
		return fmt.Sprintf("PR #%d merged — not a dockhand branch, so nothing here removes it", r.PR.Number)
	case r.PR.Merged:
		// `status`: report the merge and name the verb that acts on it.
		// Load-bearing wording (D27): with the split nothing begins on
		// its own, and a merged branch nobody is told about is a branch
		// that stands forever.
		return fmt.Sprintf("PR #%d merged — `dockhand cycle` retires the branch", r.PR.Number)
	case r.PR.Open:
		return fmt.Sprintf("PR #%d open (%s)", r.PR.Number, r.PR.URL)
	default:
		return fmt.Sprintf("PR #%d closed without merging", r.PR.Number)
	}
}
