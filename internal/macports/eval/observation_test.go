package eval

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestObservationOwnsDeclarationsAndDoesNotLeakProfiles(t *testing.T) {
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/observed/Portfile", `PortSystem 1.0
name observed
version 1.0
master_sites https://example.invalid/$version:release
if {${os.major} >= 17} {
 revision 3
} else {
 revision 4
}
subport observed-child {
 revision 8
 checksums a.zip sha256 aaaa size 2
}
if {${build_arch} eq "arm64"} { distfiles a.zip:release } else { distfiles b.zip:release }
`)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "observed", Subport: "observed-child"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	native, err := e.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Modeled)
	require.Equal(t, native.Runtime, got.Snapshot.Runtime)
	require.Equal(t, "16", got.Snapshot.Platform.Version)
	o := got.Ports["observed-child"]
	require.Empty(t, o.Problems)
	require.Len(t, o.Distfiles, 1)
	require.Equal(t, macports.Distfile{Name: "b.zip", URLs: []string{"https://example.invalid/1.0/b.zip"}}, o.Distfiles[0])
	require.Len(t, o.Declarations, 3)
	require.Equal(t, "revision", o.Declarations[0].Command)
	require.Equal(t, []string{"4"}, o.Declarations[0].Values)
	require.Equal(t, "checksums", o.Declarations[2].Command)
	final, err := e.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	require.Equal(t, native.Ports, final.Ports)
	require.Equal(t, native.Platform, final.Platform)
	plain, err := e.Observe(t.Context(), bound, macports.ObservationRequest{})
	require.NoError(t, err)
	require.False(t, plain.Modeled)
	require.Empty(t, plain.Ports["observed-child"].Declarations)
	require.Equal(t, native.Ports, plain.Snapshot.Ports)
}

func TestObservedDeclarationsResolveNestedSource(t *testing.T) {
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	contents := `PortSystem 1.0
name fixture
version 1
subport fixture-child {
 revision 5
 checksums a.zip sha256 abcd size 2
}
`
	putFile(t, tree.Root(), "devel/fixture/Portfile", contents)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture", Subport: "fixture-child"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	o, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	require.NoError(t, err)
	for _, d := range o.Ports["fixture-child"].Declarations {
		cmd, err := portfile.LocateDeclaration([]byte(contents), filepath.Join(tree.Root(), "devel/fixture/Portfile"), d)
		require.NoError(t, err, "%+v", d)
		name, _ := cmd.Name([]byte(contents))
		require.Equal(t, d.Command, name)
	}
}
