package observe

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"

	"github.com/stretchr/testify/require"
)

func TestExplanationContinuesAfterFailureWithoutChangingTheVerdict(t *testing.T) {
	b := newBaseFixture(t)
	d := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	bad := macports.Declaration{Command: "dockhand.host-access", Values: []string{"unexplained", "closed", "portfile", "exec false"}}
	p := macports.PortObservation{ModeledHostAccess: true, Problems: []string{"unexplained", d.Values[0]}, Declarations: []macports.Declaration{bad, d}, BaseLibrary: b.lib}
	data := []byte("PortSystem 1.0\nname x\nversion 1\n")
	root := t.TempDir()
	want, failed := Tolerate(t.Context(), p, data, root)
	got := Explain(t.Context(), p, data, root)
	require.True(t, failed)
	require.Equal(t, want, got.Observation)
	require.Equal(t, failed, got.Inconclusive)
	require.Len(t, got.Accesses, 2)
	require.Equal(t, RuleNone, got.Accesses[0].Rule)
	require.Equal(t, RuleBuildDefault, got.Accesses[1].Rule)
	require.True(t, got.Accesses[1].Followers[0].Benign)
	require.True(t, p.ModeledHostAccess, "the input remains unchanged")
}

func TestExplanationIdentifiesAnAccessNotCountedByTheInput(t *testing.T) {
	p := macports.PortObservation{Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"not flagged"}}}}
	got := Explain(t.Context(), p, nil, t.TempDir())
	require.False(t, got.Inconclusive)
	require.Len(t, got.Accesses, 1)
	require.True(t, got.Accesses[0].Ignored)
	require.Equal(t, RuleNone, got.Accesses[0].Rule)
}
