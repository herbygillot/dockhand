package command

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// sourceArchives stands in for upstream: each version's archive holds a
// NEWS file naming it, and 1.8.1 adds src/new.c. With manual, 1.7.1
// fetches a manual first, which 1.8.1 no longer does.
type sourceArchives struct {
	repo   *git.Repository
	manual bool
}

func (s sourceArchives) FetchArchives(ctx context.Context, source model.Source, directory, into string) ([]engine.FetchedArchive, error) {
	_, data, err := s.repo.File(ctx, string(source.Tree), directory+"/Portfile")
	if err != nil {
		return nil, err
	}
	version := regexp.MustCompile(`(?m)^version\s+(\S+)$`).FindStringSubmatch(string(data))[1]
	files := map[string]string{"NEWS": "jq " + version + "\n", "src/jq.c": "int main(void) { return 0; }\n"}
	if version != "1.7.1" {
		files["src/new.c"] = "void added(void) {}\n"
	}
	archive, err := writeSourceArchive(into, "jq-"+version, files)
	if err != nil {
		return nil, err
	}
	fetched := []engine.FetchedArchive{archive}
	if s.manual && version == "1.7.1" {
		manual, err := writeSourceArchive(into, "jq-manual", map[string]string{"NEWS": "the manual\n"})
		if err != nil {
			return nil, err
		}
		fetched = append([]engine.FetchedArchive{manual}, fetched...)
	}
	return fetched, nil
}

// writeSourceArchive writes files as an archive enclosed in top.
func writeSourceArchive(into, top string, files map[string]string) (engine.FetchedArchive, error) {
	name := top + ".tar.gz"
	path := filepath.Join(into, name)
	out, err := os.Create(path)
	if err != nil {
		return engine.FetchedArchive{}, err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, member := range []string{"NEWS", "src/jq.c", "src/new.c"} {
		if body, ok := files[member]; ok {
			if err := tw.WriteHeader(&tar.Header{Name: top + "/" + member, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				return engine.FetchedArchive{}, err
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				return engine.FetchedArchive{}, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return engine.FetchedArchive{}, err
	}
	if err := gz.Close(); err != nil {
		return engine.FetchedArchive{}, err
	}
	if err := out.Close(); err != nil {
		return engine.FetchedArchive{}, err
	}
	sum := sha256.Sum256([]byte(top))
	return engine.FetchedArchive{Name: name, Path: path, Sum: portfile.Checksum{Name: name, SHA256: hex.EncodeToString(sum[:])}}, nil
}

func TestDiffArchiveShowsWhatChangedInside(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	testArchiveFetcher = func(e *engine.Engine) engine.ArchiveFetcher { return sourceArchives{repo: e.Repo} }
	t.Cleanup(func() { testArchiveFetcher = nil })
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := dockhand(t, "diff", "--archive")
	require.NoError(t, err)
	require.Contains(t, out, "textproc/jq · jq-1.7.1.tar.gz → jq-1.8.1.tar.gz: 2 files differ: 1 changed, 1 added\n\n")
	require.Contains(t, out, "--- a/NEWS\n+++ b/NEWS\n@@ -1 +1 @@\n-jq 1.7.1\n+jq 1.8.1\n")
	require.Contains(t, out, "+++ b/src/new.c\n")
	require.NotContains(t, out, "src/jq.c")

	out, _, err = dockhand(t, "diff", "--archive", "--stat", "jq")
	require.NoError(t, err)
	require.Equal(t, "textproc/jq · jq-1.7.1.tar.gz → jq-1.8.1.tar.gz: 2 files differ: 1 changed, 1 added\n", out)

	out, _, err = dockhand(t, "diff", "--archive", "curl")
	require.NoError(t, err)
	require.Equal(t, "No changed port has archives to compare.\n", out)
}

// Each archive is diffed with the one it corresponds to, as an assessment
// pairs them: a manual the base fetched first and the branch doesn't is no
// longer fetched, where by position it had been diffed with 1.8.1's source
// (the architecture review's finding 2).
func TestDiffArchivePairsWhatCorresponds(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	testArchiveFetcher = func(e *engine.Engine) engine.ArchiveFetcher { return sourceArchives{repo: e.Repo, manual: true} }
	t.Cleanup(func() { testArchiveFetcher = nil })
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := dockhand(t, "diff", "--archive", "--stat")
	require.NoError(t, err)
	require.Equal(t, "textproc/jq · jq-1.7.1.tar.gz → jq-1.8.1.tar.gz: 2 files differ: 1 changed, 1 added\n\n"+
		"textproc/jq · jq-manual.tar.gz is no longer fetched\n", out)

	out, _, err = dockhand(t, "diff", "--archive", "--stat", "--json")
	require.NoError(t, err)
	require.Contains(t, out, `"old": "jq-1.7.1.tar.gz",
        "new": "jq-1.8.1.tar.gz",
        "status": "matched",
        "basis": "pattern"`)
	require.Contains(t, out, `"old": "jq-manual.tar.gz",
        "status": "removed"`)
}
