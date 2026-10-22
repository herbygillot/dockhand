package branchstate

import "github.com/herbygillot/dockhand/internal/model"

// Bucket is where a branch stands in status's overview (part 5, item 4):
// it needs you, it's under way, it waits on someone else, or its work is
// over.
type Bucket int

const (
	// BucketNeedsYou is a branch with something for the person to do or
	// look at: a row of Attend's.
	BucketNeedsYou Bucket = iota
	// BucketWorking is a branch being edited, checked, or not yet
	// submitted, that asks nothing of the person now.
	BucketWorking
	// BucketWaiting is a branch whose open pull request waits on
	// reviewers, or on its merge, with nothing for the person: its CI
	// failing or a review asking for changes is a row of Attend's.
	BucketWaiting
	// BucketDone is a branch merged, closed, archived, or cleaned.
	BucketDone
)

// String is the bucket's name, as status's JSON gives it.
func (b Bucket) String() string {
	return [...]string{"needs-you", "working", "waiting", "done"}[b]
}

// BucketOf is the bucket a branch's facts put it in.
func BucketOf(facts Facts) Bucket {
	b := facts.Branch
	switch {
	case len(Attend(facts)) > 0:
		return BucketNeedsYou
	case b.Cleaned || b.Record.State != model.BranchOpen:
		return BucketDone
	}
	if pr := b.Record.PullRequest; pr != nil {
		if pr.Observed != nil && pr.Observed.State != model.PullRequestOpen {
			return BucketDone
		}
		return BucketWaiting
	}
	return BucketWorking
}

// Working is what moves a branch on that asks nothing of the person now:
// following its check, or checking what isn't checked yet; none where
// there's nothing to do yet, as a branch with no edits.
func Working(facts Facts) (Next, bool) {
	b := facts.Branch
	name := b.Record.ShortName()
	switch facts.Check.Standing() {
	case Checking:
		run := facts.Check.Checking()
		return Next{Verb: VerbWait, Args: []string{run.Name()}, Branch: name}, true
	case NotChecked:
		if b.Commits > 0 || len(b.Edited) > 0 {
			return Next{Verb: VerbCheck, Branch: name}, true
		}
	}
	return Next{}, false
}
