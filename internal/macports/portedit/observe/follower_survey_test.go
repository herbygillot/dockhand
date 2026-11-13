//go:build surveymeasure

package observe

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSurveyWithholdsOnlyTheFollowerAndChecksParity(t *testing.T) {
	b := newBaseFixture(t)
	d := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	p := macports.PortObservation{ModeledHostAccess: true, Problems: []string{d.Values[0]}, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib}
	on, off, err := CompareTolerance(t.Context(), p, []byte("build.args ${build.cmd}\n"), t.TempDir())
	require.NoError(t, err)
	require.False(t, on.Inconclusive)
	require.True(t, off.Inconclusive)
	require.Equal(t, RuleBaseLookup, on.Accesses[0].Rule)
	require.Equal(t, RuleNone, off.Accesses[0].Rule)
	require.False(t, off.Accesses[0].Followers[0].Benign)
	require.Equal(t, []string{"build.args", "build.cmd"}, on.Accesses[0].Followers[0].Watched)
}
