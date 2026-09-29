package portedit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// goModFixture serves one archive holding fixture-1.2.4/go.mod and prepares
// a Go-PortGroup-shaped port with the given extra declarations.
func goModFixture(t *testing.T, manifest, extra string) (*Service, Request) {
	t.Helper()
	executable := testsupport.MacPortsTclsh(t)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-1.2.4/go.mod", Mode: 0600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(manifest))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	t.Cleanup(server.Close)
	root := t.TempDir()
	portdir := filepath.Join(root, "devel/fixture")
	require.NoError(t, os.MkdirAll(portdir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "Portfile"), []byte(strings.ReplaceAll(`PortSystem 1.0
name fixture
categories devel
version 1.2.3
revision 1
options go.package go.offline_build go.toolchain_min
go.package example.com/fixture
worksrcdir gopath/src/example.com/fixture
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
`+extra, "@SITE@", server.URL)), 0600))
	s := &Service{Ports: &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}, Archives: archives.Client{HTTP: server.Client()}}
	r := Request{Action: model.EditUpdate, Source: model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
	return s, r
}

// In module mode Go enforces go.mod's go directive, so a literal
// go.toolchain_min of an earlier series is raised to the directive, as
// go.mod writes it; the toolchain directive, only a suggestion, is no
// requirement. The manifest is read from the archive's top directory
// through the GOPATH worksrcdir.
func TestModuleModeGoPortRaisesToolchainMinFromTheManifest(t *testing.T) {
	t.Parallel()
	s, r := goModFixture(t, "module example.com/fixture\ngo 1.24.3\ntoolchain go1.25.1\n", "go.offline_build no\ngo.toolchain_min 1.22\n")
	var messages []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
	result, err := s.Prepare(ctx, r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.4\nrevision 0")
	require.Contains(t, after, "go.toolchain_min 1.24.3\n")
	require.NotContains(t, after, "1.22")
	require.Contains(t, strings.Join(messages, "\n"), "Raising go.toolchain_min from 1.22 to 1.24.3")
	require.Equal(t, "1.24.3", result.Fidelity[len(result.Fidelity)-1].After.Ports["fixture"].Options["go.toolchain_min"])
	for _, report := range result.Fidelity {
		require.Empty(t, report.UnexpectedChanges)
	}
	require.Len(t, result.Commits, 1)
	require.Equal(t, &GoToolchain{Required: "1.24.3", Declared: "1.22", Outcome: GoToolchainRaised}, result.GoToolchain)
}

// A minimum of the required series already covers it, whatever the patch
// release, since the Go PortGroup compares only the series. It, a
// GOPATH-mode build, a port declaring no minimum, and one declaring it in a
// way that can't be rewritten are all left as they are, each with what the
// update found as a fact of the result, but for GOPATH mode, whose go.mod
// isn't read.
func TestToolchainMinIsLeftAloneWhenNotRaisable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, directive, extra, keep, message string
		fact                                  *GoToolchain
	}{
		{"already covered", "1.24", "go.offline_build no\ngo.toolchain_min 1.24\n", "go.toolchain_min 1.24", "", &GoToolchain{Required: "1.24", Declared: "1.24", Outcome: GoToolchainCovered}},
		{"a patch release of the series", "1.24.8", "go.offline_build no\ngo.toolchain_min 1.24\n", "go.toolchain_min 1.24\n", "", &GoToolchain{Required: "1.24.8", Declared: "1.24", Outcome: GoToolchainCovered}},
		{"a later series", "1.24.8", "go.offline_build no\ngo.toolchain_min 1.25.0\n", "go.toolchain_min 1.25.0\n", "", &GoToolchain{Required: "1.24.8", Declared: "1.25.0", Outcome: GoToolchainCovered}},
		{"gopath mode", "1.24", "go.offline_build yes\ngo.toolchain_min 1.22\n", "go.toolchain_min 1.22", "GOPATH mode", nil},
		{"undeclared", "1.24", "go.offline_build no\n", "", "declares no go.toolchain_min", &GoToolchain{Required: "1.24", Outcome: GoToolchainUndeclared}},
		{"not literal", "1.24", "go.offline_build no\nset floor 1.22\ngo.toolchain_min ${floor}\n", "go.toolchain_min ${floor}", "raise it by hand", &GoToolchain{Required: "1.24", Declared: "1.22", Outcome: GoToolchainByHand}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, r := goModFixture(t, "module example.com/fixture\ngo "+test.directive+"\n", test.extra)
			var messages []string
			ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
			result, err := s.Prepare(ctx, r)
			require.NoError(t, err)
			after := string(result.Files[0].After)
			require.Contains(t, after, "version 1.2.4")
			if test.keep != "" {
				require.Contains(t, after, test.keep)
			}
			require.NotContains(t, after, "go.toolchain_min "+test.directive+"\n"+"go.toolchain_min")
			if test.message != "" {
				require.Contains(t, strings.Join(messages, "\n"), test.message)
			}
			require.Equal(t, test.fact, result.GoToolchain)
		})
	}
}

