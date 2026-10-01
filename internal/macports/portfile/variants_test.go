package portfile_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

func TestAVariantDeclaringArchivesIsNamed(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"doc", "extra"}, portfile.ArchiveVariants([]byte(`PortSystem 1.0
name demo
distfiles demo.tar.gz
variant doc description {docs} {
    if {${name} eq ${subport}} {
        distfiles-append    demo-doc.tar.gz
    }
}
variant quiet description {no archives} {
    configure.args-append --quiet
}
if {${os.major} > 20} {
    variant extra description {inside a condition} {
        checksums-append extra.tar.gz sha256 aaaa size 1
    }
} else {
    variant extra description {defined again, named once} {
        checksums-append extra-old.tar.gz sha256 bbbb size 2
    }
}
variant patched description {a patch, which fetches nothing} {
    patchfiles-append fix.diff
}
variant indirect description {through a procedure} {
    add_docs
}
`)))
	require.Empty(t, portfile.ArchiveVariants([]byte("variant {")), "text that isn't Tcl names nothing")
}

// Only what MacPorts runs is read: a variant written inside a string, and
// a checksums inside a variant's set, are data, neither a variant nor a
// declaration, while a variant a platform block or a condition runs is
// one (the helper-ownership review's finding 3, its probe as a regression
// test).
func TestArchiveVariantsReadNoData(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"set documentation {variant imaginary {distfiles-append example.tar.gz}}\n",
		"variant docs description {Documentation} {set example {checksums sha256 aaaa}}\n",
	} {
		require.Empty(t, portfile.ArchiveVariants([]byte(src)), src)
	}
	require.Equal(t, []string{"arm", "late"}, portfile.ArchiveVariants([]byte(`platform darwin arm {
    variant arm description {ARM} { distfiles-append arm.tar.gz }
}
if {${os.major} > 20} {
    variant late description {Late} { if {1} { master_sites-append https://example.org/ } }
}
`)))
}

// The variants that depend on a port are those whose own body declares a
// dependency on it, in any phase and any form Base reads, enchant2's
// +nuspell; one depending on another port, a default dependency, one
// through a variable, or the name inside data isn't one.
func TestTheVariantsThatDependOnAPort(t *testing.T) {
	src := []byte(`PortSystem 1.0
name enchant2
depends_lib port:glib2
variant nuspell description {Use nuspell} {
    depends_lib-append port:nuspell
}
variant hunspell description {Use hunspell} {
    depends_lib-append path:lib/libhunspell.dylib:hunspell
}
variant both {
    depends_build-append port:pkgconfig
    depends_run-append lib:libnuspell:Nuspell
}
variant indirect {
    depends_lib-append port:${dep}
}
variant quoted {
    set note "depends_lib-append port:nuspell"
}
`)
	require.Equal(t, []string{"nuspell", "both"}, portfile.DependencyVariants(src, "nuspell"))
	require.Equal(t, []string{"hunspell"}, portfile.DependencyVariants(src, "hunspell"))
	require.Empty(t, portfile.DependencyVariants(src, "glib2"), "a default dependency is the index's")
	require.Empty(t, portfile.DependencyVariants([]byte("variant x {"), "nuspell"), "unparseable")
}
