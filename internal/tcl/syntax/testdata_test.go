package syntax

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
)

// fixtureRoots names this package's own directories of checked-in
// fixtures, which hold pure Tcl: sampled real-world scripts and files
// from Tcl's own test suite.
//
// The MacPorts corpus — Portfiles and PortGroups — is swept too, because
// those files are this parser's production dialect, but it is asked for
// by name rather than by path. Where the corpus lives is testenv's fact,
// and a "../.." spelled here is a path that breaks the day either
// package moves.
var fixtureRoots = map[string]string{
	"scripts":  filepath.Join("testdata", "scripts"),
	"tcltests": filepath.Join("testdata", "tcltests"),
}

// TestTestdata parses every fixture hermetically. The tiling invariant must
// hold on every file, and every file must parse without errors — the
// fixtures are all valid Tcl that tclsh accepts, so an error here is a
// parser bug, not a bad fixture.
func TestTestdata(t *testing.T) {
	for dir, root := range fixtureRoots {
		t.Run(dir, func(t *testing.T) {
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.NotEmpty(t, entries, "no fixtures in %s", root)
			for _, e := range entries {
				src, err := os.ReadFile(filepath.Join(root, e.Name()))
				require.NoError(t, err)
				parses(t, dir, e.Name(), src)
			}
		})
	}
	t.Run("portfiles", func(t *testing.T) {
		names := testenv.Portfiles(t)
		require.NotEmpty(t, names, "no Portfiles in the corpus")
		for _, name := range names {
			parses(t, "portfiles", name, testenv.Portfile(t, name))
		}
	})
	t.Run("portgroups", func(t *testing.T) {
		names := testenv.Portgroups(t)
		require.NotEmpty(t, names, "no PortGroups in the corpus")
		for _, name := range names {
			parses(t, "portgroups", name, testenv.Portgroup(t, name))
		}
	})
}

// parses holds one fixture to both promises: the parse tiles the source
// exactly, and it reports no errors.
func parses(t *testing.T, dir, name string, src []byte) {
	t.Helper()
	s, errs := Parse(src)
	verifyScript(t, src, s, 0, len(src))
	for _, pe := range errs {
		t.Errorf("%s/%s:%s", dir, name, pe.Describe(src))
	}
}
