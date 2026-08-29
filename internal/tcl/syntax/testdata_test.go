package syntax

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixtureRoots names every directory of checked-in parser fixtures. The
// package's own testdata holds pure Tcl: sampled real-world scripts and
// files from Tcl's own test suite. The MacPorts corpus — Portfiles and
// PortGroups — lives with the MacPorts domain in macports/testdata, but the
// parser sweeps it too, by path: those files are this parser's production
// dialect, and reading fixtures across a package boundary is file access,
// not a dependency.
var fixtureRoots = map[string]string{
	"scripts":    filepath.Join("testdata", "scripts"),
	"tcltests":   filepath.Join("testdata", "tcltests"),
	"portfiles":  filepath.Join("..", "..", "macports", "testdata", "portfiles"),
	"portgroups": filepath.Join("..", "..", "macports", "testdata", "portgroups"),
}

// TestTestdata parses every fixture hermetically. The tiling invariant must
// hold on every file, and every file must parse without errors — the
// fixtures are all valid Tcl that tclsh accepts, so an error here is a
// parser bug, not a bad fixture.
func TestTestdata(t *testing.T) {
	for dir, root := range fixtureRoots {
		dir, root := dir, root
		t.Run(dir, func(t *testing.T) {
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.NotEmpty(t, entries, "no fixtures in %s", root)
			for _, e := range entries {
				src, err := os.ReadFile(filepath.Join(root, e.Name()))
				require.NoError(t, err)
				s, errs := Parse(src)
				verifyScript(t, src, s, 0, len(src))
				for _, pe := range errs {
					t.Errorf("%s/%s:%s", dir, e.Name(), pe.Describe(src))
				}
			}
		})
	}
}
