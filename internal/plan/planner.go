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
// Nothing calls through this interface yet, deliberately — each command
// wires its intent concretely, and the first polymorphic caller will be
// the sweep, which is also the moment this signature gets its real
// test. Until then the interface exists for one purpose: the intents
// carry compile-time assertions against it, so the shape the first two
// converged on is enforced rather than merely observed, and a third
// intent that drifts is a build failure instead of a review comment.
type Planner interface {
	Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*Plan, error)
}
