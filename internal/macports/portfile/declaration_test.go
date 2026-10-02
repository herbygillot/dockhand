package portfile_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/textedit"
	"github.com/stretchr/testify/require"
)

func TestRewriteLiteralDeclarationReplacesTheOneCarrier(t *testing.T) {
	t.Parallel()
	src := []byte("go.toolchain_min 1.22\nsubport x { go.toolchain_min ${v} }\n")
	out, err := portfile.RewriteLiteralDeclaration(src, "go.toolchain_min", "1.22", "1.24")
	require.NoError(t, err)
	require.Equal(t, "go.toolchain_min 1.24\nsubport x { go.toolchain_min ${v} }\n", string(out), "only the literal carrier changes")
	_, err = portfile.RewriteLiteralDeclaration(src, "go.toolchain_min", "1.23", "1.24")
	require.ErrorIs(t, err, portfile.ErrUnsupported, "a value not carried by one literal is refused")
	_, err = portfile.RewriteLiteralDeclaration([]byte("go.toolchain_min 1.22\ngo.toolchain_min 1.22\n"), "go.toolchain_min", "1.22", "1.24")
	require.ErrorIs(t, err, portfile.ErrUnsupported, "two carriers are refused too")
}

// A declaration a variant's body makes has no file on Tcl's stack, which
// names the variant's procedure and then the command, as the evaluator
// recorded git's +doc checksums-append. It's located by its text in that
// variant's body alone (the git run's finding 1).
func TestADeclarationInAVariantIsLocatedInItsBody(t *testing.T) {
	src := []byte(`PortSystem 1.0
name observed
checksums a.tar.gz sha256 aaaa size 2
variant doc description {docs} {
    if {${name} eq ${subport}} {
        distfiles-append    b.tar.gz
        checksums-append    b.tar.gz \
                            sha256  bbbb \
                            size    3
    }
}
variant other description {other} {
    checksums-append    b.tar.gz  sha256  bbbb  size    3
}
`)
	declared := func(variant, command string) macports.Declaration {
		return macports.Declaration{Command: "checksums-append", Values: []string{"b.tar.gz", "sha256", "bbbb", "size", "3"}, Frames: []macports.SourceFrame{
			{Line: 1, Command: "eval_variants variations"},
			{File: "/opt/local/libexec/macports/lib/port1.0/portutil.tcl", Line: 2051, Command: "catch \"variant-${vname}\" result"},
			{Line: 1, Command: "builtin_catch variant-" + variant + " result"},
			{Line: 1, Command: "variant-" + variant},
			{Line: 5, Command: command},
			{Line: 1, Command: "::dockhand_observation::record {checksums-append b.tar.gz sha256 bbbb size 3} enter"},
		}}
	}
	cmd, err := portfile.LocateDeclaration(src, "/tree/devel/observed/Portfile", declared("doc", "checksums-append    b.tar.gz  sha256  bbbb  size    3"))
	require.NoError(t, err)
	line, _ := textedit.Position(src, cmd.Span.Start)
	require.Equal(t, 7, line, "doc's own, not other's of the same text")
	cmd, err = portfile.LocateDeclaration(src, "/tree/devel/observed/Portfile", declared("other", "checksums-append    b.tar.gz  sha256  bbbb  size    3"))
	require.NoError(t, err)
	line, _ = textedit.Position(src, cmd.Span.Start)
	require.Equal(t, 13, line)

	_, err = portfile.LocateDeclaration(src, "/tree/devel/observed/Portfile", declared("doc", "add_docs b.tar.gz"))
	require.ErrorContains(t, err, "variant doc makes the declaration through another procedure")
	_, err = portfile.LocateDeclaration(src, "/tree/devel/observed/Portfile", declared("absent", "checksums-append    b.tar.gz  sha256  bbbb  size    3"))
	require.ErrorContains(t, err, "the Portfile defines variant absent 0 times")
	twice := append(src, []byte("variant doc description {again} {\n    checksums-append    b.tar.gz  sha256  bbbb  size    3\n}\n")...)
	_, err = portfile.LocateDeclaration(twice, "/tree/devel/observed/Portfile", declared("doc", "checksums-append    b.tar.gz  sha256  bbbb  size    3"))
	require.ErrorContains(t, err, "the Portfile defines variant doc 2 times")
	_, err = portfile.LocateDeclaration(src, "/tree/devel/observed/Portfile", macports.Declaration{Command: "checksums-append", Frames: []macports.SourceFrame{{Line: 1, Command: "some_proc"}}})
	require.ErrorContains(t, err, "declaration has no source location in the Portfile", "no variant on the stack")
}

func TestAChecksumsBlockIsWrittenAsTheGuideLaysItOut(t *testing.T) {
	require.Equal(t, `checksums           git-2.56.0.tar.xz \
                    rmd160  aaaa \
                    sha256  bbbb \
                    size    8177180 \
                    git-htmldocs-2.56.0.tar.xz \
                    rmd160  cccc \
                    sha256  dddd \
                    size    1713768`, portfile.ChecksumsBlock([]portfile.Checksum{
		{Name: "git-2.56.0.tar.xz", RMD160: "aaaa", SHA256: "bbbb", Size: 8177180},
		{Name: "git-htmldocs-2.56.0.tar.xz", RMD160: "cccc", SHA256: "dddd", Size: 1713768},
	}))
}
