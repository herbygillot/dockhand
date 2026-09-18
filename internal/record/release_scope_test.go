package record

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Verification coverage is an explicit intent: the initiating target alone,
// which must build something in the release, or every buildable member. An
// initiating target that builds nothing is an error, not every member.
func TestRequiredTargetsFollowTheCoverageIntent(t *testing.T) {
	t.Parallel()
	stub := Target{Name: "py-foo", Portfile: "python/py-foo/Portfile"}
	a := Target{Name: "py313-foo", Portfile: stub.Portfile, Subport: "py313-foo"}
	b := Target{Name: "py314-foo", Portfile: stub.Portfile, Subport: "py314-foo"}
	scope := &ReleaseScope{Input: ReleaseInput{Portfile: stub.Portfile}, Affected: []ReleaseMember{{Target: stub, MetadataOnly: true}, {Target: a}, {Target: b}}}
	targets, err := scope.RequiredTargets(CoverageInitiating, b)
	require.NoError(t, err)
	require.Equal(t, []Target{b}, targets)
	targets, err = scope.RequiredTargets(CoverageAll, b)
	require.NoError(t, err)
	require.Equal(t, []Target{a, b}, targets)
	_, err = scope.RequiredTargets(CoverageInitiating, stub)
	require.ErrorIs(t, err, ErrCoverage, "the stub builds nothing; widening to every member is refused")
	_, err = scope.RequiredTargets(CoverageIntent("sideways"), b)
	require.ErrorIs(t, err, ErrCoverage)
	spec := JobSpec{Targets: []Target{b}}
	require.Equal(t, CoverageInitiating, spec.Coverage())
	spec.AllSubports = true
	require.Equal(t, CoverageAll, spec.Coverage())
	targets, err = JobSpec{Targets: []Target{a}}.RequiredTargets(nil)
	require.NoError(t, err)
	require.Equal(t, []Target{a}, targets, "without a scope the job's own targets are required")
}
