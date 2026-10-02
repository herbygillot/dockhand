package portedit

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/model"

	"github.com/stretchr/testify/require"
)

// A port that fetches nothing, a metaport or a _select port, is updated
// only where its livecheck reads its version, and then edits its version
// alone, with nothing to download (batch 37).
func TestAPortThatFetchesNothingEditsItsVersionAloneWhereALivecheckReadsIt(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 1
distfiles
homepage @SITE@/
livecheck.type regex
livecheck.url @SITE@/releases
livecheck.regex {fixture-(\d+(?:\.\d+)*)}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Empty(t, result.Downloads)
	require.Empty(t, *requests, "nothing is downloaded")
	require.Contains(t, string(result.Files[0].After), "version 1.2.4")
	require.Contains(t, string(result.Files[0].After), "revision 0")
}

// One whose livecheck reads no version has no release discovery finds,
// but a version named is set, revision 0, as terraform's obsolete stub's
// was by hand (field testing, 2026-10-02, the person's word).
func TestAPortWhoseVersionIsMacPortsOwnTakesAVersionNamed(t *testing.T) {
	t.Parallel()
	for name, livecheck := range map[string]string{"none": "livecheck.type none", "fallback": ""} {
		t.Run(name, func(t *testing.T) {
			s, r, requests := archiveFixture(t, "version 1.2.3\nrevision 2\ndistfiles\n"+livecheck)
			result, err := s.Prepare(t.Context(), r)
			require.NoError(t, err)
			require.Contains(t, string(result.Files[0].After), "version 1.2.4")
			require.Contains(t, string(result.Files[0].After), "revision 0")
			require.Empty(t, *requests)
		})
	}
}

// A subport that fetches nothing moves with its sibling's release, and
// owns none of its archives; selected itself, it doesn't move the sibling
// that fetches them.
func TestAFamilyMemberThatFetchesNothing(t *testing.T) {
	t.Parallel()
	family := `version 1.2.3
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-select {
    distfiles
}
`
	s, r, _ := archiveFixture(t, family)
	r.SharedRelease = true
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err, "fixture-select owns none of fixture's archives")
	require.Len(t, result.Downloads, 1)

	s, r, _ = archiveFixture(t, family)
	r.SharedRelease = true
	r.Selection.Subport = "fixture-select"
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.ErrorContains(t, err, "fixture, another port of the same Portfile, fetches its own source (its distfiles differ from fixture-select's), which updating fixture-select doesn't move")
}

// A port that fetches nothing is covered: its assessment finds its version
// input where update edits it, and has none to find where its version is
// MacPorts' own (batch 37).
func TestAPortThatFetchesNothingIsCovered(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, livecheck, fetch, outcome string }{
		{"a _select port", "livecheck.type none", "Fetches nothing, and no livecheck reads its version: its version is MacPorts' own, which update leaves", OwnVersion},
		{"a metaport following its release", "homepage @SITE@/\nlivecheck.type regex\nlivecheck.url @SITE@/releases\nlivecheck.regex {fixture-(\\d+(?:\\.\\d+)*)}", "Fetches nothing in any observed context; an update edits its version alone", InputFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, r, _ := archiveFixture(t, "version 1.2.3\ndistfiles\n"+test.livecheck)
			input, err := s.load(t.Context(), &r)
			require.NoError(t, err)
			p := &VersionProbe{editor: s, request: r, input: input}
			local, err := p.Assess(t.Context(), nil)
			require.NoError(t, err)
			require.Equal(t, test.outcome, local.Outcome, "%+v", local.Findings)
			require.Equal(t, test.fetch, assessmentFinding(t, local, "fetch").Detail)
			require.Equal(t, "No checksums: nothing is downloaded", assessmentFinding(t, local, "checksums").Detail)
			require.NotEmpty(t, local.Coverage)
			for _, context := range local.Coverage {
				require.True(t, context.FetchesNothing, "%+v", context)
			}
		})
	}
}

// A port that fetches nothing on one platform but its source on another,
// as libcxx, follows that source: its version isn't MacPorts' own, though
// no livecheck reads it.
func TestAPortThatFetchesOnlyElsewhereIsUpdatedAsAnyOther(t *testing.T) {
	t.Parallel()
	body := `version 1.2.3
master_sites @SITE@/${version}
checksums intel.zip sha256 bbbb size 3
if {${build_arch} eq "arm64"} {distfiles} else {distfiles intel.zip}
livecheck.type none
`
	s, r, requests := archiveFixture(t, body)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 1)
	require.Equal(t, []string{"/1.2.4/intel.zip"}, *requests)

	s, r, _ = archiveFixture(t, body)
	input, err := s.load(t.Context(), &r)
	require.NoError(t, err)
	local, err := (&VersionProbe{editor: s, request: r, input: input}).Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, InputFound, local.Outcome, "%+v", local.Findings)
}

// A subport with no block of its own shares the Portfile's revision, as
// the python PortGroup's do: a revbump of one bumps that line, and its
// siblings move with it, as revbump's help says (field testing,
// 2026-10-02: py-lmdb and py313-lmdb were refused).
func TestARevbumpOfASubportSharingTheRevision(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, "version 1.2.3\nrevision 2\ndistfiles\nforeach v {a b} {\n    subport fixture-$v {}\n}\n")
	r.Action, r.Version, r.Release, r.Subject = model.EditRevbump, "", nil, "rebuild for libfoo 2"
	r.Selection.Subport = "fixture-b"
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), "revision 3\n")
	require.Equal(t, "fixture-b: rebuild for libfoo 2", result.Commits[0].Subject)
}

// A command below the source directory, as qemu's configure.cmd
// ${worksrcpath}/configure, moves with the version, and isn't a change of
// its own: update, update --outdated, and checksums each refused qemu
// (field testing, 2026-10-02).
func TestACommandBelowTheSourceMovesWithTheVersion(t *testing.T) {
	t.Parallel()
	body := `version 1.2.3
master_sites @SITE@/
distfiles fixture-${version}.tar.gz
checksums sha256 aaaa size 2
configure.cmd ${worksrcpath}/configure
`
	s, r, _ := archiveFixture(t, body)
	r.Release.Tag = ""
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), "version 1.2.4")

	s, r, _ = archiveFixture(t, body)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	_, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
}
