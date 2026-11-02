package eval

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// lintPortfile is a port whose lint says one error, an invalid checksum,
// and warnings of its own lines, and none of its dependency, which an
// evaluation's tree has no index to look up.
const lintPortfile = `# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4
PortSystem          1.0

name                linted
version             1.0
categories          textproc
maintainers         {@herbygillot gmail.com:herby.gillot} openmaintainer
license             MIT
description         A port lint runs on
long_description    {*}${description}
homepage            https://example.invalid
master_sites        https://example.invalid/
depends_lib         port:zlib
checksums           rmd160  0 \
                    sha256  0 \
                    size    1
`

func lintSource(t *testing.T, portfile string) macports.Context {
	t.Helper()
	root := t.TempDir()
	putFile(t, root, "textproc/linted/Portfile", portfile)
	tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("b", 40))}, root, model.Platform{})
	require.NoError(t, err)
	source, err := tree.Select(model.Target{Name: "linted", Portfile: "textproc/linted/Portfile"})
	require.NoError(t, err)
	return source
}

// TestLintRunsBaseLintThroughTheDispatcher is the roadmap's one unknown
// answered: mportexec's lint target runs in the evaluator's interpreter,
// its worker guarded as an evaluation's, and says what port lint says.
func TestLintRunsBaseLintThroughTheDispatcher(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	lint, err := evaluator.Lint(t.Context(), lintSource(t, lintPortfile), false)
	require.NoError(t, err)
	require.Contains(t, lint.Errors, "checksum type rmd160, but checksum is invalid: 0")
	require.Contains(t, lint.Warnings, "checksum type is insecure on its own: size")
	require.NotContains(t, lint.Warnings, "Line 2 should be a newline (after modeline)")
	for _, line := range append(lint.Errors, lint.Warnings...) {
		require.NotContains(t, line, "zlib")
		require.NotContains(t, line, "index")
	}
	nitpicked, err := evaluator.Lint(t.Context(), lintSource(t, lintPortfile), true)
	require.NoError(t, err)
	require.Subset(t, nitpicked.Warnings, lint.Warnings)
	require.Contains(t, nitpicked.Warnings, "Line 2 should be a newline (after modeline)")
}
