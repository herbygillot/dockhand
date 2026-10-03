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

// An archive past the scan limit says so, whether the limit falls between
// members or within one, rather than "unexpected EOF": rustc's source, 3.5
// GiB uncompressed, read as broken (the rust and cargo run).
func TestAWalkPastTheLimitSaysSo(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"root/a.txt", "root/b.txt"} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: 2048, Typeflag: tar.TypeReg}))
		_, _ = tw.Write(bytes.Repeat([]byte("x"), 2048))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	tarball := filepath.Join(t.TempDir(), "source-123")
	require.NoError(t, os.WriteFile(tarball, data.Bytes(), 0600))

	saved := scanLimit
	t.Cleanup(func() { scanLimit = saved })
	for _, limit := range []int64{1024, 3072} {
		scanLimit = limit
		err := Walk(t.Context(), tarball, func(m Member) error {
			_, err := io.Copy(io.Discard, m.Body)
			return err
		})
		require.ErrorIs(t, err, errScanLimit, "a limit of %d", limit)
		require.NotContains(t, err.Error(), "source-123", "the temporary file isn't named")
	}
	scanLimit = 1 << 20
	require.NoError(t, Walk(t.Context(), tarball, func(Member) error { return nil }), "within the limit, the archive ends as it does")

	// A zip archive's members count as they're read, under the same limit,
	// where it had none (the limits sweep); one not read costs nothing.
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	for _, name := range []string{"root/a.txt", "root/b.txt"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	}
	require.NoError(t, zw.Close())
	zipfile := filepath.Join(t.TempDir(), "source-456")
	require.NoError(t, os.WriteFile(zipfile, zipped.Bytes(), 0600))
	scanLimit = 3000
	err := Walk(t.Context(), zipfile, func(m Member) error {
		_, err := io.Copy(io.Discard, m.Body)
		return err
	})
	require.ErrorIs(t, err, errScanLimit)
	require.ErrorContains(t, err, "more than the 3 KB dockhand reads of one")
	require.NoError(t, Walk(t.Context(), zipfile, func(Member) error { return nil }))
}

// An installer package or an executable is said to be a binary, with no
// source in it, rather than misread as a broken tarball: 1password-cli's
// .pkg read as "archive/tar: invalid tar header" (field testing's batch
// 10, finding 2).
func TestABinaryIsSaidToBeOne(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{"op_apple_universal_v2.30.0.pkg": "xar!\x00\x1c\x00\x01rest", "tool": "\xcf\xfa\xed\xfe\x07\x00\x00\x01"} {
		file := filepath.Join(directory, name)
		require.NoError(t, os.WriteFile(file, []byte(data), 0o644))
		err := Walk(t.Context(), file, func(Member) error { return nil })
		require.ErrorIs(t, err, ErrBinary, name)
		require.ErrorContains(t, err, name+" is a ")
	}
}
