package portedit

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The candidates are a release's assets MacPorts extracts that name no
// platform, the PortGroup's default name first.
func TestSourceCandidatesLeaveOutBinaryBundles(t *testing.T) {
	assets := []string{"tool-1.0-darwin-arm64.tar.gz", "tool_1.0_tarball.tgz", "tool-1.0-x86_64-linux-musl.tar.xz", "tool-1.0.tar.gz", "tool-win64.zip",
		"tool-1.0.tar.zst", "checksums.txt", "tool-1.0.tar.gz.sig", "tool-1.0-universal2.zip", "tool-1.0.tbz"}
	require.Equal(t, []string{"tool-1.0.tar.gz", "tool_1.0_tarball.tgz", "tool-1.0.tbz"}, sourceCandidates(assets, "tool", "1.0"))
}

func tree(top string, files ...string) sourceTree {
	t := sourceTree{top: top, files: map[string]bool{}, dirs: map[string]int{}}
	for _, name := range files {
		t.files[name] = true
		for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
			t.dirs[dir]++
		}
	}
	return t
}

// One source in two compressions is one, gzip taken; two passing with
// different files are ambiguous; one lacking a submodule doesn't pass.
func TestJudgingSourcesByTheirFiles(t *testing.T) {
	needs := sourceNeeds{files: []string{"CMakeLists.txt"}, submodules: []string{"deps/lib"}}
	full := tree("tool-1.0", "CMakeLists.txt", "deps/lib/a.c")
	require.Empty(t, needs.lacks(full))
	require.Equal(t, "lacks deps/lib", needs.lacks(tree("tool-1.0", "CMakeLists.txt")))
	require.Equal(t, "lacks CMakeLists.txt", needs.lacks(tree("tool-1.0", "deps/lib/a.c")))
	require.Equal(t, "extracts to more than one directory", needs.lacks(tree("", "CMakeLists.txt")))

	chosen, ambiguous := judgeSources([]judgedAsset{{name: "tool-1.0.tar.xz", tree: full}, {name: "tool-1.0.tar.gz", tree: full}, {name: "tool-1.0-bin.zip", lacks: "lacks deps/lib"}})
	require.Empty(t, ambiguous)
	require.Equal(t, "tool-1.0.tar.gz", chosen.name)

	chosen, ambiguous = judgeSources([]judgedAsset{{name: "a.tar.gz", tree: full}, {name: "b.tar.gz", tree: tree("b", "CMakeLists.txt", "deps/lib/a.c", "extra")}})
	require.Nil(t, chosen)
	require.Equal(t, []string{"a.tar.gz", "b.tar.gz"}, ambiguous)
}

// A name is written with the port's name and version put back.
func TestAnAssetsNameIsWrittenWithTheVariables(t *testing.T) {
	info := macports.PortInfo{Name: "mindforger", Version: "2.5.0", Options: map[string]string{"github.project": "mindforger", "github.version": "2.5.0"}}
	got, ok := templated("ci_mindforger_2.5.0", info)
	require.True(t, ok)
	require.Equal(t, "ci_${name}_${version}", got)
	info.Name = "py-tool"
	info.Options["github.project"] = "tool"
	got, ok = templated("tool-src-2.5.0", info)
	require.True(t, ok)
	require.Equal(t, "${github.project}-src-${version}", got)
	_, ok = templated("tool 2.5.0", info)
	require.False(t, ok)
}

// githubGroup is the github PortGroup as far as a move reads it.
const githubGroup = `options github.author github.project github.version github.tag_prefix github.tag_suffix github.homepage github.master_sites github.tarball_from
default github.homepage {https://github.com/${github.author}/${github.project}}
default github.master_sites {[github.get_master_sites]}
default github.tarball_from releases
proc github.get_master_sites {} {
    global github.tarball_from github.homepage git.branch github.author github.project
    switch -- ${github.tarball_from} {
        archive { return ${github.homepage}/archive/${git.branch} }
        tarball { return https://codeload.github.com/${github.author}/${github.project}/legacy.tar.gz/${git.branch}?dummy= }
        default { return ${github.homepage}/releases/download/${git.branch} }
    }
}
proc github.setup {gh_author gh_project gh_version {gh_tag_prefix ""} {gh_tag_suffix ""}} {
    global github.author github.project github.version github.tag_prefix github.tag_suffix github.homepage PortInfo
    github.author ${gh_author}
    github.project ${gh_project}
    github.version ${gh_version}
    github.tag_prefix ${gh_tag_prefix}
    github.tag_suffix ${gh_tag_suffix}
    if {![info exists PortInfo(name)]} { name ${github.project} }
    version ${github.version}
    default homepage ${github.homepage}
    git.url ${github.homepage}.git
    git.branch [join ${github.tag_prefix}]${github.version}[join ${github.tag_suffix}]
    default master_sites {${github.master_sites}}
    distname ${github.project}-${github.version}
    default extract.rename {[expr {${github.tarball_from} in {archive tarball} && [llength ${extract.only}] == 1}]}
}
`

