package portedit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// currentFixture is githubFixture's port already at 1.2.4, revision 2, its
// release found current.
func currentFixture(t *testing.T, body string, release fakeRelease) (*Service, Request, func() []string) {
	t.Helper()
	body = strings.Replace(strings.Replace(body, "1.2.3", "1.2.4", 1), "revision            0", "revision            2", 1)
	s, r, requested := githubFixture(t, body, release)
	r.Version = ""
	r.Release.Requested, r.Release.CurrentVersion, r.Release.NoUpdate = "", "1.2.4", true
	return s, r, requested
}

// --source releases on a port already current is carried out at its
// version, the version and revision kept and the checksums made for the
// asset; a source that can't move is refused with its reason; and without
// ActOnCurrent the update stops at the current version as it did (#121).
func TestReview20261110SourceOverrideOnCurrentVersion(t *testing.T) {
	t.Parallel()
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture-1.2.4.tar.gz": tarGz(t, sourceTarball)}}

	s, r, requested := currentFixture(t, tarballPort, release)
	r.SourceMode = SourceReleases
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Nil(t, result.SourceMoved, "without ActOnCurrent nothing is done, as before")
	require.Empty(t, result.Files)
	require.Empty(t, requested())

	s, r, _ = currentFixture(t, tarballPort, release)
	r.SourceMode, r.ActOnCurrent = SourceReleases, true
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, model.ActionDone, result.SourceOutcome)
	require.Equal(t, &MovedSource{From: "tarball", To: "releases", Release: "v1.2.4", Asset: "fixture-1.2.4.tar.gz", Lines: []string{"github.tarball_from releases"}}, result.SourceMoved)
	require.Len(t, result.Files, 1)
	after := string(result.Files[0].After)
	require.Contains(t, after, "github.setup        owner fixture 1.2.4 v")
	require.Contains(t, after, "revision            2", "the revision stays: the version didn't move")
	require.NotContains(t, after, "0000000000000000000000000000000000000000", "the checksums are the asset's")

	release.assets["fixture-1.2.4.tar.gz"] = tarGz(t, map[string]string{"fixture-1.2.4/src/a.c": ""})
	s, r, _ = currentFixture(t, tarballPort, release)
	r.SourceMode, r.ActOnCurrent = SourceReleases, true
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "lacks CMakeLists.txt")
}

// --source archive on a current port fetching GitHub's tarball moves it to
// the archive at its version; on one that fetches the archive already it is
// satisfied and changes nothing.
func TestSourceArchiveOnACurrentPort(t *testing.T) {
	t.Parallel()
	release := fakeRelease{tag: tarGz(t, tagArchive)}
	s, r, _ := currentFixture(t, tarballPort, release)
	r.SourceMode, r.ActOnCurrent = SourceArchive, true
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, model.ActionDone, result.SourceOutcome)
	require.Equal(t, &MovedSource{From: "tarball", To: "archive"}, result.SourceMoved)
	require.Contains(t, string(result.Files[0].After), "github.tarball_from archive")
	require.Contains(t, string(result.Files[0].After), "revision            2")
	require.NotContains(t, string(result.Files[0].After), "0000000000000000000000000000000000000000", "the checksums are the archive's")

	archivePort := strings.Replace(tarballPort, "github.tarball_from tarball", "github.tarball_from archive", 1)
	s, r, requested := currentFixture(t, archivePort, release)
	r.SourceMode, r.ActOnCurrent = SourceArchive, true
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, model.ActionSatisfied, result.SourceOutcome)
	require.Empty(t, result.Files)
	require.Empty(t, requested())
}

// The setting alone, suggest or choose, asks nothing of a current port:
// a plain update of it stays the no-op it is (#121).
func TestTheSourceSettingAloneAsksNothingOfACurrentPort(t *testing.T) {
	t.Parallel()
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture-1.2.4.tar.gz": tarGz(t, sourceTarball)}}
	for _, mode := range []SourceMode{"", SourceSuggest, SourceChoose} {
		s, r, requested := currentFixture(t, tarballPort, release)
		r.SourceMode, r.ActOnCurrent = mode, true
		result, err := s.Prepare(t.Context(), r)
		require.NoError(t, err, mode)
		require.Empty(t, result.Files, mode)
		require.Empty(t, result.SourceOutcome, mode)
		require.Empty(t, requested(), mode)
	}
}

// Without ActOnCurrent a member is affected where its version moved alone,
// as before #195, whatever else is set: the source move at the current
// version is unreachable until step 5 switches it on.
func TestWithoutActOnCurrentAffectedIsTheVersionAlone(t *testing.T) {
	t.Parallel()
	input := &sourceInput{sourceMoved: &MovedSource{From: "tarball", To: "releases"}}
	other := &sourceInput{}
	same, moved := macports.PortInfo{Version: "1.2.4"}, macports.PortInfo{Version: "1.2.5"}
	for _, noUpdate := range []bool{false, true} {
		request := Request{Release: &model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: noUpdate}}}
		for _, member := range []*sourceInput{input, other} {
			require.False(t, affectedMember(request, input, member, same, same))
			require.True(t, affectedMember(request, input, member, same, moved))
		}
	}
	request := Request{ActOnCurrent: true, Release: &model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: true}}}
	require.True(t, affectedMember(request, input, input, same, same), "the moved port, asked of a current one")
	require.False(t, affectedMember(request, input, other, same, same), "another member")
}
