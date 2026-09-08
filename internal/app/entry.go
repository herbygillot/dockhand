package app

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Hold, Unhold and Dismiss are ENTRY POINTS, not operations, and the
// distinction is the whole of the 2026-09-07 ruling on R22. An OPERATION
// is a type with a Run that sequences more than one lifecycle — Cancel
// stops a job, releases a lease and settles an attempt; Discard closes a
// change and demolishes a branch. Each of these advances exactly ONE
// lifecycle with exactly one mutator, so it sequences nothing.
//
// They exist anyway, three or four lines apiece, because the alternative
// was cli importing change and statestore to open an Amend of its own —
// and build step 10's done-criterion is that `go list` shows cli
// importing app and report and nothing else of the domain. That
// criterion is a MECHANICAL guard, checkable on every build forever,
// against the defect this restructure exists to remove: the shipped
// internal/cmd imports twelve domain packages, and "just one mutator" is
// how that started. A draft of this design read R22 as licensing the
// direct call, in app's own package doc and in three mutators' comments,
// while the build order forbade it one section away. R22 says what earns
// an operation; it never said cli may reach past app.
type Hold struct {
	State *statestore.Store
	Now   func() time.Time
}

// Run holds a change back. The reason and the owner ride into the record
// because a hold nobody can attribute is one nobody can lift with
// confidence.
func (e Hold) Run(ctx context.Context, id record.ChangeID, reason string, by record.OwnerID) error {
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		return change.HoldIn(tx, id, reason, by, e.Now())
	})
}

// Unhold lifts a hold, and refuses a change nothing is holding
// (change.ErrNotHeld) rather than succeeding quietly: the verb was asked
// to release something and there was nothing to release.
type Unhold struct {
	State *statestore.Store
	Now   func() time.Time
}

// Run lifts the hold.
func (e Unhold) Run(ctx context.Context, id record.ChangeID) error {
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		return change.UnholdIn(tx, id, e.Now())
	})
}

// Dismiss answers a proposal with no. It is the negative half of the pair
// whose positive half is bump-revision --for (the Accept operation), and
// until this file existed the negative answer had a verb and no shape.
type Dismiss struct {
	State *statestore.Store
	Now   func() time.Time
}

// Run records the dismissal over the candidates as amended. It is one
// mutator and one Amend: an answer is a fact about the finding, and
// nothing else moves when a person says no.
func (e Dismiss) Run(ctx context.Context, id record.ChangeID, kind record.FindingKind, d record.Disposition, candidates []record.Candidate) error {
	return e.State.Amend(ctx, func(tx *statestore.Txn) error {
		return change.AnswerIn(tx, id, kind, d, candidates, e.Now())
	})
}
