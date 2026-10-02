package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The paths a Portfile's build names below ${worksrcpath} are read, as
// semgrep's pfff and semgrep-core, which 1.179.0's source no longer had
// (field testing, 2026-10-02).
func TestTheWorksrcPathsAPortfileNamesAreRead(t *testing.T) {
	src := []byte(`build {
    system -W ${worksrcpath}/pfff "make"
    system -W $worksrcpath/semgrep-core/src "dune build"
}
# system -W ${worksrcpath}/commented
destroot.dir ${worksrcpath}/cli/${name}
configure.dir   ${worksrcpath}/pfff
`)
	require.Equal(t, []string{"cli", "pfff", "semgrep-core/src"}, WorksrcPaths(src))
}
