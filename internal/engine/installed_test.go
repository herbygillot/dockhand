package engine

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check's build is compared with the port's previous result in the same
// environment: a library whose install name moved is said with the check
// it compared against; with no earlier reading nothing is said (Then's
// item 9: exiv2 0.28's soname).
func TestACheckSaysWhatMovedOfWhatItInstalled(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	reading := func(soname string) func(*model.GuestExecution, *model.TargetResult) {
		var key string
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			var err error
			key, err = tx.RecordInputs(model.TargetInputs{Environment: "tart darwin 25 arm64 " + soname, Directory: "textproc/jq", Tree: model.ObjectID(strings.Repeat("a", 40)), Resources: model.ObjectID(strings.Repeat("b", 40)), Active: []model.ActivePort{}})
			return err
		}))
		return func(_ *model.GuestExecution, jq *model.TargetResult) {
			jq.Inputs = key
			jq.Installed = []string{"/opt/local/bin/jq", "/opt/local/lib/" + soname}
			jq.Libraries = []model.Library{{File: "/opt/local/lib/" + soname, Name: "/opt/local/lib/" + soname, Compatibility: "1.0.0"}}
			jq.Configure = []string{"-- Found ZLIB", "checking for " + soname + "... yes"}
		}
	}
	latest := func() model.Run {
		var runs []model.Run
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			var err error
			runs, err = r.Runs(store.RunFilter{Branch: branch.ID})
			return err
		}))
		return runs[0]
	}
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, reading("libjq.1.dylib"))
	found, err := e.InstallChanges(t.Context(), latest(), true)
	require.NoError(t, err)
	require.Empty(t, found, "no earlier reading says nothing")

	first := latest()
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, reading("libjq.2.dylib"))
	found, err = e.InstallChanges(t.Context(), latest(), true)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, model.TargetID("jq"), found[0].Target)
	require.Equal(t, first.Name(), found[0].Check)
	require.Equal(t, "libjq.1.dylib is libjq.2.dylib now", found[0].Libraries[0].Words())
	require.Empty(t, found[0].Added, "a new soname is the same file")
	require.Equal(t, []string{"checking for libjq.2.dylib... yes"}, found[0].Found, "and configure's drift (Then's item 10)")
	require.Equal(t, []string{"checking for libjq.1.dylib... yes"}, found[0].Lost)
}

// A moved library's line names its dependents, and says which of them the
// branch rebuilds, as the description says it (Then's item 9).
func TestAMovedLibraryNamesTheDependentsTheBranchRebuilds(t *testing.T) {
	t.Parallel()
	finding := InstallFinding{Check: "check-12", Dependents: []string{"darktable", "gpsbabel"}}
	finding.Target = "exiv2"
	finding.Environment = model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	finding.Libraries = []evidence.LibraryChange{{Was: model.Library{Name: "/opt/local/lib/libexiv2.27.dylib"}, Now: model.Library{Name: "/opt/local/lib/libexiv2.28.dylib"}}}
	where := DescribeEnvironment(finding.Environment)
	require.Equal(t, []InstallLine{{true, "exiv2: libexiv2.27.dylib is libexiv2.28.dylib now on " + where + " against check-12; darktable, gpsbabel link it, which update --revbump-dependents rebuilds"}}, finding.Lines(nil))
	require.Equal(t, "exiv2: libexiv2.27.dylib is libexiv2.28.dylib now on "+where+" against check-12; darktable, gpsbabel link it, and this branch rebuilds gpsbabel", finding.Lines([]string{"gpsbabel"})[0].Words)
	require.Equal(t, "exiv2: libexiv2.27.dylib is libexiv2.28.dylib now on "+where+" against check-12; darktable, gpsbabel link it, and this branch rebuilds each", finding.Lines([]string{"gpsbabel", "darktable"})[0].Words)

	// An earlier build older than master's says its version.
	finding.Version, finding.Master = "0.27.7", "0.28.2"
	require.Contains(t, finding.Lines(nil)[0].Words, " against check-12 (0.27.7, older than master's 0.28.2);")
}

