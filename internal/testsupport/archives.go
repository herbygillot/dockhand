package testsupport

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Tarball writes a gzipped tar of files under one top directory, as a
// release's source archive is laid out, and returns its path.
func Tarball(t testing.TB, top string, files map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), top+".tar.gz")
	out, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if err := tw.WriteHeader(&tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(files[path])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(files[path])); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{tw, gz, out} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return name
}

// Zipball writes a zip of files under one top directory, and returns its
// path.
func Zipball(t testing.TB, top string, files map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), top+".zip")
	out, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, path := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(top + "/" + path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[path])); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{zw, out} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return name
}
