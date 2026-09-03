package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
)

func changed(entries map[info.SubportKey][]info.FieldChange) info.Delta {
	return info.Delta{Changed: entries}
}

func key(subport string, variants info.VariantSet) info.SubportKey {
	return info.SubportKey{Subport: subport, Variants: variants}
}

func declineOf(t *testing.T, err error) *plan.Decline {
	t.Helper()
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	return d
}

func TestSubportsUnchangedCountsBothSides(t *testing.T) {
	require.NoError(t, SubportsUnchanged(info.Delta{}))

	d := declineOf(t, SubportsUnchanged(info.Delta{
		Added:   map[info.SubportKey]info.Values{key("a", ""): {}, key("b", ""): {}},
		Removed: map[info.SubportKey]info.Values{key("c", ""): {}},
	}))
	assert.Equal(t, plan.SubportsChanged, d.Type)
	assert.Equal(t, "2 added, 1 removed", d.Detail)
}

func TestOnlyFieldsAllowsTheIntentsOwnSet(t *testing.T) {
	may := map[info.Field]bool{info.FieldChecksums: true}
	d := changed(map[info.SubportKey][]info.FieldChange{
		key("jq", ""): {{Field: info.FieldChecksums, Old: []string{"a"}, New: []string{"b"}}},
	})
	require.NoError(t, OnlyFields(d, may))

	d.Changed[key("jq", "")] = append(d.Changed[key("jq", "")],
		info.FieldChange{Field: info.FieldVersion, Old: []string{"1"}, New: []string{"2"}})
	dec := declineOf(t, OnlyFields(d, may))
	assert.Equal(t, plan.UnexpectedChange, dec.Type)
	assert.Equal(t, "jq: version", dec.Detail)
}

// The sweep reads a map, and which entry it reaches first is the
// runtime's business. Which one it reports must not be: a message that
// varies between runs on identical input is a message nobody can test.
func TestOnlyFieldsNamesTheSameOffenderEveryTime(t *testing.T) {
	d := changed(map[info.SubportKey][]info.FieldChange{
		key("zeta", ""):    {{Field: info.FieldLicense}},
		key("alpha", ""):   {{Field: info.FieldHomepage}},
		key("alpha", "+x"): {{Field: info.FieldName}},
		key("mid", ""):     {{Field: info.FieldEpoch}},
	})
	for range 64 {
		dec := declineOf(t, OnlyFields(d, nil))
		require.Equal(t, "alpha: homepage", dec.Detail,
			"least by (subport, variants), which is the order a plan renders contexts in")
	}
}

func TestViaSetIsolatedRefusesASiblingThatMoved(t *testing.T) {
	own := changed(map[info.SubportKey][]info.FieldChange{
		key("jq", ""): {{Field: info.FieldChecksums}},
	})
	require.NoError(t, ViaSetIsolated(own, "jq"))

	withSibling := changed(map[info.SubportKey][]info.FieldChange{
		key("jq", ""):       {{Field: info.FieldChecksums}},
		key("jq-devel", ""): {{Field: info.FieldChecksums}},
	})
	dec := declineOf(t, ViaSetIsolated(withSibling, "jq"))
	assert.Equal(t, plan.UnexpectedChange, dec.Type)
	assert.Contains(t, dec.Detail, "jq-devel moved with it")
}

// Every key in a snapshot carries the frame the handle was taken
// through, so scoping by a key built with the zero frame finds nothing
// the moment a planner runs under a non-default one — and an intent
// that finds nothing reports that its edit reached nothing.
func TestOwnChangesIgnoresTheVariantFrame(t *testing.T) {
	d := changed(map[info.SubportKey][]info.FieldChange{
		key("jq", "+universal"):       {{Field: info.FieldVersion, New: []string{"1.8.2"}}},
		key("jq-devel", "+universal"): {{Field: info.FieldVersion, New: []string{"9.9"}}},
	})
	got := OwnChanges(d, "jq")
	require.Len(t, got, 1)
	assert.Equal(t, info.FieldVersion, got[0].Field)
	assert.Equal(t, []string{"1.8.2"}, got[0].New)

	assert.Empty(t, d.Changed[info.SubportKey{Subport: "jq"}],
		"the zero-frame index is the trap this helper exists to close")
	assert.Empty(t, OwnChanges(d, "absent"))
}

func TestOwnChangesConcatenatesFramesInOrder(t *testing.T) {
	d := changed(map[info.SubportKey][]info.FieldChange{
		key("jq", "+b"): {{Field: info.FieldRevision, New: []string{"2"}}},
		key("jq", "+a"): {{Field: info.FieldRevision, New: []string{"1"}}},
	})
	got := OwnChanges(d, "jq")
	require.Len(t, got, 2)
	assert.Equal(t, []string{"1"}, got[0].New, "frames in canonical order, so the result is stable")
	assert.Equal(t, []string{"2"}, got[1].New)
}
