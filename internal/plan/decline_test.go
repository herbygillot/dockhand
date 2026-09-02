package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// The taxonomy ends where String stops naming members, which is the
// idiom the exit table already sweeps with. Every member must answer
// all three questions — what it is, what a machine calls it, and what
// the user does about it — so a type added later cannot ship with a
// blank remedy or an invented code.
func TestEveryDeclineTypeIsNamedCodedAndRemedied(t *testing.T) {
	const past = "unknown decline"
	require.Equal(t, past, DeclineType(1000).String())
	require.Equal(t, "unknown-decline", DeclineType(1000).Code())
	require.Empty(t, DeclineType(1000).Remedy())

	seen := map[string]bool{}
	for dt := AlreadyCurrent; dt.String() != past; dt++ {
		assert.NotEmpty(t, dt.Code(), "%s has no machine name", dt)
		assert.NotEqual(t, "unknown-decline", dt.Code(), "%s falls through Code", dt)
		assert.NotEmpty(t, dt.Remedy(), "%s has no remedy", dt)
		assert.False(t, seen[dt.Code()], "%q is claimed twice", dt.Code())
		seen[dt.Code()] = true
	}
	assert.Len(t, seen, 9, "the taxonomy is nine members; a change to it is a change to the contract")
}

// The sentence is the finding, then the detail, then the remedy — the
// shape every refusal in dockhand uses.
func TestDeclineErrorCarriesTheRemedy(t *testing.T) {
	d := &Decline{Type: AlreadyCurrent, Detail: "1.8.2"}
	assert.Equal(t,
		"plan: declined: already in the desired state: 1.8.2 — "+
			"nothing needs doing here; ask for a different state if this is not the one you meant",
		d.Error())

	bare := &Decline{Type: VendoredBlock}
	assert.Equal(t,
		"plan: declined: vendored dependency block requires regeneration — "+
			"regenerate the vendored block and commit that first; dockhand will not edit around it",
		bare.Error(), "no detail is a shorter sentence, not a missing remedy")
}

// A decline exits in the declined band, never the failure one.
func TestDeclineExitsDeclined(t *testing.T) {
	for dt := AlreadyCurrent; dt.String() != "unknown decline"; dt++ {
		assert.Equal(t, exitcode.PlanDeclined, (&Decline{Type: dt}).DockhandExit(), "%s", dt)
	}
}

// The withheld path: same outcome, different consequence, so a caller
// can tell the two apart without reading prose. Nothing populates
// Withheld yet — the riders arrive later — and the branch is pinned
// now so that arriving cannot quietly change the exit contract.
func TestDeclineWithheldRidersGetTheirOwnCode(t *testing.T) {
	d := &Decline{Type: AlreadyCurrent, Detail: "1.8.2", Withheld: []string{"cargo.crates"}}
	assert.Equal(t, exitcode.AlreadyCurrent, d.DockhandExit())
	assert.Equal(t, "declined", exitcode.Family(d.DockhandExit()), "a withheld decline is still a decline")

	assert.Equal(t, exitcode.PlanDeclined, (&Decline{Type: AlreadyCurrent, Withheld: nil}).DockhandExit())
	assert.Equal(t, exitcode.PlanDeclined,
		(&Decline{Type: VendoredBlock, Withheld: []string{"cargo.crates"}}).DockhandExit(),
		"only AlreadyCurrent splits; the others withhold nothing worth a code")
}

// The twin reads the decline through the interfaces exitcode declares,
// so a document's reason and its code come off the same error.
func TestDeclineAnswersTheTwin(t *testing.T) {
	tw := exitcode.TwinOf(&Decline{Type: ChecksumsNotLocated, Detail: "no literals"})
	assert.Equal(t, exitcode.Twin{Code: 10, Family: "declined", Reason: "checksums-not-located"}, tw)
}
