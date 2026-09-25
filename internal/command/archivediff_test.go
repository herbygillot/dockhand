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
// NEWS file naming it, and 1.8.1 adds src/new.c.
type sourceArchives struct{ repo *git.Repository }

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
	name := "jq-" + version + ".tar.gz"
	path := filepath.Join(into, name)
	out, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, member := range []string{"NEWS", "src/jq.c", "src/new.c"} {
		if body, ok := files[member]; ok {
			if err := tw.WriteHeader(&tar.Header{Name: "jq-" + version + "/" + member, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				return nil, err
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(version))
	return []engine.FetchedArchive{{Name: name, Path: path, Sum: portfile.Checksum{Name: name, SHA256: hex.EncodeToString(sum[:])}}}, nil
}

func TestDiffArchiveShowsWhatChangedInside(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	testArchiveFetcher = func(e *engine.Engine) engine.ArchiveFetcher { return sourceArchives{repo: e.Repo} }
	t.Cleanup(func() { testArchiveFetcher = nil })
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "src", "macports-branches", "jq-update"))
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
