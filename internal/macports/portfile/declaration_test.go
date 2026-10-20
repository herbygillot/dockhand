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

// Of two commands of the same text, the one a frame of the Portfile places
// on its line is the one that ran: LyX's two checksums blocks of the same
// words, in two branches of an if, read as ambiguous (the roadmap's v0.5.0
// part 3, step 2). Where no frame places one, it's still ambiguous, for a
// probe to settle.
func TestOfTwoCommandsOfOneTextTheFramesLineChooses(t *testing.T) {
	t.Parallel()
	src := []byte("PortSystem 1.0\nif {$a} {\n    checksums sha256 aaaa\n} elseif {$b} {\n    checksums sha256 aaaa\n} else {\n    checksums sha256 bbbb\n}\n")
	path := "/tree/aqua/LyX/Portfile"
	declaration := macports.Declaration{Command: "checksums", Values: []string{"sha256", "aaaa"}, Frames: []macports.SourceFrame{{File: path, Line: 5, Command: "checksums sha256 aaaa"}}}
	cmd, err := portfile.LocateDeclaration(src, path, declaration)
	require.NoError(t, err)
	line, _ := textedit.Position(src, cmd.Span.Start)
	require.Equal(t, 5, line)

	declaration.Frames[0].Line = 9
	_, err = portfile.LocateDeclaration(src, path, declaration)
	ambiguous := new(portfile.AmbiguousDeclaration)
	require.ErrorAs(t, err, &ambiguous)
	require.Len(t, ambiguous.Matches, 2)
	require.ErrorIs(t, err, portfile.ErrUnsupported)
	require.ErrorContains(t, err, "declaration source is ambiguous (2 matches)")
}

// A declaration a PortGroup's own code makes is the PortGroup's, at its
// file and line, with the tables its command reads, never the Portfile's
// to edit: crossbinutils 1.0 writes each port's checksums from
// crossbinutils.versions_info (the roadmap's v0.5.0 part 3, step 2).
func TestADeclarationAPortGroupMakesIsThePortGroups(t *testing.T) {
	t.Parallel()
	src := []byte("PortSystem 1.0\nPortGroup crossbinutils 1.0\ncrossbinutils.setup aarch64-elf 2.47\n")
	path := "/tree/cross/aarch64-elf-binutils/Portfile"
	group := "/tree/_resources/port1.0/group/crossbinutils-1.0.tcl"
	declaration := macports.Declaration{Command: "checksums", Values: []string{"binutils-2.47.tar.xz", "sha256", "aaaa"}, Frames: []macports.SourceFrame{
		{File: path, Line: 3, Command: "crossbinutils.setup aarch64-elf 2.47"},
		{File: group, Line: 166, Command: "checksums   binutils-${version}${extract.suffix} {*}[lindex [set crossbinutils.versions_info($version)] 1]"},
	}}
	_, err := portfile.LocateDeclaration(src, path, declaration)
	inGroup := new(portfile.InPortGroup)
	require.ErrorAs(t, err, &inGroup)
	require.Equal(t, &portfile.InPortGroup{Group: "crossbinutils 1.0", File: "_resources/port1.0/group/crossbinutils-1.0.tcl", Line: 166, Tables: []string{"crossbinutils.versions_info"}}, inGroup)
	require.ErrorIs(t, err, portfile.ErrUnsupported)
	require.NotContains(t, err.Error(), "Portfile to edit")
	require.Contains(t, err.Error(), "a change there moves every port of the group")

	// crossgcc 1.0's checksums are in an if of its procedure, whose body
	// Tcl compiled apart, with no file: the nearest frame outside it that
	// has one is the PortGroup's.
	declaration.Frames = []macports.SourceFrame{
		{File: path, Line: 9, Command: "crossgcc.setup arm-none-eabi 8.3.0"},
		{File: "/tree/_resources/port1.0/group/crossgcc-1.0.tcl", Line: 200, Command: "if {...} {...}"},
		{Command: "checksums   gcc-${version}${extract.suffix} {*}[lindex [set crossgcc.versions_info($version)] 1]"},
	}
	_, err = portfile.LocateDeclaration(src, path, declaration)
	require.ErrorAs(t, err, &inGroup)
	require.Equal(t, &portfile.InPortGroup{Group: "crossgcc 1.0", File: "_resources/port1.0/group/crossgcc-1.0.tcl", Line: 200, Tables: []string{"crossgcc.versions_info"}}, inGroup)
}

// rust 1.0's checksums-append carries each cargo.crates row's sha256, which
// the Portfile writes: it's the Portfile's to change, where crossbinutils
// 1.0's table row isn't (field testing on 5a727e00: every cargo port's
// update was refused as the PortGroup's).
func TestChecksumsThePortfileWritesAreItsOwn(t *testing.T) {
	t.Parallel()
	src := []byte(`PortSystem 1.0
PortGroup rust 1.0
github.setup ggml-org purl 0.2.12 v
checksums ${distname}${extract.suffix} \
          rmd160 1111111111111111111111111111111111111111 \
          sha256 2222222222222222222222222222222222222222222222222222222222222222 \
          size 1000
cargo.crates \
    adler2 2.0.0 512761e0bb2578dd7380c6baaa0f4ce03e84f95e960231d1dec8bf4d7d6e2627 \
    anyhow 1.0.86 b3d1d046238990b9cf5bcde22a3fb3584ee5cf65fb2765f454ed428c7a0063da
`)
	crate := []string{"adler2-2.0.0.crate", "sha256", "512761e0bb2578dd7380c6baaa0f4ce03e84f95e960231d1dec8bf4d7d6e2627"}
	require.True(t, portfile.WritesChecksums(src, crate))
	require.True(t, portfile.WritesChecksums(src, []string{"adler2-2.0.0.crate sha256 512761e0bb2578dd7380c6baaa0f4ce03e84f95e960231d1dec8bf4d7d6e2627"}), "one value or many")
	require.False(t, portfile.WritesChecksums(src, []string{"binutils-2.47.tar.xz", "sha256", "aaaa", "size", "1000"}), "a table's row")
	require.False(t, portfile.WritesChecksums(src, []string{"x.tar.gz", "size", "1000"}), "a size alone")
}
