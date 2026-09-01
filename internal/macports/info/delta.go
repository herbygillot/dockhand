package info

import "slices"

// Field identifies one field of Values. The set is closed, and String
// speaks MacPorts' own option names — these are the canonical field
// identifiers shared by everything that names port metadata.
type Field int

const (
	FieldName Field = iota
	FieldVersion
	FieldRevision
	FieldEpoch
	FieldCategories
	FieldLicense
	FieldMaintainers
	FieldPlatforms
	FieldDescription
	FieldHomepage
	FieldLongDescription
	FieldDistfiles
	FieldChecksums
	FieldDependsFetch
	FieldDependsExtract
	FieldDependsPatch
	FieldDependsBuild
	FieldDependsLib
	FieldDependsRun
	FieldDependsTest
)

func (f Field) String() string {
	switch f {
	case FieldName:
		return "name"
	case FieldVersion:
		return "version"
	case FieldRevision:
		return "revision"
	case FieldEpoch:
		return "epoch"
	case FieldCategories:
		return "categories"
	case FieldLicense:
		return "license"
	case FieldMaintainers:
		return "maintainers"
	case FieldPlatforms:
		return "platforms"
	case FieldDescription:
		return "description"
	case FieldHomepage:
		return "homepage"
	case FieldLongDescription:
		return "long_description"
	case FieldDistfiles:
		return "distfiles"
	case FieldChecksums:
		return "checksums"
	case FieldDependsFetch:
		return "depends_fetch"
	case FieldDependsExtract:
		return "depends_extract"
	case FieldDependsPatch:
		return "depends_patch"
	case FieldDependsBuild:
		return "depends_build"
	case FieldDependsLib:
		return "depends_lib"
	case FieldDependsRun:
		return "depends_run"
	case FieldDependsTest:
		return "depends_test"
	}
	return "unknown field"
}

// fieldTable is the single source of field extraction: Diff, Values
// equality, and any future field-addressed access all read Values through
// it. A field added to Values but not taught here would be invisible to
// every diff, which is why the table and the Field enum must move together.
var fieldTable = []struct {
	field Field
	get   func(Values) []string
}{
	{FieldName, func(v Values) []string { return scalar(v.Name) }},
	{FieldVersion, func(v Values) []string { return scalar(v.Version) }},
	{FieldRevision, func(v Values) []string { return scalar(v.Revision) }},
	{FieldEpoch, func(v Values) []string { return scalar(v.Epoch) }},
	{FieldCategories, func(v Values) []string { return v.Categories }},
	{FieldLicense, func(v Values) []string { return v.License }},
	{FieldMaintainers, func(v Values) []string { return v.Maintainers }},
	{FieldPlatforms, func(v Values) []string { return v.Platforms }},
	{FieldDescription, func(v Values) []string { return scalar(v.Description) }},
	{FieldHomepage, func(v Values) []string { return scalar(v.Homepage) }},
	{FieldLongDescription, func(v Values) []string { return scalar(v.LongDescription) }},
	{FieldDistfiles, func(v Values) []string { return v.Distfiles }},
	{FieldChecksums, func(v Values) []string { return v.Checksums }},
	{FieldDependsFetch, func(v Values) []string { return v.Depends.Fetch }},
	{FieldDependsExtract, func(v Values) []string { return v.Depends.Extract }},
	{FieldDependsPatch, func(v Values) []string { return v.Depends.Patch }},
	{FieldDependsBuild, func(v Values) []string { return v.Depends.Build }},
	{FieldDependsLib, func(v Values) []string { return v.Depends.Lib }},
	{FieldDependsRun, func(v Values) []string { return v.Depends.Run }},
	{FieldDependsTest, func(v Values) []string { return v.Depends.Test }},
}

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
// (fieldTable) order. It is Diff's per-context comparison, exported for
// callers rendering one-sided context changes.
func ChangesBetween(before, after Values) []FieldChange {
	var out []FieldChange
	for _, f := range fieldTable {
		old, now := f.get(before), f.get(after)
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
