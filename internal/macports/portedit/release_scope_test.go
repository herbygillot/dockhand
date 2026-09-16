package portedit

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

func TestSharedReleaseRequiresAuthorizationAndPreservesIndependentSibling(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "refuse", true: "prepare"}[authorized], func(t *testing.T) {
			s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-a {}
subport fixture-b {}
subport fixture-pinned {
 version 0.9.0
 revision 7
 distname pinned-0.9.0
 master_sites @SITE@/0.9.0
 checksums sha256 bbbb size 3
}
if {${subport} eq "fixture"} {distfiles; fetch {}; use_configure no; build {}}
`)
			r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-a"}
			r.SharedRelease = authorized
			result, err := s.Prepare(t.Context(), r)
			if !authorized {
				require.ErrorIs(t, err, ErrFidelity)
				require.Empty(t, *requests)
				return
			}
			require.NoError(t, err)
			require.Len(t, *requests, 1)
			require.Len(t, result.Scope.Affected, 3)
			require.Len(t, result.Scope.BuildTargets(), 2)
			require.Len(t, result.Scope.Protected, 1)
			require.Equal(t, "fixture-pinned", result.Scope.Protected[0].Target.Name)
			require.Equal(t, "1.2.3", result.Scope.Input.Before)
			require.Equal(t, "1.2.4", result.Scope.Input.After)
			require.Contains(t, string(result.Files[0].After), "version 0.9.0\n revision 7")
			require.Contains(t, string(result.Files[0].After), "checksums sha256 bbbb size 3")
		})
	}
}

func TestSharedReleaseRefusesUnprovableChecksumOwners(t *testing.T) {
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-a {}
subport fixture-b {}
if {${subport} eq "fixture-b"} {
 checksums sha256 aaaa size 2
}
`)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-a"}
	r.SharedRelease = true
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, portfile.ErrUnsupported)
	require.Empty(t, *requests)
}
