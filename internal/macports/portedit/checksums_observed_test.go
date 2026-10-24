package portedit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

func TestRefreshChecksumsCoversPerArchitectureArchives(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
master_sites @SITE@/${version}
checksums arm.zip sha256 aaaa size 2 intel.zip sha256 bbbb size 3
if {${build_arch} eq "arm64"} {distfiles arm.zip} else {distfiles intel.zip}
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 2)
	require.ElementsMatch(t, []string{"/1.2.3/arm.zip", "/1.2.3/intel.zip"}, *requests)
	require.Len(t, result.Commits, 1)
	require.Equal(t, "fixture: refresh checksums", result.Commits[0].Subject)
	after := string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.3")
	require.Contains(t, after, "revision 2")
	require.NotContains(t, after, "sha256 aaaa")
	require.NotContains(t, after, "sha256 bbbb")
	require.Len(t, result.Coverage, 2, "both architectures are observed contexts")
	original, err := os.ReadFile(filepath.Join(r.Workspace.Root(), "devel/fixture/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(original), "sha256 aaaa", "the workspace is restored")

	// Writing the refreshed contents back and refreshing again downloads but changes nothing.
	require.NoError(t, os.WriteFile(filepath.Join(r.Workspace.Root(), "devel/fixture/Portfile"), result.Files[0].After, 0600))
	*requests = nil
	again, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, again.Downloads, 2)
	require.Empty(t, again.Files)
	require.Empty(t, again.Commits)
}

// A Portfile still carrying md5 and sha1 is brought to the current layout
// the first time dockhand touches its archive: the group is rewritten as
// rmd160, sha256, and size in the Portfile's own column alignment, on a
// refresh and on a version bump alike. A group already made of current
// algorithms keeps its layout.
func TestLegacyChecksumBlocksAreModernizedWhenTouched(t *testing.T) {
	t.Parallel()
	body := `version 1.2.3
revision 2
master_sites @SITE@/${version}
distfiles fixture.zip
checksums           md5     aaaa \
                    sha1    bbbb \
                    rmd160  cccc
`
	s, r, _ := archiveFixture(t, body)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	after := string(result.Files[0].After)
	require.NotContains(t, after, "md5")
	require.NotContains(t, after, "sha1")
	require.Regexp(t, `checksums           rmd160  [0-9a-f]{40} \\\n                    sha256  [0-9a-f]{64} \\\n                    size    \d+\n`, after)
	require.Contains(t, after, "revision 2")

	s, r, _ = archiveFixture(t, body)
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after = string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.4")
	require.NotContains(t, after, "md5")
	require.Regexp(t, `rmd160  [0-9a-f]{40} \\\n                    sha256  [0-9a-f]{64} \\\n                    size    \d+\n`, after)

	s, r, _ = archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture.zip
checksums sha256 aaaa \
          size 2
`)
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after = string(result.Files[0].After)
	require.NotContains(t, after, "rmd160", "a current group keeps its algorithms")
	require.Regexp(t, `checksums sha256 [0-9a-f]{64} \\\n          size \d+\n`, after)
}

// --keep-old-checksums refreshes a legacy block in place: the same
// algorithms in the same layout, every value recomputed from the download.
func TestKeepOldChecksumsRefreshesLegacyBlockInPlace(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture.zip
checksums           md5     aaaa \
                    sha1    bbbb \
                    rmd160  cccc
`)
	r.Action, r.Version, r.Release, r.KeepOldChecksums = model.EditChecksums, "", nil, true
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	after := string(result.Files[0].After)
	require.Regexp(t, `checksums           md5     [0-9a-f]{32} \\\n                    sha1    [0-9a-f]{40} \\\n                    rmd160  [0-9a-f]{40}\n`, after)
	require.NotContains(t, after, "sha256")
	require.NotContains(t, after, "aaaa")
	download := result.Downloads[0]
	require.Contains(t, after, "md5     "+download.MD5)
	require.Contains(t, after, "sha1    "+download.SHA1)
}

