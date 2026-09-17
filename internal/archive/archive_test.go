package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkReadsTarGzipAndZip(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./root/a.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte("abc"))
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "a.txt"}))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	tarball := filepath.Join(t.TempDir(), "s.tar.gz")
	require.NoError(t, os.WriteFile(tarball, data.Bytes(), 0600))
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, err := zw.Create("root/a.txt")
	require.NoError(t, err)
	_, _ = w.Write([]byte("abc"))
	require.NoError(t, zw.Close())
	zipfile := filepath.Join(t.TempDir(), "s.zip")
	require.NoError(t, os.WriteFile(zipfile, zipped.Bytes(), 0600))
	for _, file := range []string{tarball, zipfile} {
		var names []string
		require.NoError(t, Walk(t.Context(), file, func(m Member) error {
			clean, ok := m.Clean()
			require.True(t, ok)
			if m.Regular {
				body, err := io.ReadAll(m.Body)
				require.NoError(t, err)
				require.Equal(t, "abc", string(body))
			}
			names = append(names, clean)
			return nil
		}))
		require.Equal(t, "root/a.txt", names[0])
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, Walk(ctx, tarball, func(Member) error { return nil }), context.Canceled)
	for _, name := range []string{"/abs", "../up", "a/../b", "."} {
		_, ok := Member{Name: name}.Clean()
		require.False(t, ok, name)
	}
}
