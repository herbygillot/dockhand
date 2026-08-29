package info

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func k(sub string) SubportKey { return SubportKey{Subport: sub} }

func snap(entries map[string]Values) Snapshot {
	s := Snapshot{}
	for name, v := range entries {
		s[k(name)] = v
	}
	return s
}

func TestDiffIdenticalIsEmpty(t *testing.T) {
	a := snap(map[string]Values{"foo": {Name: "foo", Version: "1.0", Categories: []string{"devel"}}})
	d := a.Diff(a)
	require.True(t, d.Empty())
	require.True(t, d.Equal(Delta{}))
}

func TestDiffFieldChanges(t *testing.T) {
	before := snap(map[string]Values{"foo": {
		Name: "foo", Version: "1.4.1", Revision: "2",
		Depends: Depends{Lib: []string{"port:zlib"}},
	}})
	after := snap(map[string]Values{"foo": {
		Name: "foo", Version: "1.4.2", Revision: "0",
		Depends: Depends{Lib: []string{"port:zlib", "port:openssl"}},
	}})
	d := before.Diff(after)
	require.Empty(t, d.Added)
	require.Empty(t, d.Removed)
	require.Len(t, d.Changed, 1)

	changes := d.Changed[k("foo")]
	require.Equal(t, []FieldChange{
		{FieldVersion, []string{"1.4.1"}, []string{"1.4.2"}},
		{FieldRevision, []string{"2"}, []string{"0"}},
		{FieldDependsLib, []string{"port:zlib"}, []string{"port:zlib", "port:openssl"}},
	}, changes, "changes must appear in canonical field order")
}

func TestDiffAbsentToPresent(t *testing.T) {
	before := snap(map[string]Values{"foo": {Name: "foo", Version: "1.0"}})
	after := snap(map[string]Values{"foo": {Name: "foo", Version: "1.0", Epoch: "1"}})
	d := before.Diff(after)
	require.Equal(t, []FieldChange{{FieldEpoch, nil, []string{"1"}}}, d.Changed[k("foo")])
}

func TestDiffMembership(t *testing.T) {
	// The mp4ff case: a bump makes subports appear. Membership is a
	// first-class change, carrying full Values as evidence.
	before := snap(map[string]Values{
		"py-hid":    {Name: "py-hid", Version: "1.0"},
		"py39-hid":  {Name: "py39-hid", Version: "1.0"},
		"py310-hid": {Name: "py310-hid", Version: "1.0"},
	})
	after := snap(map[string]Values{
		"py-hid":    {Name: "py-hid", Version: "1.0"},
		"py310-hid": {Name: "py310-hid", Version: "1.0"},
		"py311-hid": {Name: "py311-hid", Version: "1.0"},
	})
	d := before.Diff(after)
	require.Empty(t, d.Changed)
	require.Len(t, d.Added, 1)
	require.Len(t, d.Removed, 1)
	require.Equal(t, "py311-hid", d.Added[k("py311-hid")].Name)
	require.Equal(t, "py39-hid", d.Removed[k("py39-hid")].Name)
}

func TestDiffVariantFramesAreVisible(t *testing.T) {
	// Snapshots taken under different variant frames do not reconcile:
	// the mismatch surfaces as membership churn, honestly.
	vs, err := Variants("+x11")
	require.NoError(t, err)
	before := Snapshot{SubportKey{Subport: "foo"}: {Name: "foo"}}
	after := Snapshot{SubportKey{Subport: "foo", Variants: vs}: {Name: "foo"}}
	d := before.Diff(after)
	require.Len(t, d.Added, 1)
	require.Len(t, d.Removed, 1)
	require.True(t, d.Empty() == false)
}

func TestDeltaEqual(t *testing.T) {
	before := snap(map[string]Values{"foo": {Name: "foo", Version: "1.0"}})
	after := snap(map[string]Values{"foo": {Name: "foo", Version: "2.0"}})
	d1 := before.Diff(after)
	d2 := before.Diff(after)
	require.True(t, d1.Equal(d2))
	require.False(t, d1.Equal(Delta{}))
	require.True(t, Delta{}.Equal(Delta{Added: map[SubportKey]Values{}}),
		"nil and empty maps are the same absence")

	// A predicted delta constructed by hand matches an observed one.
	predicted := Delta{Changed: map[SubportKey][]FieldChange{
		k("foo"): {{FieldVersion, []string{"1.0"}, []string{"2.0"}}},
	}}
	require.True(t, predicted.Equal(d1))
}

func TestFieldStringsSpeakMacPorts(t *testing.T) {
	require.Equal(t, "version", FieldVersion.String())
	require.Equal(t, "depends_lib", FieldDependsLib.String())
	require.Equal(t, "revision", FieldRevision.String())
}

func TestFieldTableCoversAllFields(t *testing.T) {
	// A field added to Values but not to the table would be invisible to
	// every diff. The enum and table must move together.
	require.Len(t, fieldTable, int(FieldDependsTest)+1)
	seen := map[Field]bool{}
	for _, f := range fieldTable {
		seen[f.field] = true
	}
	require.Len(t, seen, len(fieldTable), "duplicate field in table")
}