// Checksums a declaration reads from a digest table are refreshed where the
// table writes them; the declaration itself is untouched.
func TestRefreshChecksumsRewritesADigestTable(t *testing.T) {
	t.Parallel()
	rmd, sha := strings.Repeat("a", 40), strings.Repeat("b", 64)
	s, r, _ := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture.zip
array set modules {
    fixture {
        {
            `+rmd+` \
            `+sha+` \
            646632
        }
    }
}
set info $modules(fixture)
checksums           rmd160  [lindex [lindex ${info} 0] 0] \
                    sha256  [lindex [lindex ${info} 0] 1] \
                    size    [lindex [lindex ${info} 0] 2]
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	after := string(result.Files[0].After)
	require.NotContains(t, after, rmd)
	require.NotContains(t, after, sha)
	require.NotContains(t, after, "646632")
	require.Contains(t, after, "checksums           rmd160  [lindex [lindex ${info} 0] 0]", "the declaration keeps reading the table")
	download := result.Downloads[0]
	require.Contains(t, after, "            "+download.RMD160+" \\\n            "+download.SHA256+" \\\n            "+strconv.FormatInt(download.Size, 10)+"\n")
}

// An old convention declared checksums for patch files beside the archive's;
// with the patches present in files, MacPorts fetches and checks nothing
// for them, so the entries are left as written rather than refused as
// declarations no archive covers.
func TestChecksumEntriesForLocalPatchesAreLeftAsWritten(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
distfiles fixture.tar.gz
patchfiles patch-x.diff
checksums fixture.tar.gz md5 00000000000000000000000000000000 \
          patch-x.diff md5 11111111111111111111111111111111
`)
	require.NoError(t, os.MkdirAll(filepath.Join(r.Workspace.Root(), "devel/fixture/files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(r.Workspace.Root(), "devel/fixture/files/patch-x.diff"), []byte("--- a\n+++ b\n"), 0o644))
	require.NoError(t, r.Workspace.Rescan(), "an adopted directory learns the files added under it")
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"/1.2.4/fixture.tar.gz"}, *requests, "only the archive is fetched")
	after := string(result.Files[0].After)
	require.Contains(t, after, "patch-x.diff md5 11111111111111111111111111111111", "the patch entry is untouched")
	require.NotContains(t, after, "00000000000000000000000000000000", "the archive's entry is refreshed")
	require.Contains(t, after, "fixture.tar.gz rmd160")

	// Absent from files, the patch would be fetched and checked, so its
	// entry is a declaration no observed archive covers.
	s, r, _ = archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
distfiles fixture.tar.gz
patchfiles patch-x.diff
checksums fixture.tar.gz md5 00000000000000000000000000000000 \
          patch-x.diff md5 11111111111111111111111111111111
`)
	_, err = s.Prepare(t.Context(), r)
	require.Error(t, err)
}

// A subport whose block declares no checksums, where the port's names the
// port's own archive, has a declaration written in its block, after its
// last fetch line, filled in from its archive; the port's own stays as it
// was. kb-old, a subport beside kb at 0.1.6, was refused (field testing on
// rc3). A main port with none is still refused.
func TestASubportsChecksumsAreWrittenInItsBlock(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
checksums ${distname}${extract.suffix} rmd160 aaaa sha256 bbbb size 2
subport fixture-old {
    version 0.1.6
    revision 0
}
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	r.Selection.Subport = "fixture-old"
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"/1.2.3/fixture-0.1.6.tar.gz"}, *requests, "master_sites is the port's, read before the subport's version")
	require.Len(t, result.Files, 1)
	after := string(result.Files[0].After)
	require.Contains(t, after, "checksums ${distname}${extract.suffix} rmd160 aaaa sha256 bbbb size 2\n", "the port's own declaration stays")
	sum := result.Downloads[0].Checksum
	require.Contains(t, after, "    version 0.1.6\n    checksums           rmd160  "+sum.RMD160+" \\\n                        sha256  "+sum.SHA256+" \\\n                        size    "+strconv.FormatInt(sum.Size, 10)+"\n    revision 0\n}")
	require.Equal(t, "fixture-old: refresh checksums", result.Commits[0].Subject)

	s, r, _ = archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	_, err = s.Prepare(t.Context(), r)
	require.ErrorContains(t, err, "no checksum declaration for fixture-1.2.3.tar.gz")
}
