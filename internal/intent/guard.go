package intent

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
)

// The guards are the questions every intent asks of its own predicted
// delta, in the order Finish asks them. They are exported because an
// intent that must ask one out of order — before an expensive step, or
// interleaved with a judgment only it can make — should ask the same
// question rather than write a fourth copy of it.

// SubportsUnchanged refuses a prediction in which evaluation contexts
// appear or disappear. No intent in the catalogue creates or destroys a
// subport, so either the edits did far more than they were asked to or
// the Portfile's structure is not what the planner read — and both are
// beyond what a prediction can honestly promise.
func SubportsUnchanged(predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &plan.Decline{Type: plan.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}
	return nil
}

// OnlyFields refuses a prediction that moves a field outside the set
// the intent is allowed to move. The set is the intent's own: the same
// field can be required by one intent and forbidden to another, which
// is why this takes the set rather than knowing one.
//
// When more than one context or field is out of bounds, the least by
// (subport, variants, field) is the one named. Which one is reported
// does not change the decision, but a message that varies between runs
// on identical input is a message nobody can test against, and the
// order chosen here is the one a plan already renders contexts in.
func OnlyFields(predicted info.Delta, may map[info.Field]bool) error {
	var (
		found bool
		at    info.SubportKey
		field info.Field
	)
	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if may[ch.Field] {
				continue
			}
			// Changes within a context arrive in canonical field order,
			// so the first offender in one is that context's least.
			if !found || keyLess(key, at) {
				found, at, field = true, key, ch.Field
			}
			break
		}
	}
	if !found {
		return nil
	}
	return &plan.Decline{Type: plan.UnexpectedChange,
		Detail: fmt.Sprintf("%s: %s", at.Subport, field)}
}

// ViaSetIsolated refuses a prediction in which any context but the
// named one moved, and exists for edits whose justification is one
// context's evaluation.
//
// A checksum located in a set variable stands outside any checksums
// command, so the corroboration that placed it is a single subport's
// evaluation — and two subports can record an identical value. The
// total shadow is the proof the aliasing hazard demands: with such an
// edit in play, no context but the edited one may move at all.
//
// Which sibling gets named when several moved is info.Delta's choice
// and varies between runs. The refusal does not.
func ViaSetIsolated(predicted info.Delta, contextName string) error {
	if key, moved := predicted.OtherContext(contextName); moved {
		return &plan.Decline{Type: plan.UnexpectedChange,
			Detail: fmt.Sprintf("a checksum edit landed in a set variable, and %s moved with it; the carrier is ambiguous", key.Subport)}
	}
	return nil
}

// OwnChanges collects what moved in the named context, across every
// variant frame the delta holds for it.
//
// It matches on the subport name and ignores the variant set, which is
// the only predicate that is correct. Every key in a snapshot carries
// the handle it was taken through, so within one delta the frame is
// constant and says nothing — while indexing the map by a key built
// with the zero frame silently misses every context the moment a
// planner runs under a non-default one, and reports that the edit
// reached nothing.
func OwnChanges(predicted info.Delta, contextName string) []info.FieldChange {
	keys := make([]info.SubportKey, 0, len(predicted.Changed))
	for key := range predicted.Changed {
		if key.Subport == contextName {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b info.SubportKey) int {
		switch {
		case keyLess(a, b):
			return -1
		case keyLess(b, a):
			return 1
		}
		return 0
	})
	var out []info.FieldChange
	for _, key := range keys {
		out = append(out, predicted.Changed[key]...)
	}
	return out
}

// keyLess orders evaluation contexts the way a plan renders them:
// subport first, then variant frame.
func keyLess(a, b info.SubportKey) bool {
	if a.Subport != b.Subport {
		return a.Subport < b.Subport
	}
	return a.Variants < b.Variants
}
