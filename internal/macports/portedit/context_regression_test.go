package portedit

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrepareDerivedOSBoundary(t *testing.T) {
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
set modern [expr {${os.major} >= 17}]
if {$modern} {
 distfiles source.tar.gz
 checksums sha256 aaaa size 2
} else {
 distfiles binary.zip
 checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/1.2.4/source.tar.gz", "/1.2.4/binary.zip"}, *requests)
	require.Len(t, result.Downloads, 2, "both archives change with the shared version")
}

func TestPrepareMultilineOSBoundary(t *testing.T) {
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
if {
 ${os.major} >= 17
} {
 distfiles source.tar.gz
 checksums sha256 aaaa size 2
} else {
 distfiles binary.zip
 checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/1.2.4/source.tar.gz", "/1.2.4/binary.zip"}, *requests)
	require.Len(t, result.Downloads, 2, "both archives change with the shared version")
}
