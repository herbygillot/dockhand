// Package families is the registry of vendored-block families, and the
// one place a family's refusal becomes a plan's.
//
// Two things live here rather than in a planner. The list itself,
// because both intents consult it: a bump regenerates the blocks a
// version move invalidates, and a refresh asks the same families
// whether re-hashing the port's own distfiles beside their block can be
// honest. A registry owned by one intent is a registry the other has to
// reach into, which is how the second copy of a rule gets written.
//
// And the translation. The family packages hold a block's format and
// nothing above it — depguard denies them internal/plan, so a generator
// does not drag the planning layer in behind it — so each raises a
// vendored.Decline in its own words. Mapping that into the plan's
// vocabulary is a boundary, and a boundary crossed in one place is a
// boundary; crossed at every call site it is a convention. The
// Regenerators handed out here have already crossed it, which is why an
// intent iterating this registry writes no mapping of its own.
package families

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/cargo"
	"github.com/herbygillot/dockhand/internal/vendored/go2port"
)

// All lists every vendored-block family, in the order an intent
// consults them. The order is not load-bearing today — a port carrying
// two families' blocks does not exist in the tree — and the list is
// rebuilt per call rather than shared, so a caller that sorts or
// filters it cannot reach the next caller's copy.
//
// The next family is a line here. That is the whole promise the
// Regenerator contract was written for.
func All() []vendored.Regenerator {
	return []vendored.Regenerator{
		declining{cargo.Blocks{}},
		declining{go2port.Blocks{}},
	}
}

// declining is one family with its refusals translated. Only the two
// error-returning methods need wrapping: Veto and VetoRefresh answer
// with a reason, and the intent that asked words its own decline around
// it.
type declining struct{ vendored.Regenerator }

// Supplied names the block's distfiles, or says why it cannot.
func (d declining) Supplied(ctx context.Context, rc vendored.Regen) ([]string, error) {
	out, err := d.Regenerator.Supplied(ctx, rc)
	return out, declined(err)
}

// Regenerate rebuilds the block, or declines in the plan's vocabulary.
func (d declining) Regenerate(ctx context.Context, rc vendored.Regen) ([]edit.Edit, error) {
	out, err := d.Regenerator.Regenerate(ctx, rc)
	return out, declined(err)
}

// declined maps a family's refusal into the planner's, and leaves every
// other error alone. The type is what a caller branches on — a decline
// is a judgment about the port and exits 10, a failure is a failure —
// so an untranslated vendored.Decline would not be a wrong message but
// a wrong outcome, which is why the translation is not optional and not
// spread out.
//
// The detail is carried verbatim: the family chose that sentence
// because the family is what knows why the block cannot be honest.
func declined(err error) error {
	var d *vendored.Decline
	if errors.As(err, &d) {
		return &plan.Decline{Type: plan.VendoredBlock, Detail: d.Detail}
	}
	return err
}
