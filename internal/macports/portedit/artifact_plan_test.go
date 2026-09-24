package portedit

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func archiveFixture(t *testing.T, body string) (*Service, Request, *[]string) {
	t.Helper()
	executable := testsupport.MacPortsTclsh(t)
	var requested []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "archive bytes for "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	path := filepath.Join(root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	src := "PortSystem 1.0\nname fixture\ncategories devel\n" + strings.ReplaceAll(body, "@SITE@", server.URL) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(src), 0600))
	s := &Service{Ports: &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}, Archives: archives.Client{HTTP: server.Client()}}
	r := Request{Action: record.Bump, Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &record.Release{Selection: record.Selection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
	return s, r, &requested
}

func TestPrepareAllArchitectureArchivesWithConstantNames(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
master_sites @SITE@/${version}
checksums arm.zip sha256 aaaa size 2 intel.zip sha256 bbbb size 3
if {${build_arch} eq "arm64"} {distfiles arm.zip} else {distfiles intel.zip}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 2)
	require.ElementsMatch(t, []string{"/1.2.4/arm.zip", "/1.2.4/intel.zip"}, *requests)
	require.Contains(t, string(result.Files[0].After), "version 1.2.4")
	require.Contains(t, string(result.Files[0].After), "revision 0")
	require.NotContains(t, string(result.Files[0].After), "sha256 aaaa")
	original, err := os.ReadFile(filepath.Join(r.Workspace.Root(), "devel/fixture/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(original), "version 1.2.3")
}
func TestPrepareSharedOSBranchesPreservesAuxiliaryPin(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
if {${os.major} >= 17} {
 distfiles source.tar.gz
 checksums source.tar.gz sha256 aaaa size 2
} else {
 distfiles binary.zip
 checksums binary.zip sha256 bbbb size 3
}
master_sites @SITE@/${version}:release @SITE@/pinned:pin
set distfiles [list ${distfiles}:release pinned.zip:pin]
checksums-append pinned.zip sha256 cccc size 4
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Downloads, 2)
	require.ElementsMatch(t, []string{"/1.2.4/source.tar.gz", "/1.2.4/binary.zip"}, *requests)
	require.Contains(t, string(result.Files[0].After), "checksums-append pinned.zip sha256 cccc size 4")
}
func TestPrepareKeepsIndependentOldOSVersionAndChecksums(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `if {${os.major} >= 17} {
 version 1.2.3
 revision 2
 checksums sha256 aaaa size 2
} else {
 version 0.9.0
 revision 5
 checksums sha256 bbbb size 3
}
master_sites @SITE@/${version}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, *requests, 1)
	require.Contains(t, string(result.Files[0].After), "version 0.9.0\n revision 5\n checksums sha256 bbbb size 3")
}
func TestPrepareScopedSeriesWithoutForge(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 0
subport fixture-1.2 {
 set patchNumber 3
 revision 2
 checksums sha256 aaaa size 2
}
subport fixture-1.1 {
 set patchNumber 9
 revision 5
 checksums sha256 bbbb size 3
}
proc release {} {global subport patchNumber;return [lindex [split $subport -] 1].$patchNumber}
if {${subport} ne ${name}} {
 version [release]
 master_sites @SITE@/${version}
}
`)
	r.Selection.Subport = "fixture-1.2"
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, *requests, 1)
	require.Contains(t, string(result.Files[0].After), "set patchNumber 4\n revision 0")
	require.Contains(t, string(result.Files[0].After), "set patchNumber 9\n revision 5\n checksums sha256 bbbb size 3")
}

func TestCandidateCannotActivateUnprovenChecksumDeclaration(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
if {$version eq "1.2.3"} {
 checksums sha256 aaaa size 2
} else {
 checksums sha256 bbbb size 3
}
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.Empty(t, *requests)
}
func TestConflictingContextChecksumsLeaveWorkspaceUntouched(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
revision 0
master_sites @SITE@/${version}
if {${build_arch} eq "arm64"} {distfiles arm.zip} else {distfiles intel.zip}
checksums sha256 aaaa size 2
`)
	_, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.ErrorContains(t, err, "different bytes")
	original, err := os.ReadFile(filepath.Join(r.Workspace.Root(), "devel/fixture/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(original), "version 1.2.3")
}

func TestPreparePreservesRejectedPlatformGuardWithoutClaimingBuildCoverage(t *testing.T) {
	t.Parallel()
	guard := `if {${os.major} < 23 && ${build_arch} eq "arm64"} {
 known_fail yes
 pre-fetch {
  ui_error "${subport} requires a newer macOS release"
  return -code error "unsupported platform"
 }
}`
	s, r, requests := archiveFixture(t, "version 1.2.3\nmaster_sites @SITE@/${version}\nchecksums sha256 aaaa size 2\n"+guard)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), guard)
	require.Len(t, *requests, 1)
	guarded := false
	for _, profile := range result.Coverage {
		if profile.Platform.Version == "22" && profile.Platform.Architecture == "arm64" {
			guarded = true
			require.NotNil(t, profile.Fetch)
			require.True(t, profile.Fetch.Rejected)
		}
	}
	require.True(t, guarded)
}

// A Portfile that derives its version from the source's spelling, as the
// perl5 PortGroup derives the port version from the module version, is
// edited to the spelling and evaluated to the version: the release names
// both, the input holds the spelling, and the archive is fetched by it.
func TestPrepareEditsTheSourceSpellingOfADerivedVersion(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `set modver 1.2
version ${modver}00
revision 1
livecheck.version ${modver}
master_sites @SITE@/${modver}
distfiles fixture.tar.gz
checksums sha256 aaaa size 2
`)
	r.Version = "1.3"
	r.Release = &record.Release{Selection: record.Selection{Requested: "1.3"}, Archive: true, Version: "1.300", SourceVersion: "1.3"}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"/1.3/fixture.tar.gz"}, *requests)
	after := string(result.Files[0].After)
	require.Contains(t, after, "set modver 1.3")
	require.Contains(t, after, "version ${modver}00")
	require.Contains(t, after, "revision 0")
	require.Equal(t, "1.300", result.Release.Version)
	r.Release = &record.Release{Selection: record.Selection{Requested: "1.3"}, Archive: true, Version: "1.3"}
	_, err = s.Prepare(t.Context(), r)
	require.Error(t, err, "a release whose version is not what the spelling evaluates to is refused")
}

// A revision calculated from a table, as the qt family reads its module
// revisions, is reset at the one place the Portfile writes it as "revision N",
// the way a checksum held in the same table is refreshed at its literal.
func TestPrepareResetsARevisionCarriedInATable(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `set module_info {{`+strings.Repeat("a", 64)+` 2} "revision 1"}
version 1.2.3
revision [regexp -inline {[0-9]+} [lindex ${module_info} 1]]
master_sites @SITE@/${version}
distfiles fixture.tar.gz
checksums sha256 [lindex [lindex ${module_info} 0] 0] size [lindex [lindex ${module_info} 0] 1]
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Contains(t, after, `"revision 0"`)
	require.NotContains(t, after, `"revision 1"`)
	require.Contains(t, after, "version 1.2.4")
	require.NotContains(t, after, strings.Repeat("a", 64))
}
