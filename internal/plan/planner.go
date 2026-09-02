package plan

import (
	"context"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/port"
)

// Planner is the shape every intent has: parameters in the value, one
// Plan call spending everything at plan time, a *Plan out or a *Decline
// explaining why not.
//
// One caller calls through it: cmd's intentAction, the road every
// write intent travels — resolve, plan, summarize, gate, realize —
// with the Planner as the only thing that varies. The intents also
// carry compile-time assertions against it, so the shape the first
// two converged on is enforced rather than merely observed, and a
// third intent that drifts is a build failure instead of a review
// comment. A sweep would be the second polymorphic caller, not the
// first.
type Planner interface {
	Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*Plan, error)
}
