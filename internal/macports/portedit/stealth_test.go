package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// stealthPortfile declares checksums its served archive no longer matches:
// the archive changed upstream under the same name.
const stealthPortfile = `version 1.2.3
revision 2
master_sites @SITE@/${version}
distfiles fixture.zip
checksums sha256 aaaa size 2
`

// A checksum refresh that finds an archive changed under the same name
// makes a stealth update, inside the editor: the revision bumped and
// dist_subdir set, both as MacPorts evaluates them, and held to changing
// that alone. (The private-helper review of 2026-09-28, finding 9.)
func TestAStealthUpdateIsEvaluatedAsTheEditorMakesIt(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, stealthPortfile)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	r.Stealth = &StealthRequest{}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.NotNil(t, result.Stealth)
	require.Len(t, result.Stealth.Distfiles, 1)
	require.Equal(t, "fixture.zip", result.Stealth.Distfiles[0].Name)
	require.True(t, result.Stealth.Revbumped)
	require.Equal(t, "fixture/1.2.3_3", result.Stealth.DistSubdir, "as evaluated, following the bumped revision")
	after := string(result.Files[0].After)
	require.Contains(t, after, "revision 3")
	require.Contains(t, after, "dist_subdir")
	last := result.Fidelity[len(result.Fidelity)-1]
	require.ElementsMatch(t, []string{"fixture.dist_subdir", "fixture.revision +1"}, last.ExpectedChanges)
	require.Empty(t, last.UnexpectedChanges)
	require.Equal(t, 3, result.Prepared.Ports["fixture"].Revision)
}

func TestAStealthUpdateKeepingTheRevisionNumbersTheDirectory(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, stealthPortfile)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	r.Stealth = &StealthRequest{KeepRevision: true}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.Stealth.Revbumped)
	require.Equal(t, "fixture/1.2.3_1", result.Stealth.DistSubdir)
	require.Contains(t, string(result.Files[0].After), "revision 2")
}

// No stealth update is made where none is asked, or of a Portfile the
// branch changed since its base, as a new version edited by hand is.
func TestAStealthUpdateNeedsAskingAndAnUnchangedPortfile(t *testing.T) {
	t.Parallel()
	for _, asked := range []*StealthRequest{nil, {Changed: []string{"devel/fixture/Portfile"}}} {
		s, r, _ := archiveFixture(t, stealthPortfile)
		r.Action, r.Version, r.Release = model.EditChecksums, "", nil
		r.Stealth = asked
		result, err := s.Prepare(t.Context(), r)
		require.NoError(t, err)
		require.Nil(t, result.Stealth)
		require.NotContains(t, string(result.Files[0].After), "dist_subdir", "the checksums alone are refreshed")
		require.Contains(t, string(result.Files[0].After), "revision 2")
	}
}

// A subport with an archive of its own that inherits the revision would
// be bumped with the port the refresh selected, which the stealth update
// doesn't mean to: its edits are left for the person, said, and the
// checksums are refreshed. (The earlier code-organization review's
// finding 19.)
func TestAStealthUpdateThatWouldChangeASubportIsLeftForYou(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, stealthPortfile+"subport fixture-extra {\n    distfiles extra.zip\n    checksums sha256 bbbb size 3\n}\n")
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	r.Stealth = &StealthRequest{}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.NotNil(t, result.Stealth)
	require.False(t, result.Stealth.Revbumped)
	require.Empty(t, result.Stealth.DistSubdir)
	require.Contains(t, result.Stealth.Problem, "fixture-extra.revision: expected 2, got 3")
	require.Contains(t, result.Stealth.Problem, "so they are left for you")
	require.Contains(t, string(result.Files[0].After), "revision 2")
	require.NotContains(t, string(result.Files[0].After), "dist_subdir")
}

// A version update drops a stealth update's dist_subdir, whose new archive
// has a name of its own, as the editor evaluates it.
func TestANewVersionDropsTheStealthDistSubdirAsEvaluated(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
revision 2
master_sites @SITE@/${version}
distfiles fixture.zip
checksums sha256 aaaa size 2
dist_subdir ${name}/${version}_${revision}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.True(t, result.DistSubdirRemoved)
	require.NotContains(t, string(result.Files[0].After), "dist_subdir")
	require.Contains(t, string(result.Files[0].After), "version 1.2.4")
	require.Equal(t, []string{"fixture.dist_subdir"}, result.Fidelity[len(result.Fidelity)-1].ExpectedChanges)
	require.Equal(t, "fixture", result.Prepared.Ports["fixture"].Options["dist_subdir"], "MacPorts' own default again")
}
