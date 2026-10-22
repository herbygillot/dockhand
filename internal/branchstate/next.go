package branchstate

import "github.com/herbygillot/dockhand/internal/refusal"

// Next is what moves a branch on (refusal.Next), whose type lives below
// every layer that gives one (architecture, 2026-10-22): the verdict's,
// where the remedy depends on the branch's state.
type (
	Next  = refusal.Next
	Verb  = refusal.Verb
	After = refusal.After
)

// The afters a verdict names (refusal).
const (
	AfterLook  = refusal.AfterLook
	AfterCheck = refusal.AfterCheck
	AfterEdit  = refusal.AfterEdit
	AfterFork  = refusal.AfterFork
)