// tarGz is a gzipped tarball of files, each a path and its contents.
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	file, err := os.Create(path)
	require.NoError(t, err)
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	}) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(files[name])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// fakeRelease is a GitHub release's files: its assets, by name, and the
// forge's archive of its tag.
type fakeRelease struct {
	assets map[string][]byte
	tag    []byte
}

func (f fakeRelease) ReleaseAssets(context.Context, macports.PortInfo, model.Release) ([]string, error) {
	return slices.Sorted(func(yield func(string) bool) {
		for name := range f.assets {
			if !yield(name) {
				return
			}
		}
	}), nil
}

func (f fakeRelease) SourceArchive(_ context.Context, _ macports.PortInfo, source model.GitSource, directory string) (string, error) {
	path := filepath.Join(directory, string(source.Commit)+".tar.gz")
	return path, os.WriteFile(path, f.tag, 0o600)
}

// toServer sends every request to the test server, keeping its path.
type toServer struct{ server *httptest.Server }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", strings.TrimPrefix(s.server.URL, "http://")
	return http.DefaultTransport.RoundTrip(r)
}

// githubFixture is a port on the github PortGroup at 1.2.3 updating to
// v1.2.4, whose release has the files given, with the requests made.
func githubFixture(t *testing.T, body string, release fakeRelease) (*Service, Request, func() []string) {
	t.Helper()
	executable := testsupport.MacPortsTclsh(t)
	var requested []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		if data, ok := release.assets[filepath.Base(r.URL.Path)]; ok && strings.Contains(r.URL.Path, "/releases/download/") {
			_, _ = w.Write(data)
			return
		}
		fmt.Fprint(w, "archive bytes for "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	for path, text := range map[string]string{
		"_resources/port1.0/group/github-1.0.tcl": githubGroup,
		"devel/fixture/Portfile":                  "PortSystem 1.0\nPortGroup github 1.0\n\n" + body,
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(text), 0o600))
	}
	s := &Service{Ports: &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}, Archives: distfetch.Client{HTTP: &http.Client{Transport: toServer{server}}}, Releases: release}
	r := Request{Action: model.EditUpdate, Source: model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4",
		Release: &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.2.4", CurrentVersion: "1.2.3"}, Version: "1.2.4", Forge: "github", Instance: "https://github.com", Repository: "owner/fixture", Tag: "v1.2.4", Commit: strings.Repeat("c", 40)}}
	return s, r, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(requested)
	}
}

const tarballPort = `github.setup        owner fixture 1.2.3 v
# Change github.tarball_from to 'releases' or 'archive' next update
github.tarball_from tarball
revision            0
categories          devel
checksums           rmd160  0000000000000000000000000000000000000000 \
                    sha256  0000000000000000000000000000000000000000000000000000000000000000 \
                    size    1
`

const gitPort = `github.setup        owner fixture 1.2.3 v
revision            0
categories          devel

fetch.type          git

post-fetch {
    system -W ${worksrcpath} "git submodule update --init --recursive"
}
`

var (
	tagArchive    = map[string]string{"fixture-c/CMakeLists.txt": "project(fixture)\n", "fixture-c/src/a.c": "", "fixture-c/.gitmodules": "[submodule \"lib\"]\n\tpath = deps/lib\n\turl = https://github.com/owner/lib\n"}
	sourceTarball = map[string]string{"fixture-1.2.4/CMakeLists.txt": "project(fixture)\n", "fixture-1.2.4/src/a.c": "", "fixture-1.2.4/.gitmodules": ""}
)

// A tarball port goes to its release's source archive where one is the
// source, fetched once, and to GitHub's archive where it isn't, said.
func TestATarballPortGoesToItsReleasesSourceElseTheArchive(t *testing.T) {
	t.Parallel()
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture-1.2.4.tar.gz": tarGz(t, sourceTarball), "fixture-1.2.4-darwin-arm64.tar.gz": []byte("binary")}}
	s, r, requested := githubFixture(t, tarballPort, release)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, &MovedSource{From: "tarball", To: "releases", Release: "v1.2.4", Asset: "fixture-1.2.4.tar.gz", Lines: []string{"github.tarball_from releases"}}, result.SourceMoved)
	after := string(result.Files[0].After)
	require.Contains(t, after, "github.tarball_from releases\nrevision            0\ncategories")
	require.NotContains(t, after, "# Change github.tarball_from")
	require.Equal(t, []string{"/owner/fixture/releases/download/v1.2.4/fixture-1.2.4.tar.gz"}, requested(), "the asset is fetched once, and the binary bundle never")
	require.Len(t, result.Downloads, 1)

	release.assets["fixture-1.2.4.tar.gz"] = tarGz(t, map[string]string{"fixture-1.2.4/src/a.c": ""})
	s, r, _ = githubFixture(t, tarballPort, release)
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, &MovedSource{From: "tarball", To: "archive"}, result.SourceMoved)
	require.Equal(t, "release v1.2.4 carries fixture-1.2.4.tar.gz, which lacks CMakeLists.txt, so the port fetches GitHub's archive of the tag", result.SourceNotice)
	require.Contains(t, string(result.Files[0].After), "github.tarball_from archive")
}

