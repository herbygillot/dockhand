package portedit

import (
	"strings"
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
// owns none of its archives; selected itself, with --shared-release, the
// sibling's archives are the update's to fetch, as any member's own are.
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
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 1)
	require.NotContains(t, string(result.Files[0].After), "sha256 aaaa")
}

// A port that fetches nothing is covered: its assessment finds its version
// input where update edits it, and has none to find where its version is
// MacPorts' own (batch 37).
func TestAPortThatFetchesNothingIsCovered(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, livecheck, fetch, outcome string }{
		{"a _select port", "livecheck.type none", "No source is downloaded, and livecheck does not use the version; MacPorts maintains it, so update leaves it unchanged", OwnVersion},
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

// A port whose revision is written only in a block, as rust's in an if,
// beside a subport's own, as rust-src's, has that line bumped: an added
// line at the end set every subport's revision, rust-src's 2 among them
// (rc7's rust revbump, 2026-11-02).
func TestARevbumpOfARevisionWrittenInABlock(t *testing.T) {
	t.Parallel()
	body := "version 1.2.3\ndistfiles\nif {${os.platform} eq \"darwin\"} {\n    revision 0\n}\nsubport fixture-src {\n    revision 2\n}\n"
	s, r, _ := archiveFixture(t, body)
	r.Action, r.Version, r.Release, r.Subject = model.EditRevbump, "", nil, "rebuild for libfoo 2"
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.True(t, strings.HasSuffix(after, strings.Replace(body, "revision 0", "revision 1", 1)+"\n"), "the if's line, and no line added: %s", after)
	require.Equal(t, 2, strings.Count(after, "revision"), after)
}

// configure.args and configure.env, read for check --plan -v alone, name
// ${workpath}, which is named for where the Portfile was evaluated, so the
// fidelity check leaves them out: a revbump of a port whose configure.args
// name it is made, as any other (field testing's curl +sectrust notes,
// note 8b, prioritization's addition).
func TestARevbumpOfAPortWhoseConfigureArgsNameItsWorkpath(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, "version 1.2.3\nrevision 1\ndistfiles\nconfigure.args --with-cache=${workpath}/cache\nconfigure.env CACHE=${workpath}/cache\n")
	r.Action, r.Version, r.Release, r.Subject = model.EditRevbump, "", nil, "rebuild for libfoo 2"
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), "revision 2\n")
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
