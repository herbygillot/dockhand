package sourcecompare

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/project"
)

// A configure.ac's change says what it asks of the build: the options,
// pkg-config modules, and libraries it adds or drops (field testing,
// batch 11: dateutils).
func TestAConfigureAcChangeSaysWhatItAsks(t *testing.T) {
	t.Parallel()
	old := project.ReadAutoconf([]byte("AC_ARG_ENABLE([fast-arith])\nAC_CHECK_LIB([m], [floor])\n"))
	now := project.ReadAutoconf([]byte("AC_ARG_ENABLE([fast-arith])\nAC_ARG_ENABLE([contrib])\nPKG_CHECK_MODULES([TZ], [libtzdb])\n"))
	require.Equal(t, ": --enable-contrib added; pkg-config module libtzdb added; library m removed", autoconfWords(old, now))
	require.Equal(t, ", in nothing it names as an option, a pkg-config module, or a library", autoconfWords(old, old))
}
