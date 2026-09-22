package changeset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A contribution is one port directory, with shared files under _resources
// allowed beside it; anything else is refused with the reason as a predicate.
func TestScopeOfAllowsSharedResourcesBesideOnePort(t *testing.T) {
	t.Parallel()
	scope, err := ScopeOf([]string{"devel/a/Portfile", "devel/a/files/patch.diff", "_resources/port1.0/group/x-1.0.tcl"})
	require.NoError(t, err)
	require.Equal(t, Scope{Directory: "devel/a", Resources: true}, scope)
	require.Equal(t, "devel/a/Portfile", scope.Portfile())
	require.True(t, scope.Within("devel/a/files/other"))
	require.True(t, scope.Within("_resources/port1.0/livecheck/pypi.tcl"))
	require.False(t, scope.Within("devel/b/Portfile"))
	require.False(t, scope.Within("README"))
	scope, err = ScopeOf([]string{"devel/a/Portfile"})
	require.NoError(t, err)
	require.False(t, scope.Resources)
	for _, test := range []struct {
		paths []string
		want  string
	}{
		{[]string{"devel/a/Portfile", "devel/b/Portfile"}, "changes devel/a and devel/b; one port directory is supported"},
		{[]string{"_resources/port1.0/group/x-1.0.tcl"}, "changes only shared files under _resources, which names no port to prepare or verify"},
		{[]string{"devel/a/Portfile", "README"}, "changes README, which is outside a port directory"},
		{[]string{".github/workflows/main.yml", "devel/a/Portfile"}, "changes .github/workflows/main.yml, which is outside a port directory"},
		{[]string{"_resources"}, "changes _resources, which is outside a port directory"},
		{nil, "changes nothing"},
		{[]string{"devel/../a/Portfile"}, "is not a valid path"},
	} {
		_, err := ScopeOf(test.paths)
		require.ErrorIs(t, err, ErrScope, "%v", test.paths)
		require.ErrorContains(t, err, test.want)
	}
}
