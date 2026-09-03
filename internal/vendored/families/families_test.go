package families

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// blockOf maps each field of info.Vendored to the block kind it holds.
// The mapping is written out rather than derived because the field name
// and the option name are different spellings on purpose (GoVendors is
// go.vendors), and a derivation would have to guess at the next one.
var blockOf = map[string]vendored.Kind{
	"GoVendors":         vendored.GoVendors,
	"CargoCrates":       vendored.CargoCrates,
	"CargoCratesGithub": vendored.CargoCratesGithub,
}

// unregistered names the blocks that deliberately have no family of
// their own, with the reason each is covered anyway. A block reaching
// this list is a decision; a block reaching neither this list nor a
// registration is a port dockhand would edit around, which is the one
// thing the vendored boundary exists to prevent.
var unregistered = map[vendored.Kind]string{
	vendored.CargoCratesGithub: "the cargo family's sibling block: cargo.Blocks locates, subtracts and regenerates it beside cargo.crates from the one Cargo.lock, so a registration of its own would run the generator twice",
}

// The evaluated shape is the domain: a vendored field info knows about
// and no family claims is a block a bump would edit around, pinning the
// old version's dependency tree under a new version's source. So the
// struct is enumerated rather than the registry, and every field must
// end up in one of the two lists.
func TestEveryVendoredFieldHasAFamilyOrAReason(t *testing.T) {
	fields := reflect.TypeOf(info.Vendored{})
	require.Positive(t, fields.NumField())

	registered := map[vendored.Kind]bool{}
	for _, r := range All() {
		registered[r.Kind()] = true
	}

	for i := range fields.NumField() {
		name := fields.Field(i).Name
		k, known := blockOf[name]
		require.True(t, known, "info.Vendored.%s names no block kind: add it to blockOf, then register a family for it or say why it needs none", name)

		reason, skipped := unregistered[k]
		switch {
		case registered[k] && skipped:
			t.Errorf("%s is both registered and listed as needing no registration", k)
		case registered[k]:
		case skipped:
			assert.NotEmpty(t, reason, "%s is unregistered without a reason", k)
		default:
			t.Errorf("%s has no family and no reason to lack one", k)
		}
	}
}

// The other direction: a registration for a block info cannot report is
// a family that will never be Present, which is a registry lying about
// its own coverage.
func TestEveryRegistrationNamesAKnownBlock(t *testing.T) {
	known := map[vendored.Kind]bool{}
	for _, k := range blockOf {
		known[k] = true
	}
	for _, r := range All() {
		assert.True(t, known[r.Kind()], "%s is registered but is no field of info.Vendored", r.Kind())
	}
}

// The registry is handed out fresh so a caller cannot reach the next
// caller's copy: bump sorts nothing today, but a shared package-level
// slice is one append away from being everyone's.
func TestAllHandsOutASeparateSlice(t *testing.T) {
	a, b := All(), All()
	require.Len(t, a, len(b))
	assert.NotSame(t, &a[0], &b[0])
}

// go2port's refusal is the family's own sentence, and it must survive
// the crossing into the plan's vocabulary word for word — the rendered
// line is what a person reads when a refresh meets a go.vendors port.
func TestGoVendorsRefusesARefreshInTheFamilysWords(t *testing.T) {
	vals := info.Values{Vendored: info.Vendored{GoVendors: "go.vendors github.com/x/y 1.0 abc"}}
	var reasons []string
	for _, r := range All() {
		if !r.Present(vals) {
			continue
		}
		if reason, veto := r.VetoRefresh(vals); veto {
			reasons = append(reasons, reason)
		}
	}
	require.Len(t, reasons, 1)
	assert.Equal(t,
		"go.vendors pins module bytes a refresh never fetches, so the block would go on vouching for what upstream replaced",
		reasons[0])

	d := &plan.Decline{Type: plan.VendoredBlock, Detail: reasons[0]}
	assert.Equal(t,
		"plan: declined: vendored dependency block requires regeneration: "+
			"go.vendors pins module bytes a refresh never fetches, so the block would go on vouching for what upstream replaced"+
			" — regenerate the vendored block and commit that first; dockhand will not edit around it",
		d.Error())
}

// A cargo block does not stop a refresh: the crates are other bytes
// from another place, and the port's own distfile is the only thing
// being re-hashed.
func TestCargoDoesNotRefuseARefresh(t *testing.T) {
	vals := info.Values{Vendored: info.Vendored{CargoCrates: "libc 0.2.156 a5f43f1"}}
	for _, r := range All() {
		if !r.Present(vals) {
			continue
		}
		reason, veto := r.VetoRefresh(vals)
		assert.False(t, veto, "%s refused a refresh: %s", r.Kind(), reason)
	}
}

// The translation is the reason this package exists, so it is proved
// rather than assumed: a family's refusal arrives at the intent as the
// planner's, which is what decides the exit code.
func TestAFamilysDeclineArrivesAsThePlansOwn(t *testing.T) {
	err := declined(&vendored.Decline{Kind: vendored.CargoCratesGithub, Detail: "thing is pinned by a tag"})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.VendoredBlock, d.Type)
	assert.Equal(t, "thing is pinned by a tag", d.Detail, "the family's sentence travels verbatim")
}

// Everything that is not a refusal stays what it was. A malformed block
// and an absent generator are failures of the run, and turning either
// into a decline would report a broken machine as a judgment about the
// port.
func TestOtherErrorsCrossUnchanged(t *testing.T) {
	err := fmt.Errorf("reading it: %w", vendored.ErrNoGenerator)
	got := declined(err)
	assert.Equal(t, err, got)
	require.ErrorIs(t, got, vendored.ErrNoGenerator)
	assert.NotErrorAs(t, got, new(*plan.Decline))
}

// Nothing is what a family with nothing to say returns, and the wrapper
// must not invent an error out of it.
func TestNilStaysNil(t *testing.T) {
	assert.NoError(t, declined(nil))
}
