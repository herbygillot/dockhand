package portedit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestRefreshChecksumsCoversPerArchitectureArchives(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
master_sites @SITE@/${version}
checksums arm.zip sha256 aaaa size 2 intel.zip sha256 bbbb size 3
if {${build_arch} eq "arm64"} {distfiles arm.zip} else {distfiles intel.zip}
`)
	r.Action, r.Version, r.Release = record.RefreshChecksums, "", nil
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
	original, err := os.ReadFile(filepath.Join(r.Root, "devel/fixture/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(original), "sha256 aaaa", "the workspace is restored")

	// Writing the refreshed contents back and refreshing again downloads but changes nothing.
	require.NoError(t, os.WriteFile(filepath.Join(r.Root, "devel/fixture/Portfile"), result.Files[0].After, 0600))
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
	r.Action, r.Version, r.Release = record.RefreshChecksums, "", nil
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
	r.Action, r.Version, r.Release, r.KeepOldChecksums = record.RefreshChecksums, "", nil, true
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
	r.Action, r.Version, r.Release = record.RefreshChecksums, "", nil
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