type fakeManifests struct {
	manifest string
	missing  bool
	calls    []string
}

func (f *fakeManifests) Manifest(_ context.Context, _ macports.PortInfo, release model.Release, path string) ([]byte, error) {
	f.calls = append(f.calls, release.Commit+":"+path)
	if f.missing {
		return nil, macports.ErrManifestMissing
	}
	return []byte(f.manifest), nil
}

// A git-fetched module-mode port downloads no archive, so its go.mod is read
// from the repository at the resolved commit; without a manifest source, or
// with no go.mod there, the declared minimum stands.
func TestGitFetchedModuleModePortRaisesToolchainMinFromTheRepository(t *testing.T) {
	t.Parallel()
	port := `version 1.2.3
revision 1
options go.package go.offline_build go.toolchain_min
go.package example.com/fixture
go.offline_build no
go.toolchain_min 1.22
fetch.type git
git.url https://example.invalid/fixture.git
git.branch v${version}
`
	commit := strings.Repeat("c", 40)
	s, r, _ := archiveFixture(t, port)
	r.Release = gitRelease(commit)
	manifests := &fakeManifests{manifest: "module example.com/fixture\ngo 1.25\n"}
	s.Manifests = manifests
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{commit + ":go.mod"}, manifests.calls, "the manifest is read at the resolved commit")
	after := string(result.Files[0].After)
	require.Contains(t, after, "go.toolchain_min 1.25")
	require.Contains(t, after, "version 1.2.4")
	require.Equal(t, "1.25", result.Prepared.Ports["fixture"].Options["go.toolchain_min"])

	s, r, _ = archiveFixture(t, port)
	r.Release = gitRelease(commit)
	s.Manifests = &fakeManifests{missing: true}
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), "go.toolchain_min 1.22", "no go.mod at the commit leaves the minimum")

	s, r, _ = archiveFixture(t, port)
	r.Release = gitRelease(commit)
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].After), "go.toolchain_min 1.22", "no manifest source leaves the minimum")
}

// go.offline_build is read as Tcl reads a boolean: false in any spelling is
// module mode; true, unset, or not a boolean leaves the minimum alone.
func TestModuleModeReadsOfflineBuildAsTclBoolean(t *testing.T) {
	t.Parallel()
	for value, module := range map[string]bool{"no": true, "No": true, "off": true, "0": true, "yes": false, "true": false, "maybe": false} {
		info := macports.PortInfo{Options: map[string]string{"go.package": "example.com/fixture", "go.offline_build": value}}
		require.Equal(t, module, moduleModeGo(info), value)
	}
	require.False(t, moduleModeGo(macports.PortInfo{Options: map[string]string{"go.package": "example.com/fixture"}}))
	require.False(t, moduleModeGo(macports.PortInfo{Options: map[string]string{"go.offline_build": "no"}}), "not a Go PortGroup port")
}

// cargo.update is read as Tcl reads a boolean, and a value that is not one
// is refused as true would be, since it would rewrite the lockfile.
func TestCargoUpdateIsReadAsTclBoolean(t *testing.T) {
	t.Parallel()
	for value, allowed := range map[string]bool{"": true, "no": true, "Off": true, "false": true, "yes": false, "ON": false, "1": false, "sometimes": false} {
		err := checkCargoUpdate(macports.PortInfo{Options: map[string]string{"cargo.update": value}})
		if allowed {
			require.NoError(t, err, value)
		} else {
			require.ErrorContains(t, err, "cargo.update changes the upstream lockfile", value)
		}
	}
}
