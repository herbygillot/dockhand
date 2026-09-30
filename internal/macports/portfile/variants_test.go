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
