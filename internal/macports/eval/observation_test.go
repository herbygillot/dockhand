package eval

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

func TestObservationOwnsDeclarationsAndDoesNotLeakProfiles(t *testing.T) {
	t.Parallel()
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
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
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
	// A native observation is modeled exactly when its runtime models the
	// host: never on a Mac, always elsewhere.
	require.Equal(t, native.Runtime.Modeled(), plain.Modeled)
	require.Empty(t, plain.Ports["observed-child"].Declarations)
	require.Equal(t, native.Ports, plain.Snapshot.Ports)
}

func TestObservedDeclarationsResolveNestedSource(t *testing.T) {
	t.Parallel()
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

func TestModeledObservationReportsHostDependentPortfile(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", `PortSystem 1.0
name host
version 1
set host_value [exec /usr/bin/true]
`)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "host"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	o, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, o.Ports["host"].ModeledHostAccess)
	require.NotEmpty(t, o.Ports["host"].Problems)
}

// Each source frame is sent once: a PortGroup command that appends a
// checksum for each row of a long list, as golang 1.0's go.vendors does,
// had the whole list's command on every append's stack, and opkssh's
// ~500 rows went past the RPC frame limit (field testing, 2026-10-23).
// These 600 rows sent about 27 MB that way.
func TestALongListsFramesAreSentOnce(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	var rows strings.Builder
	for i := range 600 {
		fmt.Fprintf(&rows, " \\\n    row%03d.tar.gz %064d", i, i)
	}
	putFile(t, tree.Root(), "devel/listed/Portfile", `PortSystem 1.0
name listed
version 1.0
proc listed_rows {args} {
 foreach {file sum} $args { checksums-append $file sha256 $sum }
}
listed_rows`+rows.String()+"\n")
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "listed"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Declarations: true})
	require.NoError(t, err)
	declarations := got.Ports["listed"].Declarations
	require.Len(t, declarations, 600)
	last := declarations[599]
	require.Equal(t, []string{"row599.tar.gz", "sha256", fmt.Sprintf("%064d", 599)}, last.Values)
	require.True(t, slices.ContainsFunc(last.Frames, func(frame macports.SourceFrame) bool {
		return strings.HasPrefix(frame.Command, "listed_rows") && strings.Contains(frame.Command, "row599.tar.gz")
	}), "each append keeps the list's command on its stack")
}
