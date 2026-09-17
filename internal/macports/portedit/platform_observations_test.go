package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativePlatformOperands(t *testing.T) {
	for _, tc := range []struct{ name, setup, condition string }{
		{"scalar", "set minimum 17", `${os.major} >= $minimum`},
		{"inverted", "set minimum 17", `$minimum <= ${os.major}`},
		{"option", "options fixture.minimum\nfixture.minimum 17", `${os.major} >= [option fixture.minimum]`},
		{"alias", "set major ${os.major}", `$major >= 17`},
		{"formatting", `configure.args --triplet=darwin${os.major}`, `${os.major} >= 17`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, requests := archiveFixture(t, tc.setup+"\nif {"+tc.condition+`} {
version 1.2.3
revision 2
checksums sha256 aaaa size 2
} else {
version 0.9.0
revision 3
checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
			result, err := s.Prepare(t.Context(), r)
			require.NoError(t, err)
			require.Len(t, *requests, 1)
			require.Contains(t, string(result.Files[0].After), "version 0.9.0\nrevision 3\nchecksums sha256 bbbb size 3")
			require.Contains(t, string(result.Files[0].After), "version 1.2.4\nrevision 0")
		})
	}
}
func TestMutablePlatformOperandRefusedBeforeDownloads(t *testing.T) {
	s, r, requests := archiveFixture(t, `set minimum 16
set minimum 17
version 1.2.3
revision 0
if {${os.major} >= $minimum} {distfiles source.tar.gz} else {distfiles old.tar.gz}
checksums sha256 aaaa size 2
master_sites @SITE@/${version}
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrProbeInconclusive)
	require.Contains(t, err.Error(), "changes value")
	require.Empty(t, *requests)
}

func TestExternalThresholdCannotEscapeThroughNativeOnlyProfiles(t *testing.T) {
	s, r, requests := archiveFixture(t, `set minimum [exec /bin/echo 99]
version 1.2.3
revision 0
if {${os.major} < $minimum} {distfiles source.tar.gz} else {distfiles future.tar.gz}
checksums sha256 aaaa size 2
master_sites @SITE@/${version}
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrProbeInconclusive)
	require.Contains(t, err.Error(), "host state")
	require.Empty(t, *requests)
}

func TestUnmodeledFormattingReadsDoNotBlockPreparation(t *testing.T) {
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
checksums sha256 aaaa size 2
master_sites @SITE@/${version}
configure.env-append MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target}
if {${os.platform} eq "darwin" && [vercmp $macosx_deployment_target 10.12] < 0} {
    configure.args-append --without-clock_gettime
}
post-patch {
    reinplace "s/TARGET/${macosx_deployment_target}/" ${worksrcpath}/Info.plist
}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, *requests, 1)
	after := string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.4\nrevision 0")
	require.Contains(t, after, "configure.env-append MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target}")
	require.Contains(t, after, "[vercmp $macosx_deployment_target 10.12] < 0")
}

func TestUnmodeledReadSelectingSourcesRefusedBeforeDownloads(t *testing.T) {
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
if {[vercmp $macosx_deployment_target 10.12] < 0} {distfiles legacy.tar.gz} else {distfiles source.tar.gz}
checksums sha256 aaaa size 2
master_sites @SITE@/${version}
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrProbeInconclusive)
	require.Contains(t, err.Error(), "distfiles is selected by the OS minor version or deployment target")
	require.Empty(t, *requests)
}
