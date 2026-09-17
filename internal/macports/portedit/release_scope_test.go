package portedit

import (
	"strings"
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

// An obsolete main port that is replaced_by a versioned subport and spells
// that subport's version as its own literal moves with the subport's bump,
// without shared-release authorization; the terraform Portfile has this shape.
func TestObsoleteFollowerMovesWithTheSubportItIsReplacedBy(t *testing.T) {
	const portfile = `subport fixture-1 {
 version 1.2.3
 revision 0
 distname shared-${version}
 master_sites @SITE@/${version}
 checksums sha256 aaaa size 2
}
subport fixture-0 {
 version 0.9.0
 revision 0
 distname old-${version}
 master_sites @SITE@/${version}
 checksums sha256 bbbb size 3
}
if {${subport} eq ${name}} {
 replaced_by fixture-1
 version 1.2.3
 revision 0
 distfiles
 fetch {}
 use_configure no
 build {}
}
`
	s, r, _ := archiveFixture(t, portfile)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-1"}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Equal(t, 2, strings.Count(after, "version 1.2.4"), "the subport and its obsolete follower both move")
	require.Contains(t, after, "replaced_by fixture-1\n version 1.2.4")
	require.Contains(t, after, "version 0.9.0", "an unrelated series is untouched")
	require.NotNil(t, result.Scope)
	require.Len(t, result.Scope.Affected, 2)
	require.Equal(t, "fixture", result.Scope.Affected[0].Target.Name)
	require.True(t, result.Scope.Affected[0].MetadataOnly, "an obsolete port builds nothing")
	require.Equal(t, "1.2.4", result.Scope.Affected[0].After.Version)
	require.Equal(t, "fixture-1", result.Scope.Affected[1].Target.Name)
	require.Len(t, result.Scope.BuildTargets(), 1, "only the subport is verified")

	// A main port at a different version is not a follower and is left alone.
	s, r, _ = archiveFixture(t, strings.Replace(portfile, "replaced_by fixture-1\n version 1.2.3", "replaced_by fixture-1\n version 1.1.0", 1))
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-1"}
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after = string(result.Files[0].After)
	require.Equal(t, 1, strings.Count(after, "version 1.2.4"))
	require.Contains(t, after, "version 1.1.0")
	require.Nil(t, result.Scope, "nothing but the target moved")
}
