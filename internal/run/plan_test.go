package run

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// A KNOWN_FAIL PORT DECLINES HERE, IN SECONDS, WITHOUT BOOTING A VM.
// mpbb's list-time exclusion, borrowed: evaluation answers in a second
// where a VM takes an hour, and the answer is RECORDED rather than
// skipped, because a platform a port refuses is a real verdict about
// that platform.
func TestPlanDeclinesAKnownFailPortBeforeAnyVMBoots(t *testing.T) {
	spec := specOf("jq")
	_, runs, err := Plan(spec, map[string]Preflight{
		"jq": {Read: true, KnownFail: true, Reason: "no arm64 support upstream"},
	})
	require.ErrorIs(t, err, ErrNothingToBuild)
	require.Len(t, runs, 1)
	assert.Equal(t, record.Unsupported, runs["jq"].State)
	assert.Contains(t, runs["jq"].Detail, "declares known_fail on Sequoia")
	assert.Contains(t, runs["jq"].Detail, "no arm64 support upstream")
	assert.True(t, runs["jq"].State.Terminal(), "the judge must not write over it")
}

// A PREFLIGHT THAT COULD NOT BE READ IS NOT A DECLINE. Preflight.Read is
// rule 7 on that struct, and without it an unreadable Portfile would
// spend a VM and come back FAILED — the defect the stage exists to
// close, reached through an I/O failure instead of a forgotten call.
func TestPlanBuildsAMemberWhosePreflightCouldNotBeRead(t *testing.T) {
	req, runs, err := Plan(specOf("jq"), map[string]Preflight{
		"jq": {Read: false, Err: assertErr{}},
	})
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Equal(t, []string{"jq"}, req.Ports)
}

type assertErr struct{}

func (assertErr) Error() string { return "the staged Portfile could not be read" }

// A MEMBER THAT DECLINES IS LEFT OUT AND THE REST STILL BUILD: a
// known_fail on one port is a fact about that port.
func TestPlanBuildsTheMembersThatDidNotDecline(t *testing.T) {
	spec := specOf("libwidget", "gdal")
	req, runs, err := Plan(spec, map[string]Preflight{
		"libwidget": {Read: true},
		"gdal":      {Read: true, KnownFail: true},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"libwidget"}, req.Ports)
	require.Len(t, runs, 1)
	assert.Equal(t, record.Unsupported, runs["gdal"].State)
}

// THE WITHHELD MEMBERS GET THEIR ANSWER TOO. Left unrecorded, a later
// road's walk would find no run saying so and seat them beside the
// sibling they conflict with.
func TestPlanRecordsTheWithheldMembersAsWithheld(t *testing.T) {
	spec := specOf("libwidget")
	spec.Withheld = []Withheld{{Port: "gdal-devel", Why: "conflicts with gdal"}}
	_, runs, err := Plan(spec, map[string]Preflight{"libwidget": {Read: true}})
	require.NoError(t, err)
	require.Contains(t, runs, "gdal-devel")
	assert.Equal(t, record.Withheld, runs["gdal-devel"].State)
	assert.Equal(t, "conflicts with gdal", runs["gdal-devel"].Detail)
}

// THE PARALLEL SLICES ARE BUILT OVER WHAT IS ACTUALLY BEING BUILT. A
// member the preflight threw out is not in the request, and a graph
// drawn over the roster would shift every index after it and hand the
// provider one member's prerequisites under another member's name.
func TestPlanNarrowsEveryParallelSliceToWhatIsBuilt(t *testing.T) {
	spec := specOf("libwidget", "broken", "gdal")
	spec.FromSource = []string{"libwidget", "broken"}
	spec.Requires = [][]string{nil, {"libwidget"}, {"libwidget", "broken"}}
	spec.Roster[2].Forced = "gdal-devel"

	req, _, err := Plan(spec, map[string]Preflight{
		"libwidget": {Read: true},
		"broken":    {Read: true, KnownFail: true},
		"gdal":      {Read: true, NeedsXcode: true},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"libwidget", "gdal"}, req.Ports)
	assert.Equal(t, []string{"libwidget"}, req.FromSource,
		"a member that is not in the guest must not be named in the argv")
	require.Len(t, req.Requires, 2)
	assert.Empty(t, req.Requires[0])
	assert.Equal(t, []string{"libwidget"}, req.Requires[1],
		"gdal's edge to the declined member is dropped and its edge to libwidget kept")
	require.Len(t, req.Deactivate, 2)
	assert.Empty(t, req.Deactivate[0], "an ordinary member deactivates nothing")
	assert.Equal(t, "gdal-devel", req.Deactivate[1])
	assert.True(t, req.NeedsXcode, "one member needing a full Xcode is the whole guest needing it")
}

// AN ORDINARY SUBMISSION SEES EXACTLY THE REQUEST IT ALWAYS SAW: no
// deactivation, and no graph where nothing declared one.
func TestPlanLeavesTheOrdinaryRequestUntouched(t *testing.T) {
	req, runs, err := Plan(specOf("jq"), map[string]Preflight{"jq": {Read: true}})
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Nil(t, req.Deactivate)
	assert.Nil(t, req.Requires)
	assert.Nil(t, req.FromSource)
	assert.Equal(t, sequoia, req.Platform)
}
