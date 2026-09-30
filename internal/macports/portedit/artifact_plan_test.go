package portedit

import (
	"crypto/sha256"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	r := Request{Action: model.EditUpdate, Source: model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
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
	r.Release = &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.3"}, Archive: true, Version: "1.300", SourceVersion: "1.3"}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"/1.3/fixture.tar.gz"}, *requests)
	after := string(result.Files[0].After)
	require.Contains(t, after, "set modver 1.3")
	require.Contains(t, after, "version ${modver}00")
	require.Contains(t, after, "revision 0")
	require.Equal(t, "1.300", result.Release.Version)
	r.Release = &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.3"}, Archive: true, Version: "1.3"}
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

// An update whose archive depends on the macOS release, as gh's source
// tarball and older systems' prebuilt zip do, pairs each archive it replaces
// with its own replacement, fetched as MacPorts shipped it with that
// context's checksums. The current version's archives were this Mac's plan
// alone, one against the new version's two, so nothing was compared (the
// gh, usql, hk, and pgdog run's finding 2).
func TestAnUpdateOfArchivesChosenByReleasePairsEach(t *testing.T) {
	t.Parallel()
	declared := func(path string) string {
		body := "archive bytes for " + path
		return fmt.Sprintf("sha256 %x size %d", sha256.Sum256([]byte(body)), len(body))
	}
	s, r, _ := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
if {${os.major} >= 17} {
 distfiles source-${version}.tar.gz
 checksums `+declared("/1.2.3/source-1.2.3.tar.gz")+`
} else {
 distfiles binary-${version}.zip
 checksums `+declared("/1.2.3/binary-1.2.3.zip")+`
}
`)
	r.KeepArchives = t.TempDir()
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Empty(t, result.PreviousProblem)
	require.Len(t, result.Downloads, 2)
	require.Len(t, result.Pairs, 2, "one pair for each checksum declaration, however many contexts share it")
	pairs := map[string]string{}
	for _, pair := range result.Pairs {
		previous, err := os.ReadFile(pair.Previous.Path)
		require.NoError(t, err)
		next, err := os.ReadFile(pair.Next.Path)
		require.NoError(t, err)
		pairs[string(previous)] = string(next)
	}
	require.Equal(t, map[string]string{
		"archive bytes for /1.2.3/source-1.2.3.tar.gz": "archive bytes for /1.2.4/source-1.2.4.tar.gz",
		"archive bytes for /1.2.3/binary-1.2.3.zip":    "archive bytes for /1.2.4/binary-1.2.4.zip",
	}, pairs, "each context's archive beside its own replacement, once")
}

// A default variant's own distfile has its checksums updated with the
// rest, as git's +doc declares git-htmldocs' with checksums-append in its
// body, which dockhand couldn't locate (the git run's finding 1).
func TestAnUpdateWritesADefaultVariantsChecksums(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture-${version}.tar.gz
checksums fixture-${version}.tar.gz sha256 aaaa size 2
default_variants +doc
variant doc description {Install documentation} {
    distfiles-append    fixture-doc-${version}.tar.gz
    checksums-append    fixture-doc-${version}.tar.gz \
                        sha256  bbbb \
                        size    3
}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/1.2.4/fixture-1.2.4.tar.gz", "/1.2.4/fixture-doc-1.2.4.tar.gz"}, *requests)
	after := string(result.Files[0].After)
	doc := "archive bytes for /1.2.4/fixture-doc-1.2.4.tar.gz"
	require.Contains(t, after, fmt.Sprintf("    checksums-append    fixture-doc-${version}.tar.gz \\\n                        sha256  %x \\\n                        size    %d\n", sha256.Sum256([]byte(doc)), len(doc)))
	require.NotContains(t, after, "bbbb")
	require.NotContains(t, after, "aaaa")
}

// A variant that isn't on by default and declares an archive of its own
// has its checksums updated too, observed asked for on this Mac, or the
// Portfile would keep the old version's for it.
func TestAnUpdateWritesAVariantsChecksumsWhenItIsntDefault(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture-${version}.tar.gz
checksums fixture-${version}.tar.gz sha256 aaaa size 2
variant extra description {Install the extras} {
    distfiles-append    fixture-extra-${version}.tar.gz
    checksums-append    fixture-extra-${version}.tar.gz sha256 bbbb size 3
}
variant quiet description {Nothing to fetch} {}
`)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/1.2.4/fixture-1.2.4.tar.gz", "/1.2.4/fixture-extra-1.2.4.tar.gz"}, *requests)
	extra := "archive bytes for /1.2.4/fixture-extra-1.2.4.tar.gz"
	require.Contains(t, string(result.Files[0].After), fmt.Sprintf("checksums-append    fixture-extra-${version}.tar.gz sha256 %x size %d\n", sha256.Sum256([]byte(extra)), len(extra)))
	require.NotContains(t, string(result.Files[0].After), "bbbb")
	require.True(t, slices.ContainsFunc(result.Coverage, func(c ContextCoverage) bool { return c.Variant == "extra" && c.Affected && !c.Modeled }), "the variant's context is named")
	require.False(t, slices.ContainsFunc(result.Coverage, func(c ContextCoverage) bool { return c.Variant == "quiet" }), "one fetching nothing of its own isn't asked about")
}

// A checksum refresh that can't find a declaration to edit returns the
// checksums every archive has now, for a person to write, where it had
// said only why (the git run's finding 3).
func TestARefreshThatCantWriteReturnsTheChecksums(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture-${version}.tar.gz fixture-doc-${version}.tar.gz
checksums fixture-${version}.tar.gz sha256 aaaa size 2
eval checksums-append fixture-doc-${version}.tar.gz sha256 bbbb size 3
variant extra description {Install the extras} {
    distfiles-append fixture-extra-${version}.tar.gz
    checksums-append fixture-extra-${version}.tar.gz sha256 cccc size 4
}
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	_, err := s.Prepare(t.Context(), r)
	var toWrite *ChecksumsToWrite
	require.ErrorAs(t, err, &toWrite)
	var unlocated *distfiles.Unlocated
	require.ErrorAs(t, err, &unlocated, "the reason stays reachable")
	var names []string
	for _, sum := range toWrite.Checksums {
		body := "archive bytes for /1.2.3/" + sum.Name
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(body))), sum.SHA256)
		require.Equal(t, int64(len(body)), sum.Size)
		require.NotEmpty(t, sum.RMD160)
		names = append(names, sum.Name)
	}
	require.Equal(t, []string{"fixture-1.2.3.tar.gz", "fixture-doc-1.2.3.tar.gz", "fixture-extra-1.2.3.tar.gz"}, names, "each archive once, a variant's own included")
}

// A checksum refresh of a port whose family shares one declaration, as a
// python stub and its subports do, refreshes it for all of them: the stub
// had been refused as an unintended change (the py-flatbuffers run's
// finding 1).
func TestARefreshOfAFamilysSharedChecksums(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
master_sites @SITE@/${version}
distfiles fixture-${version}.tar.gz
checksums fixture-${version}.tar.gz sha256 aaaa size 2
subport py313-fixture {}
subport py312-fixture {}
`)
	r.Action, r.Version, r.Release = model.EditChecksums, "", nil
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	body := "archive bytes for /1.2.3/fixture-1.2.3.tar.gz"
	require.Contains(t, string(result.Files[0].After), fmt.Sprintf("sha256 %x", sha256.Sum256([]byte(body))))
	for _, name := range []string{"fixture", "py313-fixture", "py312-fixture"} {
		require.Contains(t, result.Prepared.Ports[name].Options["checksums"], fmt.Sprintf("%x", sha256.Sum256([]byte(body))), name)
	}
}