// --source keep moves nothing: the port stays on GitHub's tarball.
func TestSourceKeepMovesNothing(t *testing.T) {
	t.Parallel()
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture-1.2.4.tar.gz": tarGz(t, sourceTarball)}}
	s, r, _ := githubFixture(t, tarballPort, release)
	r.SourceMode = SourceKeep
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Nil(t, result.SourceMoved)
	require.Contains(t, string(result.Files[0].After), "github.tarball_from tarball")
}

// A port cloned for its submodules goes to a release asset holding each,
// its distname, suffix, and directory written and its checksums filled,
// and stays where the asset lacks one, said.
func TestAGitPortGoesToAReleaseAssetHoldingItsSubmodules(t *testing.T) {
	t.Parallel()
	asset := tarGz(t, map[string]string{"ci_fixture_1.2.4/CMakeLists.txt": "", "ci_fixture_1.2.4/src/a.c": "", "ci_fixture_1.2.4/deps/lib/lib.c": ""})
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture_1.2.4_tarball.tgz": asset, "fixture-1.2.4.tar.gz": tarGz(t, sourceTarball)}}
	s, r, requested := githubFixture(t, gitPort, release)
	var said []string
	ctx := progress.WithReporter(t.Context(), func(update progress.Update) { said = append(said, update.Message) })
	result, err := s.Prepare(ctx, r)
	require.NoError(t, err)
	require.Equal(t, &MovedSource{From: "git", To: "releases", Release: "v1.2.4", Asset: "fixture_1.2.4_tarball.tgz", Submodules: []string{"deps/lib"},
		Lines: []string{"distname ${name}_${version}_tarball", "extract.suffix .tgz", "worksrcdir ci_${name}_${version}"}}, result.SourceMoved)
	after := string(result.Files[0].After)
	require.NotContains(t, after, "fetch.type")
	require.NotContains(t, after, "post-fetch")
	require.Contains(t, after, "distname            ${name}_${version}_tarball\nextract.suffix      .tgz\nworksrcdir          ci_${name}_${version}\n\nchecksums           rmd160  ")
	require.NotContains(t, after, "rmd160  0 \\")
	require.Contains(t, after, fmt.Sprintf("size    %d", len(asset)))
	require.Len(t, result.Downloads, 1)
	require.Equal(t, 1, strings.Count(strings.Join(requested(), "\n"), "fixture_1.2.4_tarball.tgz"), "fetched once")
	require.Contains(t, said, "Fetches release v1.2.4's fixture_1.2.4_tarball.tgz, which carries the repository's Git submodules (deps/lib), instead of a Git checkout.")

	delete(release.assets, "fixture_1.2.4_tarball.tgz")
	s, r, _ = githubFixture(t, gitPort, release)
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Nil(t, result.SourceMoved)
	require.Equal(t, "release v1.2.4 carries fixture-1.2.4.tar.gz, which lacks deps/lib, so the port stays on its clone", result.SourceNotice)
	require.Contains(t, string(result.Files[0].After), "fetch.type          git")
	require.Empty(t, result.Downloads)

	r.SourceMode = SourceReleases
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrUnsupported)
	require.Contains(t, err.Error(), "--source releases: release v1.2.4 carries fixture-1.2.4.tar.gz, which lacks deps/lib")
}

// A Git port with a hook of its own, or a fork's git.url, stays, said,
// having downloaded nothing; a pinned commit chooses no source.
func TestAGitPortWithAHookOfItsOwnOrAPinStays(t *testing.T) {
	t.Parallel()
	asset := tarGz(t, map[string]string{"fixture-1.2.4/CMakeLists.txt": "", "fixture-1.2.4/src/a.c": "", "fixture-1.2.4/deps/lib/lib.c": ""})
	release := fakeRelease{tag: tarGz(t, tagArchive), assets: map[string][]byte{"fixture-1.2.4.tar.gz": asset}}
	for _, c := range []struct{ why, body, notice string }{
		{"hook", strings.Replace(gitPort, "--recursive\"\n", "--recursive\"\n    system -W ${worksrcpath} \"./bootstrap\"\n", 1),
			"the Portfile's fetch.type git and submodule hook aren't the standard ones at its top, so the port stays on its clone"},
		{"fork", gitPort + "git.url https://github.com/fork/fixture.git\n", "git.url, https://github.com/fork/fixture.git, isn't the release's repository, so the port stays on its clone"},
	} {
		s, r, requested := githubFixture(t, c.body, release)
		result, err := s.Prepare(t.Context(), r)
		require.NoError(t, err, c.why)
		require.Nil(t, result.SourceMoved, c.why)
		require.Equal(t, c.notice, result.SourceNotice, c.why)
		require.Empty(t, requested(), c.why)
	}
	// A pinned commit isn't a release's tag: its update is the pin's, as
	// before, and no source is chosen.
	s, r, requested := githubFixture(t, gitPort+"git.branch "+strings.Repeat("0", 40)+"\n", release)
	_, err := s.Prepare(t.Context(), r)
	require.Error(t, err)
	require.Empty(t, requested())
}
