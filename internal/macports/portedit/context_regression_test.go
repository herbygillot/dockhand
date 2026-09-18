package portedit

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// One boundary form end to end; the multiline and other forms are
// recognized at the scanner's seam in TestScanPlatformNeedsRecognizesOperandForms.
func TestPrepareDerivedOSBoundary(t *testing.T) {
	t.Parallel()
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

func TestPrepareResetsRevisionsAcrossAffectedContexts(t *testing.T) {
	t.Parallel()
	for _, nativeRevision := range []string{"0", "2"} {
		t.Run(nativeRevision, func(t *testing.T) {
			s, r, requests := archiveFixture(t, `version 1.2.3
if {${os.major} >= 17} {
 revision `+nativeRevision+`
 distfiles source.tar.gz
 checksums sha256 aaaa size 2
} else {
 revision 4
 distfiles binary.zip
 checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
			result, err := s.Prepare(t.Context(), r)
			require.NoError(t, err)
			require.Len(t, *requests, 2)
			require.NotContains(t, string(result.Files[0].After), "revision 4")
			require.NotContains(t, string(result.Files[0].After), "revision 2")
		})
	}
}

func TestPrepareRefusesRevisionSharedWithIndependentPin(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `revision 2
if {${os.major} >= 17} {
 version 1.2.3
 checksums sha256 aaaa size 2
} else {
 version 0.9.0
 checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.Empty(t, *requests)
}

func TestPrepareConfigureArchitectureBranches(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
if {${configure.build_arch} eq "arm64"} {
 distfiles arm.zip
 checksums sha256 aaaa size 2
} else {
 distfiles intel.zip
 checksums sha256 bbbb size 3
}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 2)
	require.ElementsMatch(t, []string{"/1.2.4/arm.zip", "/1.2.4/intel.zip"}, *requests)
}
