package portedit

import (
	"github.com/stretchr/testify/require"
	"testing"
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
