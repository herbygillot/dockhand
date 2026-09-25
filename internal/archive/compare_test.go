package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func tarball(t *testing.T, top string, files map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), top+".tar.gz")
	out, err := os.Create(name)
	require.NoError(t, err)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(files[path])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(files[path]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, out.Close())
	return name
}

func zipball(t *testing.T, top string, files map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), top+".zip")
	out, err := os.Create(name)
	require.NoError(t, err)
	zw := zip.NewWriter(out)
	for path, content := range files {
		w, err := zw.Create(top + "/" + path)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, out.Close())
	return name
}

func messages(changes []Change) []string {
	var all []string
	for _, change := range changes {
		all = append(all, change.String())
	}
	return all
}

func TestCompareFindsWhatAReviewerWouldAskAbout(t *testing.T) {
	older := tarball(t, "croc-10.2.4", map[string]string{
		"LICENSE":        "MIT\n",
		"go.mod":         "module croc\n\nrequire (\n\tgolang.org/x/sys v0.30.0\n\tgithub.com/old/dep v1.0.0\n\tgolang.org/x/text v0.1.0 // indirect\n)\n",
		"main.go":        "package main\n",
		"src/deep.go":    "package src\n",
		"CMakeLists.txt": "project(croc)\n",
	})
	newer := tarball(t, "croc-10.2.5", map[string]string{
		"LICENSE":        "Apache-2.0\n",
		"go.mod":         "module croc\n\nrequire (\n\tgolang.org/x/sys v0.31.0\n\tgolang.org/x/net v0.44.0\n\tgolang.org/x/text v0.2.0 // indirect\n)\n",
		"main.go":        "package main // changed\n",
		"CMakeLists.txt": "project(croc)\n",
		"meson.build":    "project('croc')\n",
	})
	changes, err := Compare(t.Context(), older, newer)
	require.NoError(t, err)
	require.Equal(t, []string{
		"! upstream's LICENSE changed; the Portfile's license line may need to follow",
		"! upstream: go.mod adds golang.org/x/net v0.44.0",
		"· upstream: go.mod drops github.com/old/dep",
		"· upstream: go.mod moves golang.org/x/sys from v0.30.0 to v0.31.0",
		"! upstream's meson.build is new; the build may need the Portfile to follow",
	}, messages(changes), "unchanged CMakeLists.txt, source files, and indirect modules say nothing")

	same, err := Compare(t.Context(), older, tarball(t, "croc-10.2.6", map[string]string{
		"LICENSE": "MIT\n", "go.mod": "module croc\n\nrequire (\n\tgolang.org/x/sys v0.30.0\n\tgithub.com/old/dep v1.0.0\n)\n", "CMakeLists.txt": "project(croc)\n", "main.go": "x",
	}))
	require.NoError(t, err)
	require.Empty(t, same)
}

func TestCompareReadsTheOtherManifestsAndZips(t *testing.T) {
	older := zipball(t, "pkg-1.0", map[string]string{
		"Cargo.toml":       "[package]\nname = \"pkg\"\n\n[dependencies]\nserde = \"1.0\"\n\n[dev-dependencies]\nproptest = \"1\"\n",
		"package.json":     `{"dependencies": {"left-pad": "^1.0.0"}}`,
		"requirements.txt": "requests>=2.0\n# a comment\n",
		"pyproject.toml":   "[project]\ndependencies = [\n  \"click>=8\",\n]\n",
		"docs/COPYING.md":  "GPL\n",
	})
	newer := zipball(t, "pkg-1.1", map[string]string{
		"Cargo.toml":       "[package]\nname = \"pkg\"\n\n[dependencies]\nserde = \"1.0\"\ntokio = { version = \"1\" }\n",
		"package.json":     `{"dependencies": {"left-pad": "^1.0.0", "chalk": "^5"}}`,
		"requirements.txt": "requests>=2.1\n",
		"pyproject.toml":   "[project]\ndependencies = [\n  \"click>=8\",\n  \"rich>=13\",\n]\n",
	})
	changes, err := Compare(t.Context(), older, newer)
	require.NoError(t, err)
	require.Equal(t, []string{
		"! upstream: Cargo.toml adds tokio { version = \"1\" }",
		"· upstream: Cargo.toml drops proptest",
		"! upstream's docs/COPYING.md was removed; the Portfile's license line may need to follow",
		"! upstream: package.json adds chalk ^5",
		"! upstream: pyproject.toml adds rich >=13",
		"· upstream: requirements.txt moves requests from >=2.0 to >=2.1",
	}, messages(changes))
}
