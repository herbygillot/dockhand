package info

import "slices"

// scalar lifts a scalar field into the uniform []string representation:
// one element, or nil when the field is absent.
func scalar(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// FieldChange records one field's movement within a surviving context. Old
// and New use the uniform representation: scalars are single-element, an
// absent field is nil.
type FieldChange struct {
	Field Field
	Old   []string
	New   []string
}

// Delta is the difference between two Snapshots, stated completely:
// contexts that appeared, contexts that vanished (each with their full
// Values as evidence), and per-field changes in the contexts common to
// both. Field changes are ordered canonically (Field order), so equal
// deltas compare equal deterministically.
//
// Delta is Portfile-scoped, like the snapshots it compares: Diff presumes
// both sides measure the same Portfile, and carries no provenance to check
// it — a snapshot is pure state, and knowing where two of them came from is
// the comparing layer's job (a plan records the base content hash for
// exactly this). Diffing snapshots of different ports is expressible and
// meaningless.
//
// Delta is lossless data; how strictly a predicted delta must match an
// observed one is the comparing checker's policy, not this type's. Keys
// carry the variant set, so snapshots taken under different variant frames
// diff visibly as additions and removals rather than being reconciled.
type Delta struct {
	Added   map[SubportKey]Values
	Removed map[SubportKey]Values
	Changed map[SubportKey][]FieldChange
}

// OtherContext reports the first context other than the named subport
// this delta touches, in any way — changed, added, or removed. The
// proof consumer for edits that must not reach siblings: an edit
// justified by one subport's evaluation and located outside its
// checksums block (a set-variable carrier) is only honest if no other
// context moved.
func (d Delta) OtherContext(subport string) (SubportKey, bool) {
	for key := range d.Changed {
		if key.Subport != subport {
			return key, true
		}
	}
	for key := range d.Added {
		if key.Subport != subport {
			return key, true
		}
	}
	for key := range d.Removed {
		if key.Subport != subport {
			return key, true
		}
	}
	return SubportKey{}, false
}

// Diff reports what changed from s to after.
func (s Snapshot) Diff(after Snapshot) Delta {
	var d Delta
	for k, before := range s {
		now, ok := after[k]
		if !ok {
			if d.Removed == nil {
				d.Removed = map[SubportKey]Values{}
			}
			d.Removed[k] = before
			continue
		}
		if changes := ChangesBetween(before, now); len(changes) > 0 {
			if d.Changed == nil {
				d.Changed = map[SubportKey][]FieldChange{}
			}
			d.Changed[k] = changes
		}
	}
	for k, now := range after {
		if _, ok := s[k]; !ok {
			if d.Added == nil {
				d.Added = map[SubportKey]Values{}
			}
			d.Added[k] = now
		}
	}
	return d
}

// ChangesBetween compares two Values field by field, in canonical
// (semanticTable) order. It is Diff's per-context comparison, exported
// for callers rendering one-sided context changes.
//
// IT READS ONLY THE SEMANTIC PART, and that is the whole of what a
// prediction is about. Context differs between a portdir and the shadow
// of it every prediction is made from, and Observed moves for reasons no
// intent declares — Values' doc comment states the rule and why each
// exclusion would refuse correct plans. The exclusion is a type here and
// not a memory: semanticTable is generated from Semantic, so a field
// this comparison does not see is a field that is not in Semantic.
func ChangesBetween(before, after Values) []FieldChange {
	var out []FieldChange
	for _, f := range semanticTable {
		old, now := f.get(before.Semantic), f.get(after.Semantic)
		if !slices.Equal(old, now) {
			out = append(out, FieldChange{Field: f.field, Old: old, New: now})
		}
	}
	return out
}

// Empty reports that nothing moved — the expected delta of a no-op, such
// as a whitespace-only edit.
func (d Delta) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// Equal reports whether two deltas describe the same difference. Nil and
// empty maps are the same absence.
func (d Delta) Equal(other Delta) bool {
	return valuesMapsEqual(d.Added, other.Added) &&
		valuesMapsEqual(d.Removed, other.Removed) &&
		changesMapsEqual(d.Changed, other.Changed)
}

func valuesMapsEqual(a, b map[SubportKey]Values) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || len(ChangesBetween(av, bv)) != 0 {
			return false
		}
	}
	return true
}

func changesMapsEqual(a, b map[SubportKey][]FieldChange) bool {
	if len(a) != len(b) {
		return false
	}
	for k, ac := range a {
		bc, ok := b[k]
		if !ok || !slices.EqualFunc(ac, bc, fieldChangeEqual) {
			return false
		}
	}
	return true
}

func fieldChangeEqual(a, b FieldChange) bool {
	return a.Field == b.Field && slices.Equal(a.Old, b.Old) && slices.Equal(a.New, b.New)
}
