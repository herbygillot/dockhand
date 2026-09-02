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

// DecideRetire judges one branch's standing. promoted is whether the
// branch tracks a remote at all, which is the only sense in which
// dockhand ever "knows" a branch was published.
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

// Cleans reports whether this verdict deletes the branch. --no-clean
// withholds the deletion without changing the verdict, because what a
// merged PR means does not depend on whether the user asked to act on
// it.
func (d Retirement) Cleans(noClean bool) bool { return d == RetireMerged && !noClean }

// SweepLine is `dockhand clean`'s report for one branch: what happened,
// and for everything kept, why.
//
// contentLanded is read only on the merged verdict, and the caller
// computes it only there — it is several git calls per branch, and on
// any other verdict it would be work whose answer nobody reads and
// whose failure would turn a clean report into an error. The merged line
// is also written after the demolition succeeded, so it says "cleaned"
// as a fact rather than an intention.
func (d Retirement) SweepLine(pr PRFact, contentLanded bool) string {
	switch d {
	case RetireUnpromoted:
		return "kept — never promoted"
	case RetireNoPR:
		return "kept — promoted, but no PR found"
	case RetireMerged:
		line := fmt.Sprintf("cleaned — PR #%d merged", pr.Number)
		if !contentLanded {
			// Merged is the authority; differing bytes mean a committer
			// amended in flight or a later change superseded it — worth
			// saying, never worth keeping the branch for.
			line += " (upstream bytes differ: amended on merge, or since superseded)"
		}
		return line
	case RetireOpen:
		return fmt.Sprintf("kept — PR #%d open (%s)", pr.Number, pr.URL)
	case RetireClosed:
		return fmt.Sprintf("kept — PR #%d closed without merging; rejection is information", pr.Number)
	}
	return ""
}

// Reconciliation is status's view of the same judgment, plus what
// happened when status acted on it. Status is a report that cleans as a
// side effect, so it has two outcomes clean does not: a lookup that
// could not answer at all, and a demolition that failed after the
// verdict was reached.
type Reconciliation struct {
	Promoted bool
	// Err is a lookup that could not answer — a broken remote table, a
	// forge that would not respond. It is reported rather than returned,
	// because one unreachable PR must not end a sweep over every branch.
	Err string
	PR  PRFact
	// Cleaned says the branch was actually demolished, and CleanErr why
	// it was not when the verdict said it should have been.
	Cleaned  bool
	CleanErr string
}

// Line is status's wording. It is deliberately not clean's: status
// reports and cleans in passing, so its merged line says the deletion
// happened to a branch the reader was asking about, while clean's says
// the sweep did it. Both are golden-pinned in their own verb's output.
//
// An unpromoted branch has nothing to say here at all, and says nothing.
func (r Reconciliation) Line() string {
	switch {
	case !r.Promoted:
		return ""
	case r.Err != "":
		return "PR state unavailable: " + r.Err
	case !r.PR.Found:
		return "promoted; no PR found"
	case r.CleanErr != "":
		return fmt.Sprintf("PR #%d merged; cleaning failed: %s", r.PR.Number, r.CleanErr)
	case r.Cleaned:
		return fmt.Sprintf("PR #%d merged — branch cleaned", r.PR.Number)
	case r.PR.Merged:
		// --no-clean: report the merge, withhold the deletion.
		return fmt.Sprintf("PR #%d merged — `dockhand clean` removes the branch", r.PR.Number)
	case r.PR.Open:
		return fmt.Sprintf("PR #%d open (%s)", r.PR.Number, r.PR.URL)
	default:
		return fmt.Sprintf("PR #%d closed without merging", r.PR.Number)
	}
}