// A port dockhand never checked in an environment is compared against
// the archive packages.macports.org serves for master's version and
// default variants there, read on the host: its moved library is named
// against it; an archive not served says nothing, and isn't asked again
// (Then's item 9's second follow-up).
func TestAFirstCheckComparesAgainstMastersArchive(t *testing.T) {
	t.Parallel()
	cc, ccErr := exec.LookPath("cc")
	bzip, bzErr := exec.LookPath("bzip2")
	if ccErr != nil || bzErr != nil {
		t.Skip("cc and bzip2 make the archive")
	}
	dir := t.TempDir()
	source, library := filepath.Join(dir, "x.c"), filepath.Join(dir, "libjq.1.dylib")
	require.NoError(t, os.WriteFile(source, []byte("int x(void) { return 1; }\n"), 0o644))
	if out, err := exec.CommandContext(t.Context(), cc, "-dynamiclib", "-install_name", "/opt/local/lib/libjq.1.dylib", "-o", library, source).CombinedOutput(); err != nil {
		t.Skipf("cc couldn't build a library: %s", out)
	}
	data, err := os.ReadFile(library)
	require.NoError(t, err)
	var packed bytes.Buffer
	w := tar.NewWriter(&packed)
	for _, member := range []struct {
		name string
		body []byte
	}{{"+CONTENTS", []byte("@name jq\n")}, {"./opt/local/bin/jq", []byte("#!/bin/sh\n")}, {"./opt/local/lib/libjq.1.dylib", data}} {
		require.NoError(t, w.WriteHeader(&tar.Header{Name: member.name, Mode: 0o755, Size: int64(len(member.body)), Typeflag: tar.TypeReg}))
		_, err := w.Write(member.body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	command := exec.CommandContext(t.Context(), bzip, "-c")
	command.Stdin = &packed
	archive, err := command.Output()
	require.NoError(t, err)
	var asked []string
	var serving sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serving.Lock()
		asked = append(asked, r.URL.Path)
		serving.Unlock()
		if strings.HasSuffix(r.URL.Path, ".darwin_25.arm64.tbz2") {
			_, _ = w.Write(archive)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	f := setup(t)
	f.options.Packages = server.URL
	e, _ := f.withPreparer(t)
	info := port("jq")
	info.Version, info.Revision = "1.7.1", 0
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {info}}})
	branch := committedUpdate(t, e)
	checkedWith(t, e, branch, model.OutcomePassed, model.OutcomePassed, func(_ *model.GuestExecution, jq *model.TargetResult) {
		jq.Installed = []string{"/opt/local/bin/jq", "/opt/local/lib/libjq.2.dylib"}
		jq.Libraries = []model.Library{{File: "/opt/local/lib/libjq.2.dylib", Name: "/opt/local/lib/libjq.2.dylib", Compatibility: "1.0.0"}}
	})
	var runs []model.Run
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		runs, err = r.Runs(store.RunFilter{Branch: branch.ID})
		return err
	}))
	// status reads no network: with none kept, it says nothing yet.
	found, err := e.InstallChanges(t.Context(), runs[0], false)
	require.NoError(t, err)
	require.Empty(t, found)
	require.Empty(t, asked)
	found, err = e.InstallChanges(t.Context(), runs[0], true)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "master's archive jq-1.7.1_0.darwin_25.arm64.tbz2", found[0].Check)
	require.Equal(t, "libjq.1.dylib is libjq.2.dylib now", found[0].Libraries[0].Words())
	require.Equal(t, []string{"/jq/jq-1.7.1_0.darwin_25.arm64.tbz2"}, asked, "served from the port's directory")

	_, err = e.InstallChanges(t.Context(), runs[0], true)
	require.NoError(t, err)
	require.Len(t, asked, 1, "its reading kept by its name, fetched once")
	kept, err := filepath.Glob(filepath.Join(e.ArchiveDirectory(), "master", "*"))
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(e.ArchiveDirectory(), "master", "jq-1.7.1_0.darwin_25.arm64.tbz2.json")}, kept, "the reading, and no archive")

	// One not served is marked, and asked again once the mark is a day old.
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "textproc/jq"}
	_, _, ok := e.masterArchive(t.Context(), target, tahoe, true)
	require.False(t, ok)
	require.Len(t, asked, 2)
	_, _, ok = e.masterArchive(t.Context(), target, tahoe, true)
	require.False(t, ok)
	require.Len(t, asked, 2, "not asked again within the day")
	none := filepath.Join(e.ArchiveDirectory(), "master", "jq-1.7.1_0.darwin_24.arm64.tbz2.json.none")
	old := e.now().Add(-25 * time.Hour)
	require.NoError(t, os.Chtimes(none, old, old))
	_, _, _ = e.masterArchive(t.Context(), target, tahoe, true)
	require.Len(t, asked, 3, "a day-old mark is asked again")
}
