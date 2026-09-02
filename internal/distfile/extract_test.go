package distfile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder the extractor resolves its archiver through: the
// real one, because the archives here are real.
var tools = tool.NewFinder(nil)

func TestPickMember(t *testing.T) {
	cases := []struct {
		name      string
		members   []string
		preferDir string
		want      string
		wantErr   error
	}{
		{
			name:      "under the preferred directory",
			members:   []string{"demo-1.0/src/main.rs", "demo-1.0/Cargo.lock", "demo-1.0/vendor/dep/Cargo.lock"},
			preferDir: "demo-1.0",
			want:      "demo-1.0/Cargo.lock",
		},
		{
			name:      "no preferred directory falls back to the shallowest",
			members:   []string{"pkg/Cargo.lock", "pkg/tests/fixture/Cargo.lock"},
			preferDir: "",
			want:      "pkg/Cargo.lock",
		},
		{
			name:      "preferred directory the archive lacks",
			members:   []string{"other-2.0/Cargo.lock"},
			preferDir: "demo-1.0",
			want:      "other-2.0/Cargo.lock",
		},
		{
			name:      "absent",
			members:   []string{"demo-1.0/Cargo.toml"},
			preferDir: "demo-1.0",
			wantErr:   ErrMemberMissing,
		},
		{
			name:      "two equally shallow with nothing to choose between them",
			members:   []string{"a/Cargo.lock", "b/Cargo.lock"},
			preferDir: "",
			wantErr:   ErrMemberAmbiguous,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pickMember(c.members, c.preferDir, "Cargo.lock")
			if c.wantErr != nil {
				require.ErrorIs(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// tarballWith writes a gzipped tarball, the form nearly every vendored
// port's distfile arrives in.
func tarballWith(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.tar.gz")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck // test fixture
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return path
}

// requireTar gates on the archiver the extractor drives. It goes
// through testenv rather than stat'ing tool.Tar directly so that a run
// which is supposed to have tar fails instead of quietly skipping.
func requireTar(t *testing.T) {
	t.Helper()
	testenv.Tool(t, "tar")
}

func TestExtractReadsTheFileUnderThePreferredDirectory(t *testing.T) {
	requireTar(t)
	const want = "version = 3\n"
	archive := tarballWith(t, map[string]string{
		"demo-1.0/Cargo.toml":            "[package]\n",
		"demo-1.0/Cargo.lock":            want,
		"demo-1.0/vendor/dep/Cargo.lock": "version = 9\n",
		"demo-1.0/src/main.rs":           "fn main() {}\n",
	})
	got, from, err := Extract(context.Background(), tools, []string{archive}, "demo-1.0", "Cargo.lock")
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
	assert.Equal(t, archive, from)
}

// yazi fetches its man pages as distfiles alongside its source tarball, so
// a candidate that is not an archive at all must be skipped rather than
// fail the search.
func TestExtractSkipsCandidatesThatAreNotArchives(t *testing.T) {
	requireTar(t)
	const want = "version = 3\n"
	manpage := filepath.Join(t.TempDir(), "demo.1")
	require.NoError(t, os.WriteFile(manpage, []byte(".TH DEMO 1\n"), 0o644))
	archive := tarballWith(t, map[string]string{"demo-1.0/Cargo.lock": want})

	got, from, err := Extract(context.Background(), tools, []string{manpage, archive}, "demo-1.0", "Cargo.lock")
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
	assert.Equal(t, archive, from, "the man page is skipped, the tarball answers")
}

// rust-bootstrap fetches prebuilt toolchains beside its source; only one
// candidate carries the file.
func TestExtractTakesTheFirstCandidateThatCarriesTheFile(t *testing.T) {
	requireTar(t)
	bootstrap := tarballWith(t, map[string]string{"toolchain/bin/rustc": "binary\n"})
	source := tarballWith(t, map[string]string{"demo-1.0/Cargo.lock": "version = 3\n"})
	got, from, err := Extract(context.Background(), tools, []string{bootstrap, source}, "demo-1.0", "Cargo.lock")
	require.NoError(t, err)
	assert.Equal(t, "version = 3\n", string(got))
	assert.Equal(t, source, from)
}

func TestExtractMissingFromEveryCandidate(t *testing.T) {
	requireTar(t)
	archive := tarballWith(t, map[string]string{"demo-1.0/Cargo.toml": "[package]\n"})
	_, _, err := Extract(context.Background(), tools, []string{archive}, "demo-1.0", "Cargo.lock")
	require.ErrorIs(t, err, ErrMemberMissing)
	assert.Contains(t, err.Error(), "src.tar.gz")
}

func TestExtractEmptyMemberIsNotAnAnswer(t *testing.T) {
	requireTar(t)
	archive := tarballWith(t, map[string]string{"demo-1.0/Cargo.lock": ""})
	_, _, err := Extract(context.Background(), tools, []string{archive}, "demo-1.0", "Cargo.lock")
	require.ErrorIs(t, err, ErrMemberMissing)
}
