package portedit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// goModFixture serves one archive holding fixture-1.2.4/go.mod and prepares
// a Go-PortGroup-shaped port with the given extra declarations.
func goModFixture(t *testing.T, manifest, extra string) (*Service, Request) {
	t.Helper()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts required")
	}
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
	s := &Service{Ports: &eval.Evaluator{Executable: executable}, Archives: archives.Client{HTTP: server.Client()}}
	r := Request{Action: record.Bump, Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &record.Release{Selection: record.Selection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
	return s, r
}

// In module mode Go enforces go.mod's go directive, so a literal
// go.toolchain_min below it is raised to the required series; the manifest
// is read from the archive's top directory through the GOPATH worksrcdir.
func TestModuleModeGoPortRaisesToolchainMinFromTheManifest(t *testing.T) {
	t.Parallel()
	s, r := goModFixture(t, "module example.com/fixture\ngo 1.24\ntoolchain go1.25.1\n", "go.offline_build no\ngo.toolchain_min 1.22\n")
	var messages []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
	result, err := s.Prepare(ctx, r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Contains(t, after, "version 1.2.4\nrevision 0")
	require.Contains(t, after, "go.toolchain_min 1.25")
	require.NotContains(t, after, "1.22")
	require.Contains(t, strings.Join(messages, "\n"), "Raising go.toolchain_min from 1.22 to 1.25")
	require.Equal(t, "1.25", result.Fidelity[len(result.Fidelity)-1].After.Ports["fixture"].Options["go.toolchain_min"])
	for _, report := range result.Fidelity {
		require.Empty(t, report.UnexpectedChanges)
	}
	require.Len(t, result.Commits, 1)
}

// A minimum that already covers the requirement, a GOPATH-mode build, and a
// port declaring no minimum are all left as they are, with the last one
// told what the manifest asks for.
func TestToolchainMinIsLeftAloneWhenNotRaisable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, extra, keep, message string }{
		{"already covered", "go.offline_build no\ngo.toolchain_min 1.24\n", "go.toolchain_min 1.24", ""},
		{"gopath mode", "go.offline_build yes\ngo.toolchain_min 1.22\n", "go.toolchain_min 1.22", "GOPATH mode"},
		{"undeclared", "go.offline_build no\n", "", "declares no go.toolchain_min"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, r := goModFixture(t, "module example.com/fixture\ngo 1.24\n", test.extra)
			var messages []string
			ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
			result, err := s.Prepare(ctx, r)
			require.NoError(t, err)
			after := string(result.Files[0].After)
			require.Contains(t, after, "version 1.2.4")
			if test.keep != "" {
				require.Contains(t, after, test.keep)
			}
			require.NotContains(t, after, "go.toolchain_min 1.24\n"+"go.toolchain_min")
			if test.message != "" {
				require.Contains(t, strings.Join(messages, "\n"), test.message)
			}
		})
	}
}

type fakeManifests struct {
	manifest string
	missing  bool
	calls    []string
}

func (f *fakeManifests) Manifest(_ context.Context, _ macports.PortInfo, release record.Release, path string) ([]byte, error) {
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
